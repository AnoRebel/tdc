package httpapi

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/AnoRebel/tdc/server/internal/buildinfo"
)

/*
Which version of the client API a request asks for.

The API is served under /api/v1, and under /api as well. A client that pins
/api/v1 keeps working when a later release changes something it relies on:
that change ships as /api/v2, both are served for at least a release, and /api
follows the newest. A client that does not care follows /api.

Done before routing, by rewriting the path, so every route exists once in the
routing table and the description and the OpenAPI document describe it once.
A version this server does not serve is refused by name rather than with a
bare "not found", so the mismatch is legible to whoever hits it.
*/

// apiVersionHeader tells a caller which API version answered.
const apiVersionHeader = "TDC-API-Version"

var versionedPath = regexp.MustCompile(`^/api/v([0-9]+)(/.*)?$`)

func versioned(next http.Handler) http.Handler {
	current := "/api/v" + buildinfo.APIVersion
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if !strings.HasPrefix(path, "/api/") && path != "/api" {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set(apiVersionHeader, buildinfo.APIVersion)

		if match := versionedPath.FindStringSubmatch(path); match != nil {
			if match[1] != buildinfo.APIVersion {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"message":"this server serves API version ` + buildinfo.APIVersion +
					` at ` + current + `/, and the newest at /api/"}`))
				return
			}
			rewritten := r.Clone(r.Context())
			rewritten.URL.Path = "/api" + match[2]
			rewritten.URL.RawPath = ""
			if rewritten.URL.Path == "/api" {
				rewritten.URL.Path = "/api/"
			}
			next.ServeHTTP(w, rewritten)
			return
		}
		next.ServeHTTP(w, r)
	})
}
