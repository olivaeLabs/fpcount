package extract

import (
	"fmt"
	"go/ast"
	"sort"
	"strings"

	"fpcount/internal/ifpug"
	"fpcount/internal/srcindex"
)

// DefaultMerges é uma tabela de EXEMPLO (ajuste para o seu projeto) de decisão de agrupamento de coleções MongoDB em
// arquivos lógicos (ILF). Regra: coleção filha/histórico/auxiliar que nunca é
// mantida de forma independente da principal vira RET dela. É julgamento de
// modelagem, não dedução de código: toda linha agrupada sai com revisão humana.
var DefaultMerges = map[string]string{
	"order_state_history": "order_state",
	"lead_items":          "leads",
	"filter_parameters":   "filters",
	"auth_login_attempts": "auth_sessions",
	"token_prefixes":      "api_tokens",
	"scheduler_locks":     "scheduler_runs",
	"policy_products":     "policy_pages",
}

// DefaultExcludedCollections: coleções que não são arquivos lógicos do usuário
// (origem legada lida pela migração: tratada como EIF em outra função).
var DefaultExcludedCollections = map[string]bool{
	"legacy_src": true, "legacy_campaigns": true, "campaigns": true,
}

type collection struct {
	name  string
	decl  srcindex.ConstDecl
	roots []rootDoc
	exact bool // nome da coleção casou com o nome do struct
}

type rootDoc struct {
	file *srcindex.File
	spec *ast.TypeSpec
}

// ILFRows gera uma linha ILF por grupo de coleções.
func ILFRows(ix *srcindex.Index, merges map[string]string, exclude map[string]bool) []Row {
	var cols []*collection
	byName := map[string]*collection{}
	for _, dir := range ix.Dirs() {
		for _, c := range ix.ConstDecls(dir) {
			if !strings.Contains(c.Name, "Collection") || c.Value == "" || exclude[c.Value] {
				continue
			}
			if _, dup := byName[c.Value]; dup {
				continue
			}
			col := &collection{name: c.Value, decl: c}
			col.roots, col.exact = docRoots(ix, c)
			cols = append(cols, col)
			byName[c.Value] = col
		}
	}
	sort.Slice(cols, func(i, j int) bool { return cols[i].name < cols[j].name })

	groups := map[string][]*collection{}
	var order []string
	for _, c := range cols {
		parent := c.name
		if p, ok := merges[c.name]; ok {
			if _, exists := byName[p]; exists {
				parent = p
			}
		}
		if _, ok := groups[parent]; !ok {
			order = append(order, parent)
		}
		groups[parent] = append(groups[parent], c)
	}
	sort.Strings(order)

	var rows []Row
	for i, g := range order {
		members := groups[g]
		leaves := map[string]bool{}
		keys := map[string]bool{}
		conf := Alta
		var names []string
		for _, m := range members {
			names = append(names, m.name)
			if len(m.roots) == 0 {
				conf = Baixa
				continue
			}
			if !m.exact {
				conf = lower(conf, Media)
			}
			for _, r := range m.roots {
				l, k := ix.LeafFieldsKeys(r.file, r.spec.Name)
				for _, x := range l {
					leaves[x] = true
				}
				for _, x := range k {
					keys[x] = true
				}
			}
		}
		row := Row{ID: fmt.Sprintf("D%03d", i+1), Type: ifpug.ILF, Name: g, Scope: Nucleo,
			DET: len(leaves), Ref: len(keys), Evidence: ix.Pos(members[0].decl.File, 0)}
		row.Evidence = fmt.Sprintf("%s:%d", members[0].decl.File.Rel, members[0].decl.Line)
		if row.DET == 0 {
			row.DET, row.Ref, row.Defaulted = 5, 1, true
			conf = Baixa
		}
		if len(members) > 1 {
			conf = lower(conf, Media)
			row.Note = "agrupa: " + strings.Join(names, " + ")
		}
		row.Conf = conf
		row.Finish()
		if len(members) > 1 {
			row.Review = true // agrupamento é julgamento de modelagem
		}
		rows = append(rows, row)
	}
	return rows
}

func lower(a, b Conf) Conf {
	rank := map[Conf]int{Alta: 2, Media: 1, Baixa: 0}
	if rank[a] <= rank[b] {
		return a
	}
	return b
}

// docRoots acha os structs-documento da coleção: structs com tag bson no
// arquivo que declara a constante; casa pelo nome (singular) quando há várias
// coleções no mesmo arquivo.
func docRoots(ix *srcindex.Index, c srcindex.ConstDecl) ([]rootDoc, bool) {
	var all []rootDoc
	for _, f := range ix.Dir(c.File.Dir) {
		for _, d := range f.AST.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, sp := range gd.Specs {
				ts, ok := sp.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if st, ok := ts.Type.(*ast.StructType); ok && hasBSON(st) {
					all = append(all, rootDoc{file: f, spec: ts})
				}
			}
		}
	}
	// raízes: structs não referenciados como tipo de campo de outro struct do pacote
	referenced := map[string]bool{}
	for _, r := range all {
		st := r.spec.Type.(*ast.StructType)
		for _, fld := range st.Fields.List {
			ast.Inspect(fld.Type, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok {
					referenced[id.Name] = true
				}
				return true
			})
		}
	}
	var roots []rootDoc
	for _, r := range all {
		if !referenced[r.spec.Name.Name] {
			roots = append(roots, r)
		}
	}
	want := normalize(c.Value)
	var exact []rootDoc
	for _, r := range all { // casamento por nome vale também para tipos referenciados
		if normalize(r.spec.Name.Name) == want {
			exact = append(exact, r)
		}
	}
	if len(exact) > 0 {
		return exact, true
	}
	if len(packageCollections(ix, c.File.Dir)) == 1 {
		return roots, true // único documento-raiz do pacote
	}
	var local []rootDoc // vários documentos no pacote e nenhum casou por nome
	for _, r := range roots {
		if r.file == c.File {
			local = append(local, r)
		}
	}
	return local, false
}

func packageCollections(ix *srcindex.Index, dir string) map[string]bool {
	out := map[string]bool{}
	for _, d := range ix.ConstDecls(dir) {
		if strings.Contains(d.Name, "Collection") {
			out[d.Value] = true
		}
	}
	return out
}

func normalize(s string) string {
	s = strings.ToLower(strings.ReplaceAll(s, "_", ""))
	return strings.TrimSuffix(s, "s")
}

func hasBSON(st *ast.StructType) bool {
	for _, f := range st.Fields.List {
		if f.Tag != nil && strings.Contains(f.Tag.Value, `bson:"`) {
			return true
		}
	}
	return false
}
