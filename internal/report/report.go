// Package report grava contagem.csv, sumario.json e memoria-de-calculo.md.
package report

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"fpcount/internal/extract"
	"fpcount/internal/ifpug"
)

// Scenario é um total de PF sob uma hipótese para as linhas de baixa confiança.
type Scenario struct {
	Name string `json:"cenario"`
	PF   int    `json:"pf"`
	Note string `json:"nota"`
}

// Input reúne tudo o que o relatório precisa.
type Input struct {
	Root       string
	Commit     string
	Rows       []extract.Row
	NoMergeILF int // PF das ILFs sem agrupamento de coleções (alternativa)
	Exclusions []string
	Dispatch   int
}

// Totals soma PF por escopo e tipo (somente linhas contadas).
func Totals(rows []extract.Row, scope string) (byType map[ifpug.Type]int, total, n, review int) {
	byType = map[ifpug.Type]int{}
	for _, r := range rows {
		if r.Scope != scope {
			continue
		}
		n++
		byType[r.Type] += r.PF
		total += r.PF
		if r.Review {
			review++
		}
	}
	return
}

// Sensitivity varia só as linhas de baixa confiança (ou com valor neutro).
func Sensitivity(rows []extract.Row, scope string, noMergeILF int) []Scenario {
	typ, low, high := 0, 0, 0
	for _, r := range rows {
		if r.Scope != scope {
			continue
		}
		typ += r.PF
		if r.Conf == extract.Baixa || r.Defaulted {
			low += ifpug.Weight(r.Type, ifpug.Baixa)
			high += ifpug.Weight(r.Type, ifpug.Alta)
		} else {
			low += r.PF
			high += r.PF
		}
	}
	sc := []Scenario{
		{"baixo", low, "linhas de baixa confiança ou com valor neutro na complexidade BAIXA"},
		{"tipico", typ, "complexidade calculada (valores neutros onde não extraível)"},
		{"alto", high, "linhas de baixa confiança ou com valor neutro na complexidade ALTA"},
	}
	if scope == extract.Nucleo && noMergeILF > 0 {
		var ilf int
		for _, r := range rows {
			if r.Scope == scope && r.Type == ifpug.ILF {
				ilf += r.PF
			}
		}
		sc = append(sc, Scenario{"tipico_sem_agrupar_ilf", typ - ilf + noMergeILF, "coleções MongoDB sem agrupar (cada coleção = 1 ILF)"})
	}
	return sc
}

// Write grava os três artefatos em out.
func Write(out string, in Input) error {
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(out, "contagem.csv"), in.Rows); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(out, "sumario.json"), in); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, "memoria-de-calculo.md"), []byte(memoria(in)), 0o644)
}

func writeCSV(path string, rows []extract.Row) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write([]string{"id", "escopo", "tipo", "nome", "DET", "RET_FTR", "complexidade", "PF", "confianca", "revisao_humana", "evidencia", "nota"})
	for _, r := range rows {
		_ = w.Write([]string{r.ID, r.Scope, string(r.Type), r.Name, strconv.Itoa(r.DET), strconv.Itoa(r.Ref),
			string(r.Comp), strconv.Itoa(r.PF), string(r.Conf), strconv.FormatBool(r.Review), r.Evidence, r.Note})
	}
	w.Flush()
	return w.Error()
}

func summary(in Input) map[string]any {
	scopes := map[string]any{}
	for _, sc := range []string{extract.Nucleo, extract.Satelite} {
		bt, total, n, rev := Totals(in.Rows, sc)
		types := map[string]int{}
		for k, v := range bt {
			types[string(k)] = v
		}
		scopes[sc] = map[string]any{"pf": total, "funcoes": n, "linhas_revisao_humana": rev,
			"pf_por_tipo": types, "sensibilidade": Sensitivity(in.Rows, sc, in.NoMergeILF)}
	}
	infra := 0
	for _, r := range in.Rows {
		if r.Scope == extract.Infra {
			infra++
		}
	}
	return map[string]any{"cpm": ifpug.CPMVersion, "commit": in.Commit, "escopos": scopes,
		"rotas_infra_fora_da_contagem": infra, "dispatchers_ignorados": in.Dispatch, "exclusoes": in.Exclusions}
}

func writeJSON(path string, in Input) error {
	b, err := json.MarshalIndent(summary(in), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func memoria(in Input) string {
	var b strings.Builder
	p := func(f string, a ...any) { fmt.Fprintf(&b, f, a...) }
	p("# Memória de cálculo — contagem de pontos de função (fpcount)\n\n")
	p("- Método: IFPUG CPM %s, pontos de função **não ajustados** (sem fator de ajuste).\n", ifpug.CPMVersion)
	p("- Tipo: contagem de **aplicação** (tamanho do sistema existente), gerada por análise estática (go/ast), sem LLM.\n")
	p("- Alvo: `%s` no commit `%s`.\n", filepath.Base(in.Root), in.Commit)
	p("- Status: **estimativa funcional preliminar automatizada**. Não é contagem certificada: a classificação em ILF/EIF/EI/EO/EQ e os DET/RET/FTR são aproximações determinísticas (tabelas abaixo); linhas com `revisao_humana=true` precisam de conferência manual.\n\n")
	p("## Fronteira\n- **Núcleo:** API HTTP (`internal/httpapi`, rotas lógicas sem o alias `/api/v1`), modelo de dados MongoDB, fontes externas lidas, jobs agendados e CLI `cmd/app`.\n")
	p("- **Satélites** (reportados separados, nunca somados ao núcleo): demais comandos e ferramentas informados.\n")
	p("- **Fora da contagem:** rotas de infraestrutura (listadas com PF=0), MCP (proxies finos dos mesmos caminhos da API), telas do front (expõem as mesmas transações), test-doubles, %d dispatchers `{action...}` (as operações lógicas vêm do registry).\n", in.Dispatch)
	for _, e := range in.Exclusions {
		p("  - %s\n", e)
	}
	p("\n## Totais\n\n| Escopo | Funções | PF | Linhas c/ revisão humana |\n|---|---:|---:|---:|\n")
	for _, sc := range []string{extract.Nucleo, extract.Satelite} {
		_, total, n, rev := Totals(in.Rows, sc)
		p("| %s | %d | %d | %d |\n", sc, n, total, rev)
	}
	p("\n> **Satélites:** o PF acima conta apenas pontos de entrada de CLI (análise estática sem API/ILF próprios). É um **piso**, não o tamanho do satélite; não some ao núcleo.\n")
	for _, sc := range []string{extract.Nucleo, extract.Satelite} {
		bt, total, _, _ := Totals(in.Rows, sc)
		p("\n### PF por tipo — %s (total %d)\n\n| Tipo | PF |\n|---|---:|\n", sc, total)
		var keys []string
		for k := range bt {
			keys = append(keys, string(k))
		}
		sort.Strings(keys)
		for _, k := range keys {
			p("| %s | %d |\n", k, bt[ifpug.Type(k)])
		}
		p("\n### Sensibilidade — %s\n\n| Cenário | PF | Hipótese |\n|---|---:|---|\n", sc)
		for _, s := range Sensitivity(in.Rows, sc, in.NoMergeILF) {
			p("| %s | %d | %s |\n", s.Name, s.PF, s.Note)
		}
	}
	p("\n## Tabelas de decisão (heurísticas)\n\n")
	p("**ILF (arquivos lógicos).** Cada constante `*Collection*` do módulo é uma coleção. Coleções filhas/auxiliares são agrupadas na principal (RET) conforme `DefaultMerges` (julgamento de modelagem: toda linha agrupada sai com revisão humana). DET = campos-folha distintos (tags `bson`/`json`) dos structs-documento, achatando structs aninhadas; RET = tipos-struct distintos (raiz + subgrupos). Estrutura-documento escolhida por casamento de nome (singular) com a coleção; sem casamento, vale o único documento-raiz do pacote; senão fica com valor neutro (baixa confiança).\n\n")
	p("**Rotas (EI/EO/EQ).** GET/HEAD sem derivação = EQ; GET com derivação (nome de método/função: score, compute, aggregate, report…) = EO; demais com escrita (métodos do tipo Create/Update/Save/Apply/Revoke…) = EI; sem escrita mas com derivação = EO; sem nenhum dos dois = EI (baixa confiança). DET de entrada = parâmetros de query/path + campos do corpo decodificado; DET de saída = campos da resposta de sucesso (`writeJSON`); EQ usa o maior dos dois, EO a soma, EI a entrada. FTR = dependências de dados distintas tocadas pelo handler (proxy: campos `Store/Repo/Service/...` do servidor e parâmetros injetados), mínimo 1. Operações despachadas por `{action...}` compartilham o handler (confiança no máximo média).\n\n")
	p("**Jobs agendados (EI).** Um por constante `*JobName`; FTR = dependências distintas no arquivo do job; DET neutro (10). **CLI.** Um por folha da árvore de subcomandos (switch sobre `args[0]`/`sub`); DET = flags declaradas na função-folha (neutro 10 se nenhuma); FTR neutro 2; EQ se o nome sugere leitura, senão EI.\n\n")
	p("**EIF.** Tabela `DefaultEIFs` (fontes externas lidas): DET/RET = structs `json` do pacote, sem tag `bson`. Semântica externa não verificável por código.\n\n")
	p("**Valores neutros / confiança.** Quando DET ou FTR não é extraível, usa-se valor neutro e a linha é marcada `baixa`; o cenário de sensibilidade troca essas linhas por complexidade baixa e alta.\n\n")
	p("## O que NÃO é verificável por código (limites da contagem)\n- Se cada agrupamento de coleções corresponde a um arquivo lógico reconhecido pelo usuário.\n- Se uma transação é processo elementar (unidade de significado) ou deve ser fundida/dividida.\n- Se o mesmo dado é de fato ILF (mantido) e não EIF, e quais referências de arquivo (FTR) cada transação realmente faz.\n- Regras de contagem do CPM para dados derivados, mensagens de erro e DET de controle de ação.\n")
	return b.String()
}
