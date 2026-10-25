package extract

import (
	"go/ast"
	"go/token"
	"sort"
	"strings"

	"fpcount/internal/ifpug"
	"fpcount/internal/srcindex"
)

// Route é uma rota HTTP lógica descoberta no código.
type Route struct {
	Method   string
	Path     string
	Perm     string
	Handlers []handlerRef
	Evidence string
	File     *srcindex.File
	Resolved bool // método/caminho resolvidos estaticamente
	Origin   string
}

type handlerRef struct {
	fn     *srcindex.Func
	params map[string]bool // parâmetros da fábrica (dependências injetadas)
}

const apiPrefix = "/api/v1"

// Routes percorre o pacote httpapi e devolve as rotas lógicas (sem duplicar o
// alias /api/v1) e a contagem de dispatchers `{action...}` ignorados.
func Routes(ix *srcindex.Index, dir string) (routes []*Route, dispatchers int) {
	seen := map[string]*Route{}
	for _, f := range ix.Dir(dir) {
		if f.Rel == dir+"/server.go" {
			continue
		}
		for _, d := range f.AST.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			w := &routeWalker{ix: ix, dir: dir, f: f, fd: fd}
			w.block(fd.Body, newEnv(nil))
			dispatchers += w.dispatchers
			for _, r := range w.found {
				key := r.Method + " " + strings.TrimPrefix(r.Path, apiPrefix)
				if prev, ok := seen[key]; ok {
					if len(prev.Handlers) == 0 {
						prev.Handlers = r.Handlers
					}
					continue
				}
				seen[key] = r
			}
		}
	}
	for _, r := range seen {
		routes = append(routes, r)
	}
	sort.Slice(routes, func(i, j int) bool {
		a, b := routes[i], routes[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Method < b.Method
	})
	return routes, dispatchers
}

type routeWalker struct {
	ix          *srcindex.Index
	dir         string
	f           *srcindex.File
	fd          *ast.FuncDecl
	found       []*Route
	dispatchers int
}

func (w *routeWalker) block(b *ast.BlockStmt, ev *env) {
	sc := newEnv(ev)
	for _, st := range b.List {
		w.stmt(st, sc)
	}
}

func (w *routeWalker) stmt(st ast.Stmt, ev *env) {
	switch s := st.(type) {
	case *ast.RangeStmt:
		inner := newEnv(ev)
		bindRange(s, inner)
		w.expr(s.X, ev)
		w.block(s.Body, inner)
		return
	case *ast.AssignStmt:
		if s.Tok == token.DEFINE && len(s.Lhs) == 1 && len(s.Rhs) == 1 {
			if id, ok := s.Lhs[0].(*ast.Ident); ok {
				ev.vars[id.Name] = []ast.Expr{s.Rhs[0]}
			}
		}
		for _, r := range s.Rhs {
			w.expr(r, ev)
		}
		return
	case *ast.BlockStmt:
		w.block(s, ev)
		return
	case *ast.IfStmt:
		if s.Init != nil {
			w.stmt(s.Init, ev)
		}
		w.expr(s.Cond, ev)
		w.block(s.Body, ev)
		if s.Else != nil {
			w.stmt(s.Else, ev)
		}
		return
	case *ast.ForStmt:
		w.block(s.Body, ev)
		return
	case *ast.SwitchStmt:
		w.block(s.Body, ev)
		return
	case *ast.CaseClause:
		for _, b := range s.Body {
			w.stmt(b, ev)
		}
		return
	}
	ast.Inspect(st, func(n ast.Node) bool { return w.visit(n, ev) })
}

func (w *routeWalker) expr(x ast.Expr, ev *env) {
	ast.Inspect(x, func(n ast.Node) bool { return w.visit(n, ev) })
}

func (w *routeWalker) visit(n ast.Node, ev *env) bool {
	switch t := n.(type) {
	case *ast.FuncLit:
		w.block(t.Body, ev)
		return false
	case *ast.CompositeLit:
		w.compositeRoute(t, ev)
	case *ast.CallExpr:
		w.call(t, ev)
	}
	return true
}

func keyed(cl *ast.CompositeLit) map[string]ast.Expr {
	m := map[string]ast.Expr{}
	for _, el := range cl.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			if k, ok := kv.Key.(*ast.Ident); ok {
				m[k.Name] = kv.Value
			}
		}
	}
	return m
}

func (w *routeWalker) compositeRoute(cl *ast.CompositeLit, ev *env) {
	m := keyed(cl)
	me, okm := m["Method"]
	pe, okp := m["Path"]
	if !okm || !okp {
		return
	}
	methods, ok1 := evalStrings(w.ix, w.dir, me, ev, 0)
	paths, ok2 := evalStrings(w.ix, w.dir, pe, ev, 0)
	perm := ""
	if pm, ok := m["Permission"]; ok {
		if v, ok := evalStrings(w.ix, w.dir, pm, ev, 0); ok && len(v) > 0 {
			perm = v[0]
		}
	}
	var hs []handlerRef
	if he, ok := m["Handler"]; ok {
		hs = w.handlerOf(he)
	}
	w.emit(methods, paths, perm, hs, cl.Pos(), ok1 && ok2, "Route{}")
}

func (w *routeWalker) call(c *ast.CallExpr, ev *env) {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if ok && sel.Sel.Name == "HandleFunc" && len(c.Args) == 2 {
		w.dispatchers++ // multiplexador: as operações lógicas vêm do registry
		return
	}
	// registry.Register(method, path, perm)
	if ok && sel.Sel.Name == "Register" && len(c.Args) == 3 {
		methods, ok1 := evalStrings(w.ix, w.dir, c.Args[0], ev, 0)
		paths, ok2 := evalStrings(w.ix, w.dir, c.Args[1], ev, 0)
		perm := ""
		if v, ok := evalStrings(w.ix, w.dir, c.Args[2], ev, 0); ok && len(v) > 0 {
			perm = v[0]
		}
		if ok1 && ok2 {
			w.emit(methods, paths, perm, w.factoryHandlers(), c.Pos(), true, "Register()")
		}
		return
	}
	// wrapper local que repassa o path ao registry (ex.: registerOpsCyclePermission)
	if id, ok := c.Fun.(*ast.Ident); ok && len(c.Args) >= 3 {
		if fn := w.ix.Func(w.dir, id.Name); fn != nil && wrapsRegister(fn.Decl) {
			paths, ok2 := evalStrings(w.ix, w.dir, c.Args[1], ev, 0)
			perm := ""
			if v, ok := evalStrings(w.ix, w.dir, c.Args[2], ev, 0); ok && len(v) > 0 {
				perm = v[0]
			}
			if ok2 {
				w.emit([]string{wrappedMethod(fn.Decl)}, paths, perm, w.factoryHandlers(), c.Pos(), true, "wrapper:"+id.Name)
			}
		}
	}
}

func wrapsRegister(fd *ast.FuncDecl) bool {
	if fd.Body == nil {
		return false
	}
	found := false
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			if s, ok := c.Fun.(*ast.SelectorExpr); ok && s.Sel.Name == "Register" && len(c.Args) == 3 {
				found = true
			}
		}
		return true
	})
	return found
}

func wrappedMethod(fd *ast.FuncDecl) string {
	m := "POST"
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			if s, ok := c.Fun.(*ast.SelectorExpr); ok && s.Sel.Name == "Register" && len(c.Args) == 3 {
				if se, ok := c.Args[0].(*ast.SelectorExpr); ok {
					if v, ok := httpMethods[se.Sel.Name]; ok {
						m = v
					}
				}
			}
		}
		return true
	})
	return m
}

// factoryHandlers: para operações registradas só no registry (despachadas por
// `{action...}`), o handler é o(s) `s.handleX(...)` construídos na mesma função.
func (w *routeWalker) factoryHandlers() []handlerRef {
	var hs []handlerRef
	seen := map[string]bool{}
	ast.Inspect(w.fd.Body, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if s, ok := c.Fun.(*ast.SelectorExpr); ok && strings.HasPrefix(s.Sel.Name, "handle") && !seen[s.Sel.Name] {
			seen[s.Sel.Name] = true
			hs = append(hs, w.handlerOf(c)...)
		}
		return true
	})
	return hs
}

func (w *routeWalker) handlerOf(x ast.Expr) []handlerRef {
	var name string
	params := map[string]bool{}
	switch t := x.(type) {
	case *ast.SelectorExpr:
		name = t.Sel.Name
	case *ast.CallExpr:
		if s, ok := t.Fun.(*ast.SelectorExpr); ok {
			name = s.Sel.Name
		} else if id, ok := t.Fun.(*ast.Ident); ok {
			name = id.Name
		}
	case *ast.Ident:
		name = t.Name
	case *ast.FuncLit:
		return []handlerRef{{fn: &srcindex.Func{Decl: &ast.FuncDecl{Name: ast.NewIdent("<literal>"), Type: t.Type, Body: t.Body}, File: w.f}, params: params}}
	}
	if name == "" {
		return nil
	}
	fn := w.ix.Func(w.dir, name)
	if fn == nil {
		return nil
	}
	if fn.Decl.Type.Params != nil {
		for _, p := range fn.Decl.Type.Params.List {
			if isHTTPParam(p.Type) {
				continue
			}
			for _, n := range p.Names {
				params[n.Name] = true
			}
		}
	}
	return []handlerRef{{fn: fn, params: params}}
}

func (w *routeWalker) emit(methods, paths []string, perm string, hs []handlerRef, pos token.Pos, resolved bool, origin string) {
	if !resolved {
		methods, paths = []string{"?"}, []string{"?"}
	}
	for _, m := range methods {
		for _, p := range paths {
			w.found = append(w.found, &Route{Method: m, Path: p, Perm: perm, Handlers: hs,
				Evidence: w.ix.Pos(w.f, pos), File: w.f, Resolved: resolved, Origin: origin})
		}
	}
}

// IsInfra diz se a rota é de infraestrutura (fora da contagem funcional).
func IsInfra(p string) bool {
	p = strings.TrimPrefix(p, apiPrefix)
	switch p {
	case "/healthz", "/readyz", "/metrics", "/version":
		return true
	}
	return false
}

var _ = ifpug.EI

// isHTTPParam reconhece http.ResponseWriter / *http.Request (não são dependências).
func isHTTPParam(t ast.Expr) bool {
	if st, ok := t.(*ast.StarExpr); ok {
		t = st.X
	}
	if se, ok := t.(*ast.SelectorExpr); ok {
		return se.Sel.Name == "ResponseWriter" || se.Sel.Name == "Request"
	}
	return false
}
