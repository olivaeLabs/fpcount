package extract

import (
	"fmt"
	"go/ast"
	"go/token"
	"regexp"
	"sort"
	"strings"

	"fpcount/internal/ifpug"
	"fpcount/internal/srcindex"
)

// Papéis de processo (serve/api/worker/all) não são transações funcionais.
var cliRoles = map[string]bool{"serve": true, "api": true, "worker": true, "all": true, "help": true, "-h": true, "--help": true, "version": true}

var (
	reCLIRead = regexp.MustCompile(`(?i)(list|show|status|verify|get|read|check|dump|print|audit-?log|inspect|explain|report|diff|plan|dry)`)
	reFlagFn  = regexp.MustCompile(`^(String|Bool|Int|Int64|Uint|Float64|Duration|StringVar|BoolVar|IntVar|DurationVar|Var|Func)$`)
)

// CLIRows extrai os comandos da CLI de um pacote main (cmd/<bin>) como
// transações: cada folha da árvore de subcomandos (switch sobre args[0]/sub).
func CLIRows(ix *srcindex.Index, dir, bin, scope string) []Row {
	main := ix.Func(dir, "runMain")
	if main == nil {
		main = ix.Func(dir, "main")
	}
	if main == nil {
		return nil
	}
	var leaves []cliLeaf
	walkCLI(ix, dir, main, bin, nil, &leaves, 0, map[*ast.FuncDecl]bool{})
	if len(leaves) == 0 { // binário de comando único (sem subcomandos)
		leaves = append(leaves, cliLeaf{name: bin, flags: countFlags(main.Decl), evidence: ix.Pos(main.File, main.Decl.Pos())})
	}
	sort.Slice(leaves, func(i, j int) bool { return leaves[i].name < leaves[j].name })
	var rows []Row
	for i, l := range leaves {
		row := Row{ID: fmt.Sprintf("C-%s-%02d", bin, i+1), Name: l.name, Scope: scope,
			Evidence: l.evidence, Conf: Baixa}
		if reCLIRead.MatchString(l.name) {
			row.Type = ifpug.EQ
		} else {
			row.Type = ifpug.EI
		}
		row.DET = l.flags
		row.Ref = 2 // sem rastreio estático de ILF por comando: valor neutro
		row.Defaulted = true
		if row.DET == 0 {
			row.DET = neutralDET
		}
		row.Note = fmt.Sprintf("flags=%d; FTR neutro (2), DET neutro (10) se flags=0; tipo por nome do comando", l.flags)
		row.Finish()
		rows = append(rows, row)
	}
	return rows
}

type cliLeaf struct {
	name     string
	flags    int
	evidence string
}

func walkCLI(ix *srcindex.Index, dir string, fn *srcindex.Func, prefix string, path []string, out *[]cliLeaf, depth int, seen map[*ast.FuncDecl]bool) {
	if fn == nil || fn.Decl.Body == nil || depth > 3 || seen[fn.Decl] {
		return
	}
	seen[fn.Decl] = true
	found := false
	ast.Inspect(fn.Decl.Body, func(n ast.Node) bool {
		sw, ok := n.(*ast.SwitchStmt)
		if !ok || sw.Tag == nil || !isArgsTag(sw.Tag) {
			return true
		}
		for _, st := range sw.Body.List {
			cc, ok := st.(*ast.CaseClause)
			if !ok {
				continue
			}
			var names []string
			for _, e := range cc.List {
				if bl, ok := e.(*ast.BasicLit); ok && bl.Kind == token.STRING {
					names = append(names, strings.Trim(bl.Value, `"`))
				}
			}
			for _, nm := range names {
				if cliRoles[nm] {
					continue
				}
				found = true
				leaf := cliLeaf{name: prefix + " " + strings.Join(append(append([]string{}, path...), nm), " "),
					evidence: ix.Pos(fn.File, cc.Pos())}
				callee := calleeInClause(ix, dir, fn.File, cc)
				if callee != nil {
					before := len(*out)
					walkCLI(ix, dir, callee, prefix, append(append([]string{}, path...), nm), out, depth+1, seen)
					if len(*out) > before {
						continue
					}
					leaf.flags = countFlags(callee.Decl)
				}
				*out = append(*out, leaf)
			}
		}
		return false
	})
	_ = found
}

func isArgsTag(e ast.Expr) bool {
	switch t := e.(type) {
	case *ast.IndexExpr:
		switch x := t.X.(type) {
		case *ast.Ident:
			return x.Name == "args"
		case *ast.SelectorExpr:
			return x.Sel.Name == "Args"
		}
	case *ast.Ident:
		return t.Name == "sub" || t.Name == "cmd" || t.Name == "action" || t.Name == "subcmd"
	}
	return false
}

func calleeInClause(ix *srcindex.Index, dir string, f *srcindex.File, cc *ast.CaseClause) *srcindex.Func {
	var res *srcindex.Func
	for _, st := range cc.Body {
		ast.Inspect(st, func(n ast.Node) bool {
			if res != nil {
				return false
			}
			if c, ok := n.(*ast.CallExpr); ok {
				if id, ok := c.Fun.(*ast.Ident); ok && strings.HasPrefix(id.Name, "run") {
					res = ix.Func(dir, id.Name)
				}
			}
			return true
		})
	}
	return res
}

func countFlags(fd *ast.FuncDecl) int {
	names := map[string]bool{}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := c.Fun.(*ast.SelectorExpr)
		if !ok || !reFlagFn.MatchString(sel.Sel.Name) || len(c.Args) < 2 {
			return true
		}
		for _, a := range c.Args {
			if bl, ok := a.(*ast.BasicLit); ok && bl.Kind == token.STRING {
				names[strings.Trim(bl.Value, `"`)] = true
				break
			}
		}
		return true
	})
	return len(names)
}
