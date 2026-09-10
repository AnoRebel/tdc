package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

/*
Holding more than one server at a time.

Somebody may belong to their community's server and their employer's, and
switching between them should be picking a different one rather than signing
out of the first. That means a session per server — and, more importantly, one
server's session never reaching another.
*/

func TestNoServerChosenIsRefusedRatherThanGuessed(t *testing.T) {
	service := New()

	_, err := service.Send(context.Background(), Request{Method: "GET", Path: "/api/server"})

	if !errors.Is(err, ErrNoServer) {
		t.Errorf("a request with no server chosen gave %v", err)
	}
}

func TestAServerIsRememberedOnceAdded(t *testing.T) {
	service := New()

	if _, err := service.UseServer("https://tdc.or.tz"); err != nil {
		t.Fatalf("adding a server: %v", err)
	}

	if got := service.CurrentServer(); got != "https://tdc.or.tz" {
		t.Errorf("the current server is %q", got)
	}
	if got := service.Servers(); !slices.Equal(got, []string{"https://tdc.or.tz"}) {
		t.Errorf("the servers are %v", got)
	}
}

/*
The refusal is here, not in whatever calls this.

These are bindings a program can call directly, so a check anywhere else is one
that can be skipped.
*/
func TestAnInsecureAddressIsRefusedAtTheServiceItself(t *testing.T) {
	service := New()

	if _, err := service.UseServer("http://tdc.or.tz"); !errors.Is(err, ErrInsecure) {
		t.Errorf("an unencrypted remote address gave %v", err)
	}

	if got := service.CurrentServer(); got != "" {
		t.Errorf("a refused address became the current server: %q", got)
	}
}

/* Two spellings of one server are one server, not two half-signed-in ones. */
func TestTheSameServerAddedTwiceIsOneServer(t *testing.T) {
	service := New()

	_, _ = service.UseServer("https://tdc.or.tz")
	_, _ = service.UseServer("https://tdc.or.tz/")

	if got := service.Servers(); len(got) != 1 {
		t.Errorf("the same server was held twice: %v", got)
	}
}

/*
The property this exists for: one server's session never reaches another.

A single cookie jar shared between servers would send the community server's
session cookie to an employer's, which is a credential leaking across a trust
boundary — and the kind of thing that looks like it works until somebody
notices their session on a server they never signed in to.
*/
func TestOneServersSessionNeverReachesAnother(t *testing.T) {
	// A server that sets a session cookie and reports back what it received.
	var receivedByFirst, receivedBySecond []string

	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, cookie := range r.Cookies() {
			receivedByFirst = append(receivedByFirst, cookie.Name+"="+cookie.Value)
		}
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "first-secret", Path: "/"})
		w.Write([]byte(`{}`))
	}))
	defer first.Close()

	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, cookie := range r.Cookies() {
			receivedBySecond = append(receivedBySecond, cookie.Name+"="+cookie.Value)
		}
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "second-secret", Path: "/"})
		w.Write([]byte(`{}`))
	}))
	defer second.Close()

	service := New()

	// Sign in to the first, which leaves a cookie behind.
	mustUse(t, service, first.URL)
	mustSend(t, service, "/api/auth/signin")

	// Switch to the second and make a request.
	mustUse(t, service, second.URL)
	mustSend(t, service, "/api/server")

	for _, cookie := range receivedBySecond {
		if cookie == "session=first-secret" {
			t.Fatal("the first server's session was sent to the second")
		}
	}

	// And back again: the first server's own session is still there, which is
	// what makes switching different from signing out.
	mustUse(t, service, first.URL)
	mustSend(t, service, "/api/server")

	if !slices.Contains(receivedByFirst, "session=first-secret") {
		t.Error("switching away and back lost the first server's session")
	}
}

/*
Removing a server takes its session with it.

Somebody removing a server means to be rid of it. Leaving a signed-in jar
behind so that re-adding it silently resumes the old session would be a
surprise — and on a shared machine, the wrong kind.
*/
func TestForgettingAServerDiscardsItsSession(t *testing.T) {
	var received []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, cookie := range r.Cookies() {
			received = append(received, cookie.Name+"="+cookie.Value)
		}
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "secret", Path: "/"})
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	service := New()
	mustUse(t, service, server.URL)
	mustSend(t, service, "/api/auth/signin")

	if err := service.ForgetServer(server.URL); err != nil {
		t.Fatalf("forgetting a server: %v", err)
	}
	if got := service.Servers(); len(got) != 0 {
		t.Errorf("a forgotten server is still listed: %v", got)
	}

	// Re-added, it must be a fresh session rather than the old one resumed.
	received = nil
	mustUse(t, service, server.URL)
	mustSend(t, service, "/api/server")

	if slices.Contains(received, "session=secret") {
		t.Error("re-adding a forgotten server resumed its old session")
	}
}

/*
Forgetting the current one moves to another rather than leaving requests

	pointed at a server no session is held for.
*/
func TestForgettingTheCurrentServerSwitchesToAnother(t *testing.T) {
	service := New()
	mustUse(t, service, "https://one.example")
	mustUse(t, service, "https://two.example")

	if err := service.ForgetServer("https://two.example"); err != nil {
		t.Fatalf("forgetting: %v", err)
	}

	if got := service.CurrentServer(); got != "https://one.example" {
		t.Errorf("after forgetting the current server, it is %q", got)
	}
}

func TestForgettingTheOnlyServerLeavesNone(t *testing.T) {
	service := New()
	mustUse(t, service, "https://one.example")

	_ = service.ForgetServer("https://one.example")

	if got := service.CurrentServer(); got != "" {
		t.Errorf("after forgetting the only server, it is %q", got)
	}
}

/*
A refusal by the server is an answer, not an error.

The whole reason Send returns both: a 403 is something the server said and the
interface needs its body to say why, where an error means nothing answered at
all and the shared layer decides differently about the two.
*/
func TestARefusalComesBackAsAnAnswer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"message": "slow down"})
	}))
	defer server.Close()

	service := New()
	mustUse(t, service, server.URL)

	response, err := service.Send(context.Background(), Request{Method: "GET", Path: "/api/rooms"})
	if err != nil {
		t.Fatalf("a refusal came back as an error: %v", err)
	}

	if response.OK {
		t.Error("a 429 was reported as success")
	}
	if response.Status != http.StatusTooManyRequests {
		t.Errorf("the status is %d", response.Status)
	}
	if response.RetryAfterSeconds != 30 {
		t.Errorf("the retry delay is %d", response.RetryAfterSeconds)
	}
}

/* And a server that is not there is an error, which is the other half. */
func TestAnUnreachableServerIsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	address := server.URL
	server.Close() // Nothing is listening now.

	service := New()
	mustUse(t, service, address)

	if _, err := service.Send(context.Background(), Request{Method: "GET", Path: "/api/rooms"}); err == nil {
		t.Error("an unreachable server came back as an answer")
	}
}

func mustUse(t *testing.T, service *Service, address string) {
	t.Helper()
	if _, err := service.UseServer(address); err != nil {
		t.Fatalf("using %s: %v", address, err)
	}
}

func mustSend(t *testing.T, service *Service, path string) {
	t.Helper()
	if _, err := service.Send(context.Background(), Request{Method: "GET", Path: path}); err != nil {
		t.Fatalf("sending to %s: %v", path, err)
	}
}
