package httpapi

import (
	"net/http"
	"sort"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/buildinfo"
)

/*
A description of this API, generated from the routes themselves.

Written by walking the routing table rather than maintained beside it, because
a description maintained beside the code is a description that is wrong. The
question it answers is not "what did somebody write down" but "what does this
server actually serve" — and the two diverge on the first afternoon nobody has
time to update both.

OpenAPI is deliberately not what this produces. Generating a document that
claims to describe request and response bodies would mean either annotating
every handler or inferring shapes that are not there, and a schema that is
confidently wrong about a body is worse than no schema: somebody generates a
client from it and the errors arrive at runtime. What a routing table honestly
knows is which paths exist, which methods they take, what their parameters are
called, and what each one needs — and that is what this says.
*/

// Endpoint is one route, as the description reports it.
type Endpoint struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	// Parameters are the path's named segments, in the order they appear.
	Parameters []string `json:"parameters,omitempty"`
	// Summary says what the endpoint is for, from the documented catalogue.
	Summary string `json:"summary"`
	// Authentication says what a caller must carry: "none", "session", or
	// "token" where either works.
	Authentication string `json:"authentication"`
	// Permission names what the route requires beyond being signed in, when it
	// requires anything.
	Permission string `json:"permission,omitempty"`
}

// Description is the whole document.
type Description struct {
	Server    string     `json:"server"`
	Version   string     `json:"version"`
	Endpoints []Endpoint `json:"endpoints"`
}

/*
Describe generates the description from the routes this server serves.

Every route must appear in the catalogue below. A route added without an entry
is reported by Undocumented, which a test fails on — so the description cannot
fall behind the code without somebody being told.
*/
func (s *Server) Describe() Description {
	routes := s.echo.Router().Routes()

	endpoints := make([]Endpoint, 0, len(routes))
	for _, route := range routes {
		if !describable(route) {
			continue
		}

		documented := documentation[key(route.Method, route.Path)]
		endpoints = append(endpoints, Endpoint{
			Method:         route.Method,
			Path:           route.Path,
			Parameters:     route.Parameters,
			Summary:        documented.Summary,
			Authentication: documented.Authentication,
			Permission:     documented.Permission,
		})
	}

	sort.Slice(endpoints, func(i, j int) bool {
		if endpoints[i].Path != endpoints[j].Path {
			return endpoints[i].Path < endpoints[j].Path
		}
		return endpoints[i].Method < endpoints[j].Method
	})

	return Description{
		Server:    s.serverName(),
		Version:   buildinfo.Version,
		Endpoints: endpoints,
	}
}

/*
Undocumented returns the routes this server serves that nothing describes.

The point of the exercise. A generated document that silently omitted what it
did not understand would be a document that shrinks quietly as the API grows,
and nobody would notice until somebody tried to use it.
*/
func (s *Server) Undocumented() []string {
	var missing []string
	for _, route := range s.echo.Router().Routes() {
		if !describable(route) {
			continue
		}
		if _, ok := documentation[key(route.Method, route.Path)]; !ok {
			missing = append(missing, route.Method+" "+route.Path)
		}
	}
	sort.Strings(missing)
	return missing
}

// handleDescription serves the description.
//
// Public, because a client deciding whether it can talk to this server should
// not have to authenticate to find out what it could ask.
func (s *Server) handleDescription(c *echo.Context) error {
	return c.JSON(http.StatusOK, s.Describe())
}

// entry is what the catalogue records about one route.
type entry struct {
	Summary        string
	Authentication string
	Permission     string
}

func key(method, path string) string { return method + " " + path }

/*
describable reports whether a route is an endpoint somebody could call.

Echo registers a placeholder per group for the requests that match its prefix
and nothing else — they carry the group's path and a generated name, and
describing them would put a dozen entries in the document for routes that do
not exist. The web client's catch-all is not part of the API either.
*/
func describable(route echo.RouteInfo) bool {
	if !strings.HasPrefix(route.Path, "/api/") {
		return false
	}
	// The placeholder carries the sentinel in place of a method, which is
	// what distinguishes it from a route somebody registered.
	return route.Method != echo.RouteNotFound && route.Name != echo.RouteNotFound
}
