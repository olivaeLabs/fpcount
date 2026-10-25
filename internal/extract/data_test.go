package extract

import (
	"os"
	"path/filepath"
	"testing"

	"fpcount/internal/ifpug"
	"fpcount/internal/srcindex"
)

func fixtureIndex(t *testing.T, files map[string]string) *srcindex.Index {
	t.Helper()
	dir := t.TempDir()
	for name, src := range files {
		p := filepath.Join(dir, name)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ix, err := srcindex.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return ix
}

const repoSrc = "package repo\n" +
	"const ParentCollection = \"parents\"\n" +
	"const KidCollection = \"kids\"\n" +
	"const SoloCollection = \"solos\"\n" +
	"type Parent struct { ID string `bson:\"_id\"`; Name string `bson:\"name\"` }\n" +
	"type Kid struct { ID string `bson:\"_id\"`; ParentID string `bson:\"parent_id\"`; Qty int `bson:\"qty\"` }\n" +
	"type Solo struct { ID string `bson:\"_id\"` }\n"

func TestILFRowsGroupsChildIntoParent(t *testing.T) {
	ix := fixtureIndex(t, map[string]string{"go.mod": "module m\n", "repo/repo.go": repoSrc})
	rows := ILFRows(ix, map[string]string{"kids": "parents"}, nil)
	if len(rows) != 2 {
		t.Fatalf("linhas = %d, quer 2 (parents+children, solos)", len(rows))
	}
	var parent *Row
	for i := range rows {
		if rows[i].Name == "parents" {
			parent = &rows[i]
		}
	}
	if parent == nil {
		t.Fatal("grupo parents ausente")
	}
	if parent.Type != ifpug.ILF || parent.Ref != 2 { // Parent + Child = 2 RET
		t.Fatalf("parent = %+v, quer ILF com RET=2", parent)
	}
	if parent.DET != 4 { // união de nomes distintos: _id, name, parent_id, qty
		t.Fatalf("DET = %d, quer 4", parent.DET)
	}
	if !parent.Review {
		t.Fatal("grupo com agregação deve exigir revisão humana")
	}
}

func TestILFRowsExcludesNamedCollections(t *testing.T) {
	ix := fixtureIndex(t, map[string]string{"go.mod": "module m\n", "repo/repo.go": repoSrc})
	rows := ILFRows(ix, nil, map[string]bool{"solos": true})
	for _, r := range rows {
		if r.Name == "solos" {
			t.Fatal("coleção excluída não deve gerar linha")
		}
	}
}

func TestEIFRowsUsesJSONOnlyStructs(t *testing.T) {
	src := "package ext\n" +
		"type Payload struct { A string `json:\"a\"`; B int `json:\"b\"` }\n" +
		"type Stored struct { A string `json:\"a\" bson:\"a\"` }\n"
	ix := fixtureIndex(t, map[string]string{"go.mod": "module m\n", "ext/ext.go": src})
	rows := EIFRows(ix, []EIFSpec{{Name: "fonte", Dir: "ext"}})
	if len(rows) != 1 || rows[0].Type != ifpug.EIF || rows[0].DET != 2 || !rows[0].Review {
		t.Fatalf("rows = %+v, quer EIF com DET=2 (Stored tem bson e fica de fora) e revisão", rows)
	}
}
