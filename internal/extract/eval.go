package extract

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"

	"fpcount/internal/srcindex"
)

// env é um escopo léxico simplificado: nome -> expressões alternativas
// (um `range` sobre um literal de slice produz uma alternativa por elemento).
type env struct {
	parent *env
	vars   map[string][]ast.Expr
}

func newEnv(p *env) *env { return &env{parent: p, vars: map[string][]ast.Expr{}} }

func (e *env) lookup(n string) ([]ast.Expr, bool) {
	for c := e; c != nil; c = c.parent {
		if v, ok := c.vars[n]; ok {
			return v, true
		}
	}
	return nil, false
}

var httpMethods = map[string]string{
	"MethodGet": "GET", "MethodPost": "POST", "MethodPut": "PUT",
	"MethodPatch": "PATCH", "MethodDelete": "DELETE", "MethodHead": "HEAD",
}

// evalStrings resolve uma expressão string para o conjunto de valores
// possíveis; ok=false quando alguma parte não é estática.
func evalStrings(ix *srcindex.Index, dir string, x ast.Expr, ev *env, depth int) ([]string, bool) {
	if depth > 8 {
		return nil, false
	}
	switch t := x.(type) {
	case *ast.BasicLit:
		if t.Kind == token.STRING {
			s, err := strconv.Unquote(t.Value)
			return []string{s}, err == nil
		}
	case *ast.ParenExpr:
		return evalStrings(ix, dir, t.X, ev, depth+1)
	case *ast.Ident:
		if alts, ok := ev.lookup(t.Name); ok {
			return evalAlts(ix, dir, alts, ev, depth)
		}
		if v, ok := ix.Const(dir, t.Name); ok {
			return []string{v}, true
		}
	case *ast.SelectorExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			if id.Name == "http" {
				if m, ok := httpMethods[t.Sel.Name]; ok {
					return []string{m}, true
				}
			}
			if alts, ok := ev.lookup(id.Name); ok { // route.campo (literal struct)
				var vals []ast.Expr
				for _, a := range alts {
					cl, ok := a.(*ast.CompositeLit)
					if !ok {
						return nil, false
					}
					found := false
					for _, el := range cl.Elts {
						if kv, ok := el.(*ast.KeyValueExpr); ok {
							if k, ok := kv.Key.(*ast.Ident); ok && k.Name == t.Sel.Name {
								vals = append(vals, kv.Value)
								found = true
							}
						}
					}
					if !found {
						return nil, false
					}
				}
				return evalAlts(ix, dir, vals, ev, depth)
			}
		}
	case *ast.BinaryExpr:
		if t.Op != token.ADD {
			return nil, false
		}
		l, ok1 := evalStrings(ix, dir, t.X, ev, depth+1)
		r, ok2 := evalStrings(ix, dir, t.Y, ev, depth+1)
		if !ok1 || !ok2 {
			return nil, false
		}
		var out []string
		for _, a := range l {
			for _, b := range r {
				out = append(out, a+b)
			}
		}
		return out, true
	}
	return nil, false
}

func evalAlts(ix *srcindex.Index, dir string, alts []ast.Expr, ev *env, depth int) ([]string, bool) {
	var out []string
	for _, a := range alts {
		v, ok := evalStrings(ix, dir, a, ev, depth+1)
		if !ok {
			return nil, false
		}
		out = append(out, v...)
	}
	return out, true
}

// bindRange liga as variáveis de um `for k, v := range <literal>`.
func bindRange(rs *ast.RangeStmt, ev *env) {
	x := rs.X
	if id, ok := x.(*ast.Ident); ok {
		if alts, ok := ev.lookup(id.Name); ok && len(alts) == 1 {
			x = alts[0]
		}
	}
	cl, ok := x.(*ast.CompositeLit)
	if !ok {
		return
	}
	if v, ok := rs.Value.(*ast.Ident); ok && v.Name != "_" {
		ev.vars[v.Name] = append([]ast.Expr(nil), cl.Elts...)
	}
}

func lowerFirst(s string) string { return strings.ToLower(s) }
