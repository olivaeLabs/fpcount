// Comando fpcount: contagem de pontos de função IFPUG por análise estática.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"fpcount/internal/extract"
	"fpcount/internal/report"
	"fpcount/internal/srcindex"
)

const usage = `uso: fpcount count --root <repo> --out <dir> [--satellite <repo>]...

  count   conta PF (IFPUG CPM 4.3.1, não ajustados) e grava contagem.csv,
          memoria-de-calculo.md e sumario.json em --out. Somente leitura em --root.`

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

func main() {
	if len(os.Args) < 2 || os.Args[1] != "count" {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	fs := flag.NewFlagSet("count", flag.ExitOnError)
	root := fs.String("root", "", "raiz do repositório (módulo Go) a contar")
	out := fs.String("out", "", "diretório de saída")
	var sats multi
	fs.Var(&sats, "satellite", "repositório satélite (repetível)")
	_ = fs.Parse(os.Args[2:])
	if *root == "" || *out == "" {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	if err := run(*root, *out, sats); err != nil {
		fmt.Fprintln(os.Stderr, "fpcount:", err)
		os.Exit(1)
	}
}

func run(root, out string, sats []string) error {
	ix, err := srcindex.Load(root)
	if err != nil {
		return err
	}
	in := report.Input{Root: root, Commit: gitHead(root)}
	routes, dispatch := extract.RouteRows(ix, "internal/httpapi")
	in.Dispatch = dispatch
	in.Rows = append(in.Rows, routes...)
	in.Rows = append(in.Rows, extract.ILFRows(ix, extract.DefaultMerges, extract.DefaultExcludedCollections)...)
	in.Rows = append(in.Rows, extract.EIFRows(ix, extract.DefaultEIFs)...)
	in.Rows = append(in.Rows, extract.JobRows(ix)...)
	in.Rows = append(in.Rows, extract.CLIRows(ix, "cmd/app", "app", extract.Nucleo)...)
	for _, c := range []string{"app-reconcile", "seedcamp", "ci-affected"} {
		in.Rows = append(in.Rows, extract.CLIRows(ix, "cmd/"+c, c, extract.Satelite)...)
	}
	in.NoMergeILF = sumPF(extract.ILFRows(ix, nil, extract.DefaultExcludedCollections))
	in.Exclusions = append(in.Exclusions,
		"`cmd/mockd` (test-double de API externa) e coleções de teste (`legacy_src`, `outra`)",
		"`legacy_campaigns`/`campaigns` (origem legada da migração) entram como EIF, não como ILF")
	for _, s := range sats {
		six, err := srcindex.Load(s)
		if err != nil {
			return err
		}
		name := filepath.Base(s)
		matches, _ := filepath.Glob(filepath.Join(s, "cmd", "*"))
		for _, m := range matches {
			bin := name + "/" + filepath.Base(m)
			in.Rows = append(in.Rows, extract.CLIRows(six, "cmd/"+filepath.Base(m), bin, extract.Satelite)...)
		}
		in.Exclusions = append(in.Exclusions,
			fmt.Sprintf("satélite `%s` (commit `%s`): só CLI por análise estática; sem API HTTP/ILF/EIF próprios extraídos", name, gitHead(s)))
	}
	return report.Write(out, in)
}

func sumPF(rows []extract.Row) int {
	n := 0
	for _, r := range rows {
		n += r.PF
	}
	return n
}

func gitHead(dir string) string {
	b, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return "desconhecido"
	}
	return strings.TrimSpace(string(b))
}
