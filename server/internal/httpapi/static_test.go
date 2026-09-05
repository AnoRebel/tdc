package httpapi_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AnoRebel/tdc/server/internal/httpapi"
)

// webRoot builds a directory shaped like a Nuxt build.
func webRoot(t *testing.T) string {
	t.Helper()

	root := t.TempDir()

	write := func(name, content string) {
		full := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("preparing %s: %v", name, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}

	write("index.html", "<!doctype html><title>TDC</title><div id=app></div>")
	write("_nuxt/entry.a1b2c3.js", "console.log('app')")
	write("favicon.ico", "icon-bytes")

	// A file outside the root, to prove traversal cannot reach it.
	outside := filepath.Join(filepath.Dir(root), "outside-the-root.txt")
	if err := os.WriteFile(outside, []byte("SECRET"), 0o644); err != nil {
		t.Fatalf("writing the outside file: %v", err)
	}
	t.Cleanup(func() { os.Remove(outside) })

	return root
}

// TestWebAppIsServed covers the ordinary case: the shell and its assets.
func TestWebAppIsServed(t *testing.T) {
	handler := newTestAPIWithWebRoot(t, webRoot(t))

	recorder := call(t, handler, http.MethodGet, "/", nil, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("the root returned %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "<div id=app>") {
		t.Errorf("the root did not return the application shell: %s",
			truncate(recorder.Body.String()))
	}

	asset := call(t, handler, http.MethodGet, "/_nuxt/entry.a1b2c3.js", nil, "")
	if asset.Code != http.StatusOK {
		t.Errorf("a built asset returned %d", asset.Code)
	}
}

/*
TestApplicationRoutesReturnTheShell is what makes a single-page application
work when somebody arrives at a deep link.

/rooms/abc is a route the browser resolves, not a file on disk. Returning 404
for it would mean a shared link, a refresh, or a bookmark all landed on an
error page rather than the conversation.
*/
func TestApplicationRoutesReturnTheShell(t *testing.T) {
	handler := newTestAPIWithWebRoot(t, webRoot(t))

	for _, path := range []string{"/rooms/abc", "/settings", "/signin", "/a/deep/route"} {
		recorder := call(t, handler, http.MethodGet, path, nil, "")

		if recorder.Code != http.StatusOK {
			t.Errorf("%s returned %d, expected the application shell", path, recorder.Code)
			continue
		}
		if !strings.Contains(recorder.Body.String(), "<div id=app>") {
			t.Errorf("%s did not return the shell", path)
		}
	}
}

/*
TestMissingApiEndpointsStayJson keeps a wrong URL diagnosable.

An unknown /api path must not fall through to the application shell. A client
expecting JSON that receives HTML reports a parse error somewhere unrelated,
which is far harder to trace than a plain 404.
*/
func TestMissingApiEndpointsStayJson(t *testing.T) {
	handler := newTestAPIWithWebRoot(t, webRoot(t))

	for _, path := range []string{"/api/nonexistent", "/api/rooms/not-a-route/nope", "/api"} {
		recorder := call(t, handler, http.MethodGet, path, nil, "")

		if recorder.Code == http.StatusOK {
			t.Errorf("%s returned 200; an unknown endpoint must not serve the app", path)
		}
		if strings.Contains(recorder.Body.String(), "<div id=app>") {
			t.Errorf("%s returned the application shell instead of an error", path)
		}
	}
}

/*
TestRealEndpointsAreNotShadowed is why the catch-all is registered last.

A file named like an endpoint must never be served in its place — otherwise
anybody who can put a file in the web root can replace an API response.
*/
func TestRealEndpointsAreNotShadowed(t *testing.T) {
	root := webRoot(t)

	// A file that would shadow the health endpoint if files won.
	if err := os.MkdirAll(filepath.Join(root, "api"), 0o755); err != nil {
		t.Fatalf("preparing: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "api", "health"),
		[]byte("NOT THE REAL HEALTH RESPONSE"), 0o644); err != nil {
		t.Fatalf("writing: %v", err)
	}

	handler := newTestAPIWithWebRoot(t, root)

	recorder := call(t, handler, http.MethodGet, "/api/health", nil, "")
	if strings.Contains(recorder.Body.String(), "NOT THE REAL") {
		t.Error("a file in the web root shadowed a real endpoint")
	}
	if recorder.Code != http.StatusOK {
		t.Errorf("the health endpoint returned %d", recorder.Code)
	}
}

/*
TestTraversalCannotEscapeTheWebRoot is the rule that keeps the rest of the
filesystem out of reach.

Serving files by a path a caller supplies is exactly where directory traversal
lives, and a server that reads /etc/passwd because somebody asked nicely is the
worst outcome this file can produce.

Two things enforce it, and the test checks both because either alone would be
one refactor away from being the only one. os.DirFS refuses a path containing
"..", and the path is cleaned before it is used so that nothing containing ".."
reaches the filesystem in the first place.
*/
func TestTraversalCannotEscapeTheWebRoot(t *testing.T) {
	handler := newTestAPIWithWebRoot(t, webRoot(t))

	attempts := []string{
		"/../outside-the-root.txt",
		"/../../outside-the-root.txt",
		"/_nuxt/../../outside-the-root.txt",
		"/%2e%2e/outside-the-root.txt",
		"/..%2foutside-the-root.txt",
		"/....//outside-the-root.txt",
	}

	for _, path := range attempts {
		recorder := callRawPath(t, handler, http.MethodGet, path, nil, "")

		if strings.Contains(recorder.Body.String(), "SECRET") {
			t.Errorf("%s read a file outside the web root", path)
		}
	}
}

/*
TestPathsAreCleanedBeforeUse checks the first of those two protections
directly.

The filesystem refusing ".." is what makes traversal fail today, so a test that
only asks whether the secret leaked passes with the cleaning removed — it is
measuring the wrong guard. This asserts the cleaning itself, so that removing
it fails here rather than silently leaving one protection where there were two.
*/
func TestPathsAreCleanedBeforeUse(t *testing.T) {
	cases := map[string]string{
		"/":                    "",
		"/index.html":          "index.html",
		"/_nuxt/entry.js":      "_nuxt/entry.js",
		"/../outside.txt":      "outside.txt",
		"/a/../b/c.js":         "b/c.js",
		"/./favicon.ico":       "favicon.ico",
		"/_nuxt/../../out.txt": "out.txt",
	}

	for raw, want := range cases {
		if got := httpapi.CleanWebPathForTest(raw); got != want {
			t.Errorf("cleaning %q gave %q, want %q", raw, got, want)
		}
	}
}

// TestNoWebRootLeavesTheApiAlone covers development, where the application
// runs from its own server and there is nothing to serve.
func TestNoWebRootLeavesTheApiAlone(t *testing.T) {
	handler := newTestAPI(t)

	// Still answers, and does not pretend to have an application.
	health := call(t, handler, http.MethodGet, "/api/health", nil, "")
	if health.Code != http.StatusOK {
		t.Errorf("the health endpoint returned %d without a web root", health.Code)
	}

	recorder := call(t, handler, http.MethodGet, "/rooms/abc", nil, "")
	if recorder.Code == http.StatusOK {
		t.Error("an application route was served without a web root")
	}
}

/*
TestFingerprintedAssetsAreCachedForever reflects how Nuxt names what it builds.

A file under _nuxt carries its content hash, so its bytes can never change and
a year is safe. The shell must not be cached at all: it names the hashed assets
for the version it was built with, and a cached shell after a deployment asks
for files that are no longer there.
*/
func TestFingerprintedAssetsAreCachedForever(t *testing.T) {
	handler := newTestAPIWithWebRoot(t, webRoot(t))

	asset := call(t, handler, http.MethodGet, "/_nuxt/entry.a1b2c3.js", nil, "")
	if cache := asset.Header().Get("Cache-Control"); !strings.Contains(cache, "immutable") {
		t.Errorf("a fingerprinted asset has Cache-Control %q, expected it to be immutable", cache)
	}

	shell := call(t, handler, http.MethodGet, "/rooms/abc", nil, "")
	if cache := shell.Header().Get("Cache-Control"); !strings.Contains(cache, "no-cache") {
		t.Errorf("the application shell has Cache-Control %q, expected no-cache", cache)
	}
}
