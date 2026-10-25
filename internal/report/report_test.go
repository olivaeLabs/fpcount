package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fpcount/internal/extract"
	"fpcount/internal/ifpug"
)

func rows() []extract.Row {
	return []extract.Row{
		{ID: "T001", Type: ifpug.EI, Scope: extract.Nucleo, PF: 4, Comp: ifpug.Media, Conf: extract.Alta},
		{ID: "T002", Type: ifpug.EQ, Scope: extract.Nucleo, PF: 4, Comp: ifpug.Media, Conf: extract.Baixa, Review: true},
		{ID: "T003", Type: ifpug.EQ, Scope: extract.Infra, PF: 0},
		{ID: "C001", Type: ifpug.EI, Scope: extract.Satelite, PF: 3, Conf: extract.Baixa, Review: true},
	}
}

func TestTotalsIgnoreInfraAndSeparateScopes(t *testing.T) {
	_, total, n, rev := Totals(rows(), extract.Nucleo)
	if total != 8 || n != 2 || rev != 1 {
		t.Fatalf("nucleo = %d/%d/%d, quer 8/2/1", total, n, rev)
	}
	if _, total, _, _ := Totals(rows(), extract.Satelite); total != 3 {
		t.Fatalf("satelite = %d, quer 3 (nunca somado ao núcleo)", total)
	}
}

func TestSensitivityVariesOnlyLowConfidenceRows(t *testing.T) {
	sc := Sensitivity(rows(), extract.Nucleo, 0)
	got := map[string]int{}
	for _, s := range sc {
		got[s.Name] = s.PF
	}
	// T001 fixa (4); T002 baixa: EQ baixa=3, típica=4, alta=6
	if got["baixo"] != 4+3 || got["tipico"] != 8 || got["alto"] != 4+6 {
		t.Fatalf("cenários = %v", got)
	}
}

func TestWriteIsDeterministic(t *testing.T) {
	in := Input{Root: "/x/repo", Commit: "abc", Rows: rows()}
	a, b := t.TempDir(), t.TempDir()
	if err := Write(a, in); err != nil {
		t.Fatal(err)
	}
	if err := Write(b, in); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"contagem.csv", "sumario.json", "memoria-de-calculo.md"} {
		x, _ := os.ReadFile(filepath.Join(a, f))
		y, _ := os.ReadFile(filepath.Join(b, f))
		if string(x) != string(y) || len(x) == 0 {
			t.Fatalf("%s não é determinístico ou está vazio", f)
		}
	}
	m, _ := os.ReadFile(filepath.Join(a, "memoria-de-calculo.md"))
	if !strings.Contains(string(m), "limites da contagem") || !strings.Contains(string(m), "4.3.1") {
		t.Fatal("memória deve declarar versão do CPM e os limites da contagem")
	}
}
