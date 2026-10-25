package extract

import (
	"testing"

	"fpcount/internal/ifpug"
)

const httpFixture = `package httpapi

import (
	"encoding/json"
	"net/http"
)

type Route struct {
	Method, Path, Permission string
	Handler                  http.HandlerFunc
}

type Server struct {
	store  Store
	logger Logger
	routes Registry
}

type thing struct {
	ID   string ` + "`json:\"id\"`" + `
	Name string ` + "`json:\"name\"`" + `
}

type listResp struct {
	Items []thing ` + "`json:\"items\"`" + `
	Total int     ` + "`json:\"total\"`" + `
}

type createReq struct {
	Name string ` + "`json:\"name\"`" + `
	Kind string ` + "`json:\"kind\"`" + `
}

func (s *Server) Register(registry Registry) {
	routes := []Route{
		{Method: http.MethodGet, Path: "/things", Permission: "x:read", Handler: s.handleList},
		{Method: http.MethodPost, Path: "/things", Permission: "x:write", Handler: s.handleCreate},
		{Method: http.MethodGet, Path: "/healthz", Handler: s.handleList},
	}
	_ = routes
	for _, prefix := range []string{"", "/api/v1"} {
		registry.Register(http.MethodPost, prefix+"/things/{id}:archive", "x:write")
	}
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	_ = q.Get("page")
	items := s.store.List()
	s.logger.Info("x")
	writeJSON(w, r, http.StatusOK, listResp{Items: items, Total: 1})
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req createReq
	_ = json.NewDecoder(r.Body).Decode(&req)
	s.store.Create(req)
	writeJSON(w, r, http.StatusCreated, thing{ID: "1"})
}
`

func TestRouteRowsClassifiesAndCounts(t *testing.T) {
	ix := fixtureIndex(t, map[string]string{"go.mod": "module m\n", "internal/httpapi/routes.go": httpFixture})
	rows, _ := RouteRows(ix, "internal/httpapi")
	by := map[string]Row{}
	for _, r := range rows {
		by[r.Name] = r
	}
	if len(rows) != 4 { // /things GET+POST, /healthz, :archive (alias /api/v1 deduplicado)
		t.Fatalf("rotas = %d (%v), quer 4", len(rows), names(rows))
	}
	get := by["GET /things"]
	if get.Type != ifpug.EQ || get.DET != 3 || get.Ref != 1 || get.PF != 3 { // out: items.id, items.name, total
		t.Errorf("GET /things = %+v", get)
	}
	post := by["POST /things"]
	if post.Type != ifpug.EI || post.DET != 2 || post.Ref != 1 || post.PF != 3 {
		t.Errorf("POST /things = %+v", post)
	}
	if h := by["GET /healthz"]; h.Scope != Infra || h.PF != 0 {
		t.Errorf("/healthz deve ser infra com PF 0: %+v", h)
	}
	if a, ok := by["POST /things/{id}:archive"]; !ok || a.Scope != Nucleo {
		t.Errorf("rota registrada via registry em loop não encontrada: %v", names(rows))
	}
}

func TestLoggerFieldIsNotAFileReference(t *testing.T) {
	ix := fixtureIndex(t, map[string]string{"go.mod": "module m\n", "internal/httpapi/routes.go": httpFixture})
	rows, _ := RouteRows(ix, "internal/httpapi")
	for _, r := range rows {
		if r.Name == "GET /things" && r.Ref != 1 {
			t.Fatalf("logger contou como FTR: %+v", r)
		}
	}
}

func names(rows []Row) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.Name)
	}
	return out
}
