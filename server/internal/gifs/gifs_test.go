package gifs_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AnoRebel/tdc/server/internal/gifs"
)

// quiet is a logger that keeps test output readable.
func quiet() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

/*
dialTo sends every connection to one address, whatever host was asked for.

Lets a test point the service at a local stand-in without the service knowing:
it still builds the provider's real URL, which is what makes the parameters and
paths under test the real ones.
*/
func dialTo(address string) gifs.DialFunc {
	return func(ctx context.Context, network, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, network, address)
	}
}

/*
standIn starts a stand-in provider over TLS.

TLS because the service builds the provider's real https URL, which is the
point: the parameters and paths under test are the ones a real search sends.
The dialer redirects the connection here, and the certificate is trusted only
by the client this returns.
*/
func standIn(t *testing.T, settings gifs.Settings, handler http.HandlerFunc) *gifs.Service {
	t.Helper()

	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)

	address := strings.TrimPrefix(server.URL, "https://")

	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())

	return gifs.NewService(
		// httptest issues its certificate for example.com.
		settings.WithTestTLS(&tls.Config{RootCAs: pool, ServerName: "example.com"}),
		dialTo(address), quiet())
}

/*
TestSearchIsOffByDefault is the constraint the whole package answers to.

A server must work with no account anywhere. Configuring nothing has to mean
searching is unavailable rather than failing in some other way, and Enabled has
to say so, so that a client hides the control rather than offering one that
cannot work.
*/
func TestSearchIsOffByDefault(t *testing.T) {
	service := gifs.NewService(gifs.Settings{}, nil, quiet())

	if service.Enabled() {
		t.Error("gif search is enabled with nothing configured")
	}

	_, err := service.Search(context.Background(), "cat", 10)
	if !errors.Is(err, gifs.ErrDisabled) {
		t.Errorf("searching with nothing configured gave %v, want ErrDisabled", err)
	}
}

// TestAKeyWithoutAProviderIsStillOff covers half a configuration, which is
// what a half-finished deployment looks like.
func TestAKeyWithoutAProviderIsStillOff(t *testing.T) {
	service := gifs.NewService(gifs.Settings{APIKey: "abc123"}, nil, quiet())

	if service.Enabled() {
		t.Error("a key with no provider enabled searching")
	}
}

// TestAProviderWithoutAKeyIsOff covers the other half.
func TestAProviderWithoutAKeyIsOff(t *testing.T) {
	service := gifs.NewService(gifs.Settings{Provider: gifs.ProviderTenor}, nil, quiet())

	if service.Enabled() {
		t.Error("a provider with no key enabled searching")
	}
}

// TestTenorResultsAreMapped covers the ordinary case against Tenor's shape.
func TestTenorResultsAreMapped(t *testing.T) {
	var asked *http.Request

	service := standIn(t,
		gifs.Settings{Provider: gifs.ProviderTenor, APIKey: "secret-key"},
		func(w http.ResponseWriter, r *http.Request) {
			asked = r
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"results":[
			{"id":"1","content_description":"a cat waving",
			 "media_formats":{
				"gif":{"url":"https://media.example/1.gif","dims":[480,270]},
				"tinygif":{"url":"https://media.example/1-tiny.gif","dims":[120,68]}}}]}`)
		})

	results, err := service.Search(context.Background(), "cat", 10)
	if err != nil {
		t.Fatalf("searching: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].URL != "https://media.example/1.gif" {
		t.Errorf("the full-size url is %q", results[0].URL)
	}
	// The preview is a separate, smaller file: a grid of full-size animations
	// would be megabytes.
	if results[0].PreviewURL != "https://media.example/1-tiny.gif" {
		t.Errorf("the preview url is %q", results[0].PreviewURL)
	}
	// Alt text is what makes a result usable by somebody who cannot see it.
	if results[0].Description != "a cat waving" {
		t.Errorf("the description is %q", results[0].Description)
	}
	if results[0].Width != 480 || results[0].Height != 270 {
		t.Errorf("dimensions are %dx%d, want 480x270", results[0].Width, results[0].Height)
	}

	if asked == nil {
		t.Fatal("the provider was not called")
	}
	if got := asked.URL.Query().Get("q"); got != "cat" {
		t.Errorf("the query sent was %q", got)
	}
	// The strictest filter by default: this is a search box in a shared
	// conversation, and an innocuous word must not return something somebody
	// then has to explain.
	if got := asked.URL.Query().Get("contentfilter"); got != "high" {
		t.Errorf("the content filter was %q, want high", got)
	}
}

// TestGiphyResultsAreMapped covers Giphy's rather different shape.
func TestGiphyResultsAreMapped(t *testing.T) {
	var asked *http.Request

	service := standIn(t,
		gifs.Settings{Provider: gifs.ProviderGiphy, APIKey: "secret-key"},
		func(w http.ResponseWriter, r *http.Request) {
			asked = r
			w.Header().Set("Content-Type", "application/json")
			// Giphy sends dimensions as strings, which is the detail worth
			// covering: a naive mapping loses them.
			io.WriteString(w, `{"data":[
			{"id":"xyz","title":"dancing","alt_text":"somebody dancing",
			 "images":{
				"original":{"url":"https://media.giphy/xyz.gif","width":"300","height":"200"},
				"fixed_height":{"url":"https://media.giphy/xyz-small.gif"}}}]}`)
		})

	results, err := service.Search(context.Background(), "dance", 10)
	if err != nil {
		t.Fatalf("searching: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Width != 300 || results[0].Height != 200 {
		t.Errorf("dimensions are %dx%d, want 300x200 from Giphy's strings",
			results[0].Width, results[0].Height)
	}
	if results[0].Description != "somebody dancing" {
		t.Errorf("the description is %q", results[0].Description)
	}
	if got := asked.URL.Query().Get("rating"); got != "g" {
		t.Errorf("the rating was %q, want g", got)
	}
}

/*
TestEmptyQueryAsksForSuggestions is what makes opening the picker useful.

With nothing typed, a search endpoint returns nothing. Both providers have a
separate endpoint for what is popular, so the picker shows something to choose
from rather than an empty grid.
*/
func TestEmptyQueryAsksForSuggestions(t *testing.T) {
	paths := make(chan string, 1)

	for name, settings := range map[string]gifs.Settings{
		"tenor": {Provider: gifs.ProviderTenor, APIKey: "k"},
		"giphy": {Provider: gifs.ProviderGiphy, APIKey: "k"},
	} {
		t.Run(name, func(t *testing.T) {
			service := standIn(t, settings, func(w http.ResponseWriter, r *http.Request) {
				paths <- r.URL.Path
				io.WriteString(w, `{"results":[],"data":[]}`)
			})

			if _, err := service.Search(context.Background(), "", 10); err != nil {
				t.Fatalf("searching: %v", err)
			}

			path := <-paths
			if strings.Contains(path, "search") {
				t.Errorf("an empty query used the search endpoint %q, "+
					"which returns nothing", path)
			}
		})
	}
}

/*
TestProviderFailuresDoNotLeakTheKey keeps the operator's credential out of what
a person sees.

A provider's own refusal often quotes the request, key included. Passing that
through would put the key in a browser's network tab for anybody who typed a
search while it was misconfigured.
*/
func TestProviderFailuresDoNotLeakTheKey(t *testing.T) {
	service := standIn(t,
		gifs.Settings{Provider: gifs.ProviderTenor, APIKey: "super-secret-key-12345"},
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, `{"error":"invalid key super-secret-key-12345"}`)
		})

	_, err := service.Search(context.Background(), "cat", 10)
	if err == nil {
		t.Fatal("a refused search did not report an error")
	}
	if strings.Contains(err.Error(), "super-secret-key-12345") {
		t.Errorf("the error carries the api key: %v", err)
	}
}

// TestResultsAreBounded keeps a picker a picker.
//
// Both providers cap at 50; this caps lower, because past a couple of dozen a
// grid is scrolling rather than choosing and every result is an animation the
// browser loads.
func TestResultsAreBounded(t *testing.T) {
	limits := make(chan string, 1)

	service := standIn(t,
		gifs.Settings{Provider: gifs.ProviderTenor, APIKey: "k"},
		func(w http.ResponseWriter, r *http.Request) {
			limits <- r.URL.Query().Get("limit")
			io.WriteString(w, `{"results":[]}`)
		})

	if _, err := service.Search(context.Background(), "cat", 5000); err != nil {
		t.Fatalf("searching: %v", err)
	}

	if got := <-limits; got != "24" {
		t.Errorf("an unbounded request asked the provider for %q", got)
	}
}
