package extract

import (
	"fmt"
	"strings"

	"fpcount/internal/ifpug"
	"fpcount/internal/srcindex"
)

// Regras de classificação de rotas (tabela de decisão, ver README):
//
//	GET/HEAD  sem derivação  -> EQ
//	GET/HEAD  com derivação  -> EO
//	demais    com escrita    -> EI
//	demais    sem escrita, com derivação -> EO (baixa confiança se não houver escrita)
//	demais    sem escrita nem derivação  -> EI (baixa confiança)
const neutralDET = 10

// RouteRows converte as rotas lógicas do pacote httpapi em linhas de contagem.
func RouteRows(ix *srcindex.Index, dir string) ([]Row, int) {
	routes, dispatchers := Routes(ix, dir)
	var rows []Row
	n := 0
	for _, r := range routes {
		n++
		row := Row{ID: fmt.Sprintf("T%03d", n), Name: r.Method + " " + strings.TrimPrefix(r.Path, apiPrefix),
			Evidence: r.Evidence, Scope: Nucleo}
		if IsInfra(r.Path) {
			row.Scope = Infra
			row.Type = ifpug.EQ
			row.DET, row.Ref, row.Conf = 1, 1, Alta
			row.Note = "rota de infraestrutura (fora da contagem funcional)"
			row.Finish()
			rows = append(rows, row)
			continue
		}
		info := newInfo()
		for _, h := range r.Handlers {
			mergeInfo(info, Analyze(ix, dir, h))
		}
		classify(&row, r, info)
		row.Finish()
		rows = append(rows, row)
	}
	return rows, dispatchers
}

func mergeInfo(dst, src *Info) {
	for k := range src.InParams {
		dst.InParams[k] = true
	}
	for k := range src.InFields {
		dst.InFields[k] = true
	}
	for k := range src.OutFields {
		dst.OutFields[k] = true
	}
	for k := range src.Deps {
		dst.Deps[k] = true
	}
	for k := range src.WriteNames {
		dst.WriteNames[k] = true
	}
	dst.Writes = dst.Writes || src.Writes
	dst.Derived = dst.Derived || src.Derived
	dst.InKnown = dst.InKnown || src.InKnown
	dst.OutKnown = dst.OutKnown || src.OutKnown
}

func classify(row *Row, r *Route, info *Info) {
	read := r.Method == "GET" || r.Method == "HEAD"
	unambiguous := true
	switch {
	case read && !info.Derived:
		row.Type = ifpug.EQ
	case read && info.Derived:
		row.Type = ifpug.EO
	case info.Writes:
		row.Type = ifpug.EI
	case info.Derived:
		row.Type = ifpug.EO
		unambiguous = false
	default:
		row.Type = ifpug.EI
		unambiguous = false
	}
	in := len(info.InParams) + len(info.InFields)
	out := len(info.OutFields)
	row.Ref = len(info.Deps)
	satisfied := 0
	if unambiguous && len(r.Handlers) > 0 {
		satisfied++
	}
	if row.Ref > 0 {
		satisfied++
	} else {
		row.Ref = 1
		row.Defaulted = true
	}
	switch row.Type {
	case ifpug.EI:
		row.DET = in
		if in > 0 {
			satisfied++
		} else {
			row.DET = 1
			row.Defaulted = true
		}
	case ifpug.EQ:
		row.DET = max(in, out)
		if out > 0 {
			satisfied++
		} else {
			if row.DET == 0 {
				row.DET = neutralDET
			}
			row.Defaulted = true
		}
	case ifpug.EO:
		row.DET = in + out
		if out > 0 {
			satisfied++
		} else {
			row.DET += neutralDET
			row.Defaulted = true
		}
	}
	switch {
	case satisfied >= 3:
		row.Conf = Alta
	case satisfied == 2:
		row.Conf = Media
	default:
		row.Conf = Baixa
	}
	if len(r.Handlers) == 0 {
		row.Note = "handler não resolvido estaticamente"
	}
	if r.Origin != "Route{}" && row.Conf == Alta {
		row.Conf = Media // handler compartilhado entre operações despachadas por sufixo
		row.Note = "handler compartilhado por despacho de ação;"
	}
	row.Note = strings.TrimSpace(row.Note + fmt.Sprintf(" in=%d out=%d deps=%s perm=%s", in, out, strings.Join(sortedKeys(info.Deps), "+"), r.Perm))
}
