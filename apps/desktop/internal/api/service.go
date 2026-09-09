/*
Package api carries requests from the window to a TDC server.

The web client has a browser doing this: it keeps the cookie, follows the
redirect, and knows what origin it was served from. Here there is no origin —
the interface is served out of the binary — so the address is something a
person chooses and this package remembers, and the session cookie is something
this package holds rather than a browser.

Deliberately one method. The shared interface is a path, a body, and either an
answer or a refusal; every transport-shaped thing above it is written once in
TypeScript and sees only that.
*/
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"strings"
	"time"
)

/*
Request is one call, as the interface above sends it.

The field names are what the TypeScript sends, so the two definitions have to
agree — which is what the generated bindings are for: change this and the
frontend stops compiling rather than starting to fail at runtime.
*/
type Request struct {
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Body   json.RawMessage `json:"body,omitempty"`
}

// Response is what came back, shaped as the shared ApiResponse.
type Response struct {
	Status int             `json:"status,omitempty"`
	Body   json.RawMessage `json:"body,omitempty"`
	OK     bool            `json:"ok"`
	// Seconds the server asked us to wait, when it said so.
	RetryAfterSeconds int `json:"retryAfterSeconds,omitempty"`
}

/*
Service sends requests to whichever server this client is signed in to.

One per application. It holds the cookie jar, which is the session: losing it
signs the person out, so it outlives any individual window.
*/
type Service struct {
	client *http.Client
	// Where requests go. Empty until somebody has chosen a server, which is
	// what 9.11 asks for; a request before then is refused rather than sent
	// somewhere arbitrary.
	base string
}

// ErrNoServer is what a request before choosing a server gets.
var ErrNoServer = errors.New("no server chosen")

/*
New builds the service.

The timeout is a whole-request ceiling including the body, which is why it is
generous: a slow connection uploading an attachment is not a hung request, and
cutting it off at the usual few seconds would make large files impossible on
exactly the connections that most need patience. Requests that should give up
sooner pass their own context.
*/
func New() *Service {
	// Errors only on an invalid public-suffix list, which is compiled in.
	jar, _ := cookiejar.New(nil)

	return &Service{
		client: &http.Client{
			Jar:     jar,
			Timeout: 2 * time.Minute,
		},
	}
}

/*
UseServer points subsequent requests at an address.

Validation of the address — refusing an unencrypted one that is not loopback —
belongs to the server-selection work in 9.11 and is done before this is
called, so that a refusal can explain itself in the interface rather than
failing here as an opaque error.
*/
func (s *Service) UseServer(base string) {
	s.base = strings.TrimSuffix(base, "/")
}

/*
Send makes one request and reports what happened.

A refusal by the server is a Response with OK false, not a Go error: a 403 is
an answer, and the interface above needs the body to say why. An error is
returned only when there is no answer at all — the machine is unreachable, the
name does not resolve, the connection died mid-body. That distinction is the
whole reason the shared error type has a code for "offline" separate from its
codes for things a server said.
*/
func (s *Service) Send(ctx context.Context, request Request) (Response, error) {
	if s.base == "" {
		return Response{}, ErrNoServer
	}

	var body io.Reader
	if len(request.Body) > 0 {
		body = bytes.NewReader(request.Body)
	}

	url := s.base + "/" + strings.TrimPrefix(request.Path, "/")

	outgoing, err := http.NewRequestWithContext(ctx, request.Method, url, body)
	if err != nil {
		return Response{}, fmt.Errorf("building the request: %w", err)
	}
	if body != nil {
		outgoing.Header.Set("Content-Type", "application/json")
	}
	outgoing.Header.Set("Accept", "application/json")

	incoming, err := s.client.Do(outgoing)
	if err != nil {
		// Unreachable, not refused. The interface says "offline" for this.
		return Response{}, fmt.Errorf("reaching %s: %w", s.base, err)
	}
	defer incoming.Body.Close()

	/*
		Bounded, because a server this client does not control could otherwise
		hand back a body large enough to exhaust memory. Ten megabytes is far
		above any JSON this API produces and far below a problem; attachments
		do not come through here.
	*/
	raw, err := io.ReadAll(io.LimitReader(incoming.Body, 10<<20))
	if err != nil {
		return Response{}, fmt.Errorf("reading the response: %w", err)
	}

	response := Response{
		Status: incoming.StatusCode,
		OK:     incoming.StatusCode >= 200 && incoming.StatusCode < 300,
	}

	/*
		Only when it is JSON. A proxy returning an HTML error page is a real
		thing that happens, and handing that to a JSON parser above would
		report a parse failure where the useful fact is the status.
	*/
	if json.Valid(raw) && len(raw) > 0 {
		response.Body = raw
	}

	// Seconds only. The HTTP date form is allowed by the specification and
	// this API does not send it; a value that is not a count of seconds is
	// left absent rather than guessed at.
	if after := incoming.Header.Get("Retry-After"); after != "" {
		if seconds, err := strconv.Atoi(after); err == nil && seconds >= 0 {
			response.RetryAfterSeconds = seconds
		}
	}

	return response, nil
}
