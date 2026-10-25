package extract

import (
	"fmt"
	"go/ast"
	"strings"

	"fpcount/internal/ifpug"
	"fpcount/internal/srcindex"
)

// EIFSpec declara uma fonte externa lida (não mantida) pelo sistema.
type EIFSpec struct {
	Name string
	Dir  string
}

// DefaultEIFs é uma tabela de EXEMPLO (ajuste para o seu projeto) das fontes externas do núcleo de decisão das fontes externas do núcleo (cada uma é
// um arquivo lógico externo referenciado, não mantido). A lista é julgamento
// de fronteira; DET/RET vêm dos structs `json` do pacote (payload externo).
var DefaultEIFs = []EIFSpec{
	{"API de parceiro A (coletor)", "internal/collectors/partnera"},
	{"API de parceiro B (coletor)", "internal/collectors/partnerb"},
	{"API de anúncios (campanhas e métricas)", "internal/adsapi"},
	{"API de vídeo (mineração)", "internal/videomine"},
	{"Fonte externa de câmbio", "internal/fx"},
	{"Páginas de política de fornecedores (leitura)", "internal/vendorpolicy"},
	{"Provedores de LLM", "internal/llm"},
	{"Identidade Google (OIDC)", "internal/auth"},
	{"Origem legada de campanhas (migração)", "internal/migrate"},
}

// EIFRows gera uma linha EIF por fonte externa.
func EIFRows(ix *srcindex.Index, specs []EIFSpec) []Row {
	var rows []Row
	for i, sp := range specs {
		leaves := map[string]bool{}
		keys := map[string]bool{}
		var evidence string
		for _, f := range ix.Dir(sp.Dir) {
			if sp.Dir == "internal/auth" && !strings.Contains(f.Rel, "google") {
				continue
			}
			if sp.Dir == "internal/migrate" && !strings.Contains(f.Rel, "legacy") && !strings.Contains(f.Rel, "migrate.go") {
				continue
			}
			for _, d := range f.AST.Decls {
				gd, ok := d.(*ast.GenDecl)
				if !ok {
					continue
				}
				for _, s := range gd.Specs {
					ts, ok := s.(*ast.TypeSpec)
					if !ok {
						continue
					}
					st, ok := ts.Type.(*ast.StructType)
					if !ok || !hasJSONOnly(st) {
						continue
					}
					l, k := ix.LeafFieldsKeys(f, ts.Name)
					for _, x := range l {
						leaves[ts.Name.Name+"."+x] = true
					}
					for _, x := range k {
						keys[x] = true
					}
					if evidence == "" {
						evidence = ix.Pos(f, ts.Pos())
					}
				}
			}
		}
		row := Row{ID: fmt.Sprintf("E%03d", i+1), Type: ifpug.EIF, Name: sp.Name, Scope: Nucleo,
			DET: len(leaves), Ref: len(keys), Evidence: evidence, Conf: Baixa,
			Note: "DET/RET = structs json do pacote; semântica externa não verificável por código"}
		if row.DET == 0 {
			row.DET, row.Ref, row.Defaulted = 10, 1, true
			row.Evidence = sp.Dir
		}
		row.Finish()
		row.Review = true
		rows = append(rows, row)
	}
	return rows
}

func hasJSONOnly(st *ast.StructType) bool {
	hasJSON := false
	for _, f := range st.Fields.List {
		if f.Tag == nil {
			continue
		}
		if strings.Contains(f.Tag.Value, `bson:"`) {
			return false
		}
		if strings.Contains(f.Tag.Value, `json:"`) {
			hasJSON = true
		}
	}
	return hasJSON
}
