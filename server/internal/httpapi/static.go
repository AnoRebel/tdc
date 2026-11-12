package httpapi

import (
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/labstack/echo/v5"
)

/*
mountWebApp serves the built web application from the same origin as the API.

Same origin is the point, not a convenience. The session cookie is set
HttpOnly and SameSite=Lax; served from a different host, a browser treats it as
third-party and declines to send it back, which is why development needs an
explicit allowed-origins list and production does not. One origin also means no
CORS preflight on every request and one port to expose.

Does nothing when the directory is not there, which is the development case:
the app runs from its own dev server, and the API is reached across origins
with the allowance that requires.
*/
func (s *Server) mountWebApp(root string) {
	if root == "" {
		return
	}

	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		s.log.Info("no web application to serve", "path", root)
		return
	}

	files := os.DirFS(root)

	// The shell must exist, or there is nothing to serve and every route would
	// answer with an error rather than an application.
	shell, err := fs.ReadFile(files, "index.html")
	if err != nil {
		s.log.Warn("a web root was configured but has no index.html", "path", root)
		return
	}

	/*
		Held in memory: it is a few kilobytes, served on every navigation, and
		rewritten per request to describe what is being shared.

		Read once, which means replacing the web root under a running server
		serves the old shell against the new assets until it is restarted. The
		shell carries the built runtime configuration, so the symptom is an
		application calling the address it was built with rather than the one
		it is served from — which looks like a networking fault and is not.
		Deploying the web root is therefore a reload or a restart.
	*/
	s.shell.Store(&shell)

	/*
		Everything no real route claimed.

		RouteNotFound rather than a catch-all GET: it runs only after routing
		has failed to find anything else, so a file in the web root cannot
		shadow an endpoint however it is named, and the ordering of this
		registration stops mattering.
	*/
	s.echo.RouteNotFound("/*", func(c *echo.Context) error {
		return s.serveWebFile(c, files)
	})

	s.log.Info("serving the web application", "path", root)
}

/*
serveWebFile serves one file, falling back to the application shell.

A single-page application owns its own routing: /rooms/abc is a route the
browser resolves, not a file on disk. A request for it has to return the shell
so the application can start and route it — which is why a miss is not a 404.

An API path that reaches here is a genuine 404 rather than the shell, because
returning HTML to something expecting JSON turns a clear missing-endpoint error
into a parse failure somewhere else.
*/
func (s *Server) serveWebFile(c *echo.Context, files fs.FS) error {
	request := c.Request()

	// Cleaned before use. A path with .. in it is resolved away rather than
	// walked, so a request cannot reach outside the web root.
	name := strings.TrimPrefix(path.Clean("/"+request.URL.Path), "/")

	if strings.HasPrefix(name, "api/") || name == "api" {
		return echo.NewHTTPError(http.StatusNotFound, "no such endpoint")
	}

	if name != "" {
		if info, err := fs.Stat(files, name); err == nil && !info.IsDir() {
			return s.serveAsset(c, files, name)
		}
	}

	/*
		Not a file: the application's own router will resolve it.

		Served from memory with social metadata inserted, rather than from
		disk, because a link shared outside this server previews from what a
		crawler reads — and a client-rendered application returns an empty
		shell to anything that does not run JavaScript.

		Never cached: the shell is shared by every route, and a cached copy
		would describe whichever page was fetched first. It also names the
		hashed assets for the version it was built with, so a stale one asks
		for files that no longer exist.
	*/
	return s.serveShellWithMeta(c, *s.shell.Load(), request.URL.Path)
}

/*
serveAsset returns one file with caching that matches how it is named.

Nuxt fingerprints what it builds: a file under the build directory has its
content hash in its name, so its content can never change and it is safe to
cache for a year. Anything else keeps the conservative default, because a name
that is not content-derived can point at different bytes tomorrow.
*/
func (s *Server) serveAsset(c *echo.Context, files fs.FS, name string) error {
	if strings.HasPrefix(name, "_nuxt/") {
		c.Response().Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}

	return c.FileFS(name, files)
}

// CleanWebPathForTest exposes the path cleaning to this package's tests.
//
// Exported for testing only. The cleaning is worth asserting on its own,
// because the filesystem also refuses "..": a test that only checks whether a
// file leaked passes with the cleaning removed, and would be measuring the
// wrong guard.
func CleanWebPathForTest(raw string) string {
	return strings.TrimPrefix(path.Clean("/"+raw), "/")
}
