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
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AnoRebel/tdc/apps/desktop/internal/credentials"
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

One per application, holding every server somebody has added and the session
for each. A session is a cookie jar, and one jar shared between servers would
send one server's cookie to another — so each gets its own, and switching
between them is picking a different jar rather than signing in again.
*/
type Service struct {
	// Guards everything below. The window switches servers from one goroutine
	// and requests are made from others.
	mu sync.RWMutex

	/*
		Where sessions are kept between launches, or nil where nothing on this
		machine can hold one.

		Nil is a working application that asks for a password each launch, not
		a broken one — and the interface says so rather than leaving somebody
		to notice.
	*/
	store *credentials.Store

	/*
		A client per server, because the session lives in its cookie jar.

		Keyed by the checked origin, so two spellings of the same server —
		with and without a trailing slash, say — are one entry rather than two
		half-signed-in ones.
	*/
	clients map[string]*http.Client

	// Which server requests currently go to. Empty until somebody has chosen
	// one; a request before then is refused rather than sent somewhere
	// arbitrary.
	current string

	/*
		The servers somebody has added, in the order they added them.

		Kept separately from the clients so the interface can list them without
		holding the map, and so a server stays listed while its session is
		gone — being signed out of a server is not the same as not having it.
	*/
	order []string

	// Who the webview signed in as, so a message this person sent does not
	// raise a banner on their own machine. See whoami.go.
	who identity
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
	service := &Service{clients: map[string]*http.Client{}}

	/*
		A keychain if this machine has one. The error is deliberately not
		returned: an application that refused to start because a keyring
		daemon was not running would be worse than one that asks for a
		password each time and says why.
	*/
	if store, err := credentials.Open(); err == nil {
		service.store = store
	}

	return service
}

/*
CredentialStorage reports where credentials are kept, so the interface can say
so.

"none" is the answer that needs explaining, and CredentialAdvice is what to
say about it.
*/
func (s *Service) CredentialStorage() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return string(s.store.Kind())
}

// CredentialAdvice explains a machine with nowhere to keep a credential.
func (s *Service) CredentialAdvice() string {
	return credentials.Advice()
}

/*
UseEncryptedFile switches to a file encrypted with a passphrase.

Only reached when somebody has been told there is no keychain and has chosen
this rather than signing in each time. The passphrase is not kept: losing it
means the file cannot be read, which is what makes it worth having.
*/
func (s *Service) UseEncryptedFile(directory, passphrase string) error {
	store, err := credentials.OpenEncryptedFile(directory, func() (string, error) {
		return passphrase, nil
	})
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.store = store

	return nil
}

/*
Remember keeps what lets somebody stay signed in to one server.

Refused rather than silently skipped where there is nowhere safe: somebody who
believes they are being remembered and is not will conclude the application is
broken.
*/
func (s *Service) Remember(server, username, password, session string) error {
	checked, err := ParseAddress(server)
	if err != nil {
		return err
	}

	s.mu.RLock()
	store := s.store
	s.mu.RUnlock()

	return store.Save(checked.URL, credentials.Credential{
		Session:  session,
		Password: password,
		Username: username,
	})
}

/*
Recall returns the username held for a server, and nothing else.

Deliberately not the password or the session. Those are for this Go side to
use when signing in again; handing them to the webview would put them where a
script could reach them, which is the thing keeping them in a keychain was
for.
*/
func (s *Service) Recall(server string) (string, error) {
	checked, err := ParseAddress(server)
	if err != nil {
		return "", err
	}

	s.mu.RLock()
	store := s.store
	s.mu.RUnlock()

	credential, found, err := store.Load(checked.URL)
	if err != nil || !found {
		return "", err
	}

	return credential.Username, nil
}

/*
clientFor returns the client holding this server's session, making one if this
is the first time this server has been used.

Called with the lock held.
*/
func (s *Service) clientFor(origin string) *http.Client {
	if existing, ok := s.clients[origin]; ok {
		return existing
	}

	// Errors only on an invalid public-suffix list, which is compiled in.
	jar, _ := cookiejar.New(nil)

	client := &http.Client{Jar: jar, Timeout: 2 * time.Minute}
	s.clients[origin] = client
	return client
}

/*
UseServer points subsequent requests at an address, adding it if it is new.

The address is checked here rather than by the caller. This is the only way in,
and a check somewhere else would be one a program calling these bindings could
skip — the refusal has to be where the decision is.

Returns the checked address so the interface can show what was actually used:
somebody who typed a bare host gets https:// added, and seeing that is how they
learn it was not sent in the clear.
*/
func (s *Service) UseServer(address string) (Address, error) {
	checked, err := ParseAddress(address)
	if err != nil {
		return Address{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.clientFor(checked.URL)
	if !slices.Contains(s.order, checked.URL) {
		s.order = append(s.order, checked.URL)
	}
	s.current = checked.URL

	return checked, nil
}

// Servers lists what somebody has added, in the order they added them.
func (s *Service) Servers() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return slices.Clone(s.order)
}

// CurrentServer reports which one requests go to, or empty if none is chosen.
func (s *Service) CurrentServer() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.current
}

/*
ForgetServer removes a server and the session held for it.

The session goes with it: somebody removing a server from this client means to
be rid of it, and leaving a signed-in jar behind so that re-adding it silently
resumes the old session would be a surprise — and on a shared machine, the
wrong kind.
*/
func (s *Service) ForgetServer(address string) error {
	checked, err := ParseAddress(address)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.clients, checked.URL)
	s.order = slices.DeleteFunc(s.order, func(held string) bool { return held == checked.URL })

	// The stored credential goes with it. Leaving one behind so that re-adding
	// a server silently signed somebody back in would be a surprise, and on a
	// shared machine the wrong kind.
	_ = s.store.Forget(checked.URL)

	// Switching away from the one being removed rather than leaving requests
	// pointed at a server this no longer holds a session for.
	if s.current == checked.URL {
		s.current = ""
		if len(s.order) > 0 {
			s.current = s.order[0]
		}
	}

	return nil
}

/*
Send makes one request and reports what happened.

A refusal by the server is a Response with OK false, not a Go error: a 403 is
an answer, and the interface above needs the body to say why. An error is
returned only when there is no answer at all — the machine is unreachable, the
name does not resolve, the connection died mid-body.

That distinction is what the shared layer decides on: a refusal carries a
status and an unreachable server does not, and the difference is whether to
retry or to keep somebody's work rather than discard it.
*/
func (s *Service) Send(ctx context.Context, request Request) (Response, error) {
	s.mu.RLock()
	base, client := s.current, s.clients[s.current]
	s.mu.RUnlock()

	if base == "" || client == nil {
		return Response{}, ErrNoServer
	}

	var body io.Reader
	if len(request.Body) > 0 {
		body = bytes.NewReader(request.Body)
	}

	url := base + "/" + strings.TrimPrefix(request.Path, "/")

	outgoing, err := http.NewRequestWithContext(ctx, request.Method, url, body)
	if err != nil {
		return Response{}, fmt.Errorf("building the request: %w", err)
	}
	if body != nil {
		outgoing.Header.Set("Content-Type", "application/json")
	}
	outgoing.Header.Set("Accept", "application/json")

	incoming, err := client.Do(outgoing)
	if err != nil {
		// Unreachable, not refused. The interface says "offline" for this.
		return Response{}, fmt.Errorf("reaching %s: %w", base, err)
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
