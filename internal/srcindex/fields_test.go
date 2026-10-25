package srcindex

import (
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T, files map[string]string) *Index {
	t.Helper()
	dir := t.TempDir()
	for name, src := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ix, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return ix
}

const modelSrc = `package model

import "time"

type Address struct {
	Street string ` + "`json:\"street\"`" + `
	City   string ` + "`json:\"city\"`" + `
}

type Person struct {
	ID      string    ` + "`json:\"id\" bson:\"_id\"`" + `
	Name    string    ` + "`json:\"name\"`" + `
	Secret  string    ` + "`json:\"-\"`" + `
	Home    Address   ` + "`json:\"home\"`" + `
	Others  []Address ` + "`json:\"others\"`" + `
	Born    time.Time ` + "`json:\"born\"`" + `
	Extra   map[string]any ` + "`json:\"extra\"`" + `
}
`

func TestLeafFieldsFlattensNestedAndSkipsDash(t *testing.T) {
	ix := fixture(t, map[string]string{"go.mod": "module m\n", "model/model.go": modelSrc})
	ts, f := ix.TypeDecl("model", "Person")
	if ts == nil {
		t.Fatal("Person não encontrado")
	}
	leaves, groups := ix.LeafFields(f, ts.Type)
	// id, name, home.street, home.city, others.street, others.city, born, extra
	if len(leaves) != 8 {
		t.Fatalf("DET = %d (%v), quer 8", len(leaves), leaves)
	}
	if groups != 2 { // Address (home e others) conta uma vez
		t.Fatalf("subgrupos = %d, quer 1 tipo distinto de subgrupo (Address) + raiz = 2", groups)
	}
}

func TestConstResolution(t *testing.T) {
	ix := fixture(t, map[string]string{"go.mod": "module m\n", "p/p.go": "package p\nconst XCollection = \"xs\"\n"})
	if v, ok := ix.Const("p", "XCollection"); !ok || v != "xs" {
		t.Fatalf("Const = %q %v", v, ok)
	}
}
