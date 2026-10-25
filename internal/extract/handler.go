package extract

import (
	"go/ast"
	"go/token"
	"regexp"
	"sort"
	"strings"

	"fpcount/internal/srcindex"
)

// Info é o resultado da análise estática de um handler (e dos helpers do
// mesmo pacote que ele chama, até maxFollow níveis).
type Info struct {
	InParams   map[string]bool // parâmetros de query/path
	InFields   map[string]bool // campos do corpo decodificado
	OutFields  map[string]bool // campos do corpo de resposta de sucesso
	Deps       map[string]bool // dependências de dados tocadas (proxy de FTR)
	Writes     bool
	Derived    bool
	InKnown    bool // algum tipo de entrada foi resolvido
	OutKnown   bool
	WriteNames map[string]bool
}

const maxFollow = 2

var (
	reWrite   = regexp.MustCompile(`^(Create|Insert|Update|Save|Upsert|Put|Delete|Remove|Append|Record|Set|Store|Apply|Approve|Revoke|Enqueue|Cancel|Activate|Pause|Publish|Register|Import|Ingest|Mark|Transition|Decide|Advance|Capture|Rotate|Replace|Add|Link|Reissue|Regenerate|Start|Recalculate|Remeasure|Collect|Sync|Reconcile|Retire|Rollback|Verify|Test|Open|Close|Attest|Propose|Materialize|Reactivate|Backfill|Seed|Mutate|Patch|Submit|Reopen|Expire|Persist)`)
	reDeriv   = regexp.MustCompile(`(?i)(score|compute|calculat|aggregat|summar|explain|rank|ceiling|estimat|trend|quality|render|forecast|evaluate|decide|classif|derive|metric|econom|breakeven|projection|tier)`)
	reDecoder = regexp.MustCompile(`^(decode|Decode|parse|Parse|bind|Bind|unmarshal|Unmarshal)`)
	reDep     = regexp.MustCompile(`(?i)(Store|Repo|Repository|Service|Source|Port|Reader|Writer|Client|Recorder|Registry|Applier|Lifecycle|Capturer|Guard|Auditor|Checker|Provider|Fetcher|Planner|Queue|Cache|Index|Snapshot|DB|Mongo)s?$`)
)

// notData: campos do servidor que não são arquivos lógicos (infra, relógio, log, roteamento).
var notData = map[string]bool{"logger": true, "log": true, "clock": true, "now": true, "mux": true,
	"protectedmux": true, "publicmux": true, "routes": true, "metrics": true, "cfg": true, "config": true,
	"rt": true, "ratelimit": true, "limiter": true, "errors": true, "authn": true, "auth": true, "csrf": true,
	"guard": true, "tracer": true, "reqid": true}

func newInfo() *Info {
	return &Info{InParams: map[string]bool{}, InFields: map[string]bool{}, OutFields: map[string]bool{},
		Deps: map[string]bool{}, WriteNames: map[string]bool{}}
}

type analyzer struct {
	ix      *srcindex.Index
	dir     string
	visited map[*ast.FuncDecl]bool
	params  map[string]bool
}

// Analyze estuda o handler e devolve o Info agregado.
func Analyze(ix *srcindex.Index, dir string, h handlerRef) *Info {
	a := &analyzer{ix: ix, dir: dir, visited: map[*ast.FuncDecl]bool{}, params: h.params}
	info := newInfo()
	a.fn(h.fn, info, 0)
	return info
}

func (a *analyzer) fn(fn *srcindex.Func, info *Info, depth int) {
	if fn == nil || fn.Decl.Body == nil || a.visited[fn.Decl] {
		return
	}
	a.visited[fn.Decl] = true
	vars := map[string]varType{}
	recv := ""
	if fn.Decl.Recv != nil && len(fn.Decl.Recv.List) > 0 && len(fn.Decl.Recv.List[0].Names) > 0 {
		recv = fn.Decl.Recv.List[0].Names[0].Name
	}
	ast.Inspect(fn.Decl.Body, func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.AssignStmt:
			a.assign(fn.File, t, vars)
		case *ast.DeclStmt:
			if gd, ok := t.Decl.(*ast.GenDecl); ok && gd.Tok == token.VAR {
				for _, sp := range gd.Specs {
					vs := sp.(*ast.ValueSpec)
					if vs.Type != nil {
						for _, nm := range vs.Names {
							vars[nm.Name] = varType{expr: vs.Type, file: fn.File}
						}
					}
				}
			}
		case *ast.CallExpr:
			a.call(fn, t, recv, vars, info, depth)
		}
		return true
	})
}

type varType struct {
	expr ast.Expr
	file *srcindex.File
	lit  *ast.CompositeLit
}

func (a *analyzer) assign(f *srcindex.File, s *ast.AssignStmt, vars map[string]varType) {
	if len(s.Rhs) == 0 {
		return
	}
	first, ok := s.Lhs[0].(*ast.Ident)
	if !ok || first.Name == "_" {
		return
	}
	rhs := s.Rhs[0]
	if len(s.Rhs) == 1 {
		if vt, ok := a.typeOfExpr(f, rhs, vars); ok {
			vars[first.Name] = vt
		}
	}
}

// typeOfExpr infere o tipo de uma expressão simples.
func (a *analyzer) typeOfExpr(f *srcindex.File, e ast.Expr, vars map[string]varType) (varType, bool) {
	switch t := e.(type) {
	case *ast.UnaryExpr:
		if t.Op == token.AND {
			return a.typeOfExpr(f, t.X, vars)
		}
	case *ast.CompositeLit:
		if t.Type != nil {
			return varType{expr: t.Type, file: f, lit: t}, true
		}
	case *ast.Ident:
		if v, ok := vars[t.Name]; ok {
			return v, true
		}
	case *ast.CallExpr:
		if fn := a.ix.ResolveFunc(f, t.Fun); fn != nil && fn.Decl.Type.Results != nil && len(fn.Decl.Type.Results.List) > 0 {
			return varType{expr: fn.Decl.Type.Results.List[0].Type, file: fn.File}, true
		}
	}
	return varType{}, false
}

func addAll(dst map[string]bool, src []string) {
	for _, s := range src {
		dst[s] = true
	}
}

func (a *analyzer) call(fn *srcindex.Func, c *ast.CallExpr, recv string, vars map[string]varType, info *Info, depth int) {
	name, rcvText := calleeName(c)
	lname := strings.ToLower(name)

	// parâmetros de query/path/form
	switch name {
	case "Get", "PathValue", "Has", "FormValue", "Values":
		if len(c.Args) == 1 {
			if lit, ok := c.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if !strings.Contains(strings.ToLower(rcvText), "header") && !strings.Contains(strings.ToLower(rcvText), "cookie") {
					info.InParams[strings.Trim(lit.Value, `"`)] = true
				}
			}
		}
	}

	// corpo decodificado
	if name == "Decode" && len(c.Args) == 1 {
		if vt, ok := a.typeOfExpr(fn.File, c.Args[0], vars); ok {
			leaves, _ := a.ix.LeafFields(vt.file, vt.expr)
			addAll(info.InFields, leaves)
			info.InKnown = true
		}
	}
	if reDecoder.MatchString(name) && !strings.Contains(lname, "error") && name != "Decode" {
		if f2 := a.ix.ResolveFunc(fn.File, c.Fun); f2 != nil && f2.Decl.Type.Results != nil && len(f2.Decl.Type.Results.List) > 0 {
			leaves, _ := a.ix.LeafFields(f2.File, f2.Decl.Type.Results.List[0].Type)
			addAll(info.InFields, leaves)
			info.InKnown = true
		}
	}

	// resposta de sucesso
	if strings.HasPrefix(lname, "write") && strings.HasSuffix(lname, "json") && len(c.Args) >= 4 {
		if lit := c.Args[2]; lit != nil {
			if isErrorStatus(lit) {
				return
			}
		}
		a.output(fn, c.Args[3], vars, info)
	}

	// dependências de dados e escrita / derivação
	if sel, ok := c.Fun.(*ast.SelectorExpr); ok {
		root, chain := rootChain(sel.X)
		key := ""
		switch {
		case root == "":
		case root == recv:
			if len(chain) > 0 && !notData[strings.ToLower(chain[0])] {
				key = root + "." + chain[0]
			}
		case a.params[root] || root == "cfg":
			key = root
			if root == "cfg" && len(chain) > 0 {
				key = root + "." + chain[0]
			}
		}
		if key != "" {
			info.Deps[key] = true
			if reWrite.MatchString(sel.Sel.Name) {
				info.Writes = true
				info.WriteNames[sel.Sel.Name] = true
			}
			if reDeriv.MatchString(sel.Sel.Name) {
				info.Derived = true
			}
		}
	}
	if reDeriv.MatchString(name) && name != "" && !strings.HasPrefix(lname, "write") {
		info.Derived = true
	}

	// segue helpers do mesmo pacote
	if depth < maxFollow && name != "" && !strings.HasPrefix(lname, "write") && !strings.Contains(lname, "error") {
		if f2 := a.ix.Func(a.dir, name); f2 != nil && f2.File.Dir == a.dir {
			if _, isSel := c.Fun.(*ast.SelectorExpr); !isSel || rcvText == recv {
				a.fn(f2, info, depth+1)
			}
		}
	}
}

func isErrorStatus(e ast.Expr) bool {
	switch t := e.(type) {
	case *ast.SelectorExpr:
		n := t.Sel.Name
		return strings.HasPrefix(n, "StatusBad") || strings.HasPrefix(n, "StatusUnauth") || strings.HasPrefix(n, "StatusForb") ||
			strings.HasPrefix(n, "StatusNotFound") || strings.HasPrefix(n, "StatusConflict") || strings.HasPrefix(n, "StatusInternal") ||
			strings.HasPrefix(n, "StatusUnprocessable") || strings.HasPrefix(n, "StatusService") || strings.HasPrefix(n, "StatusTooMany") ||
			strings.HasPrefix(n, "StatusPrecondition") || strings.HasPrefix(n, "StatusMethodNot")
	}
	return false
}

func (a *analyzer) output(fn *srcindex.Func, e ast.Expr, vars map[string]varType, info *Info) {
	if cl, ok := e.(*ast.CompositeLit); ok {
		if _, isMap := cl.Type.(*ast.MapType); isMap {
			for _, el := range cl.Elts {
				if kv, ok := el.(*ast.KeyValueExpr); ok {
					if bl, ok := kv.Key.(*ast.BasicLit); ok {
						info.OutFields[strings.Trim(bl.Value, `"`)] = true
					}
				}
			}
			info.OutKnown = true
			return
		}
	}
	vt, ok := a.typeOfExpr(fn.File, e, vars)
	if !ok {
		return
	}
	if id, ok := vt.expr.(*ast.Ident); ok && (id.Name == "any" || id.Name == "interface{}") {
		return
	}
	if _, isIface := vt.expr.(*ast.InterfaceType); isIface {
		return
	}
	leaves, _ := a.ix.LeafFields(vt.file, vt.expr)
	if len(leaves) == 0 {
		return
	}
	addAll(info.OutFields, leaves)
	info.OutKnown = true
}

func calleeName(c *ast.CallExpr) (name, rcv string) {
	switch ft := c.Fun.(type) {
	case *ast.Ident:
		return ft.Name, ""
	case *ast.SelectorExpr:
		return ft.Sel.Name, exprText(ft.X)
	}
	return "", ""
}

func exprText(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return exprText(t.X) + "." + t.Sel.Name
	case *ast.CallExpr:
		return exprText(t.Fun) + "()"
	case *ast.IndexExpr:
		return exprText(t.X) + "[]"
	}
	return ""
}

// rootChain devolve (raiz, campos) de uma cadeia `root.f1.f2...`.
func rootChain(e ast.Expr) (string, []string) {
	var chain []string
	for {
		switch t := e.(type) {
		case *ast.SelectorExpr:
			chain = append([]string{t.Sel.Name}, chain...)
			e = t.X
			continue
		case *ast.Ident:
			return t.Name, chain
		}
		return "", nil
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
