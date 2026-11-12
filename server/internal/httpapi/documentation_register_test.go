package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
)

// A route documented twice refuses, so two features cannot both believe they
// own one description.
func TestARouteDocumentedTwicePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("documenting an existing route again did not panic")
		}
	}()
	documentRoutes(map[string]entry{"GET /api/description": {Summary: "again"}})
}

func TestDocumentRoutesAddsAnEntry(t *testing.T) {
	const probe = "GET /api/test/document-probe"
	t.Cleanup(func() { delete(documentation, probe) })

	documentRoutes(map[string]entry{probe: {Summary: "probe", Authentication: "none"}})
	if documentation[probe].Summary != "probe" {
		t.Fatal("a documented route was not recorded")
	}
}

// A feature that registers its routes has them mounted, ahead of the web
// application's catch-all.
func TestRegisteredRoutesAreMounted(t *testing.T) {
	const path = "/api/test/registered-probe"
	saved := routeRegistrars
	t.Cleanup(func() { routeRegistrars = saved })

	registerRoutes(func(s *Server) {
		s.echo.GET(path, func(c *echo.Context) error { return c.NoContent(http.StatusTeapot) })
	})

	recorder := httptest.NewRecorder()
	testServer(t).Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	if recorder.Code != http.StatusTeapot {
		t.Fatalf("a registered route answered %d, want %d", recorder.Code, http.StatusTeapot)
	}
}
