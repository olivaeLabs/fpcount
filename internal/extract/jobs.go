package extract

import (
	"fmt"
	"go/ast"
	"sort"
	"strings"

	"fpcount/internal/ifpug"
	"fpcount/internal/srcindex"
)

// JobRows gera uma EI por job agendado (constante `*JobName`): processo em
// lote que recebe dados externos e mantém ILFs. DET/FTR não são deriváveis de
// forma confiável: FTR = nº de dependências distintas no arquivo do job; DET
// neutro (10). Sempre baixa confiança, com revisão humana.
func JobRows(ix *srcindex.Index) []Row {
	type job struct {
		name string
		decl srcindex.ConstDecl
	}
	var jobs []job
	seen := map[string]bool{}
	for _, dir := range ix.Dirs() {
		for _, c := range ix.ConstDecls(dir) {
			ln := strings.ToLower(c.Name)
			if (strings.HasSuffix(ln, "jobname")) && !seen[c.Value] {
				seen[c.Value] = true
				jobs = append(jobs, job{c.Value, c})
			}
		}
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].name < jobs[j].name })
	var rows []Row
	for i, j := range jobs {
		deps := map[string]bool{}
		for _, f := range ix.Dir(j.decl.File.Dir) {
			if f != j.decl.File {
				continue
			}
			ast.Inspect(f.AST, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok && reDep.MatchString(sel.Sel.Name) {
					deps[sel.Sel.Name] = true
				}
				return true
			})
		}
		row := Row{ID: fmt.Sprintf("J%03d", i+1), Type: ifpug.EI, Name: "job agendado " + j.name, Scope: Nucleo,
			DET: neutralDET, Ref: max(len(deps), 1), Conf: Baixa, Defaulted: true,
			Evidence: fmt.Sprintf("%s:%d", j.decl.File.Rel, j.decl.Line),
			Note:     fmt.Sprintf("FTR=%d dependências distintas no arquivo do job; DET neutro", len(deps))}
		row.Finish()
		rows = append(rows, row)
	}
	return rows
}
