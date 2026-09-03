package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AnoRebel/tdc/server/internal/buildinfo"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	// A nil pool is deliberate: these routes must work without a database, and
	// health reports only the checks it can actually perform.
	return New(Config{
		Address:         "127.0.0.1:0",
		ReadTimeout:     5 * time.Second,
		WriteTimeout:    5 * time.Second,
		IdleTimeout:     10 * time.Second,
		ShutdownTimeout: 2 * time.Second,
		ServerName:      "Test Server",
		DefaultLanguage: "en",
	}, nil, Services{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestHealthReportsOK(t *testing.T) {
	rec := httptest.NewRecorder()
	testServer(t).Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body healthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("status = %q, want %q", body.Status, "ok")
	}
}

// The version endpoint must report exactly what was built, because clients and
// federated peers use it to decide whether they can talk to this server.
func TestVersionMatchesBuild(t *testing.T) {
	rec := httptest.NewRecorder()
	testServer(t).Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/version", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body buildinfo.Info
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Version != buildinfo.Version {
		t.Errorf("version = %q, want %q", body.Version, buildinfo.Version)
	}
	if body.ProtocolVersion != buildinfo.ProtocolVersion {
		t.Errorf("protocolVersion = %q, want %q", body.ProtocolVersion, buildinfo.ProtocolVersion)
	}
}

// Health and version must not require credentials: a client needs the version
// before it can sign in, and an uptime check holds no credential.
func TestPublicEndpointsNeedNoCredentials(t *testing.T) {
	for _, path := range []string{"/api/health", "/api/version", "/api/server"} {
		rec := httptest.NewRecorder()
		testServer(t).Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s without credentials = %d, want %d", path, rec.Code, http.StatusOK)
		}
	}
}

func TestUnknownRouteIsNotFound(t *testing.T) {
	rec := httptest.NewRecorder()
	testServer(t).Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/nope", nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// Cancelling the context must stop the server and return, so that a supervisor
// sending SIGTERM gets a clean exit rather than a hung process.
func TestRunShutsDownOnContextCancel(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close probe listener: %v", err)
	}

	server := New(Config{
		Address:         address,
		ReadTimeout:     5 * time.Second,
		WriteTimeout:    5 * time.Second,
		IdleTimeout:     10 * time.Second,
		ShutdownTimeout: 2 * time.Second,
		ServerName:      "Test Server",
		DefaultLanguage: "en",
	}, nil, Services{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()

	waitForServing(t, address)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

func waitForServing(t *testing.T, address string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + address + "/api/health")
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("server did not start listening")
}

// An allowed origin must be echoed back so a browser permits the response,
// and a disallowed one must not be, so an arbitrary site cannot read
// authenticated responses on a user's behalf.
func TestCORSOnlyPermitsConfiguredOrigins(t *testing.T) {
	server := New(Config{
		Address:         "127.0.0.1:0",
		ReadTimeout:     5 * time.Second,
		WriteTimeout:    5 * time.Second,
		IdleTimeout:     10 * time.Second,
		ShutdownTimeout: 2 * time.Second,
		ServerName:      "Test Server",
		DefaultLanguage: "en",
		AllowedOrigins:  []string{"http://localhost:3000"},
	}, nil, Services{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	t.Run("allowed origin is permitted", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
		req.Header.Set("Origin", "http://localhost:3000")
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
			t.Errorf("Access-Control-Allow-Origin = %q, want the request origin", got)
		}
	})

	t.Run("unlisted origin is refused", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
		req.Header.Set("Origin", "https://attacker.example")
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("Access-Control-Allow-Origin = %q for an unlisted origin, want empty", got)
		}
	})

	t.Run("responses vary on origin", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
		req.Header.Set("Origin", "http://localhost:3000")
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)

		// Without this a cache could serve one origin's response to another.
		if !strings.Contains(rec.Header().Get("Vary"), "Origin") {
			t.Errorf("Vary = %q, want it to include Origin", rec.Header().Get("Vary"))
		}
	})
}
