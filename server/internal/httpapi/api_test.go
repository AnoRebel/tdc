package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AnoRebel/tdc/server/internal/attachments"
	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/cache"
	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/dbtest"
	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/httpapi"
	"github.com/AnoRebel/tdc/server/internal/identity"
	"github.com/AnoRebel/tdc/server/internal/invitations"
	"github.com/AnoRebel/tdc/server/internal/moderation"
	"github.com/AnoRebel/tdc/server/internal/notify"
	"github.com/AnoRebel/tdc/server/internal/presence"
	"github.com/AnoRebel/tdc/server/internal/rooms"
	"github.com/AnoRebel/tdc/server/internal/search"
	"github.com/AnoRebel/tdc/server/internal/settings"
	"github.com/AnoRebel/tdc/server/internal/storage"
)

const testPassword = "correct horse battery staple"

// newTestAPI builds a server over a real database, cleaned before and after.
// newTestAPIWithOrigins builds a handler that permits a cross-origin caller,
// for the tests that exercise browser behaviour across origins.
func newTestAPIWithOrigins(t *testing.T, origins []string) http.Handler {
	t.Helper()
	return newTestAPIConfigured(t, origins)
}

func newTestAPI(t *testing.T) http.Handler {
	t.Helper()
	return newTestAPIConfigured(t, nil)
}

// newTestAPIWithScanner builds a handler whose attachments are scanned, for the
// tests that exercise the fail-closed policy.
func newTestAPIWithScanner(t *testing.T, address string, failOpen bool) http.Handler {
	t.Helper()
	return newTestAPIConfigured(t, nil, func(s *attachments.Service) *attachments.Service {
		return s.WithScanner(attachments.NewClamAVScanner(address, 2*time.Second), failOpen)
	})
}

// webRootForTest is set by newTestAPIWithWebRoot for the handler it builds.
//
// A package-level value rather than another parameter, because threading one
// through would change four call sites for a case only these tests need.
var webRootForTest string

/*
newTestAPIWithWebRoot builds a handler that also serves a web application.

Used by the tests covering static serving, where the question is how file
requests and API requests share one origin.
*/
func newTestAPIWithWebRoot(t *testing.T, root string) http.Handler {
	t.Helper()

	previous := webRootForTest
	webRootForTest = root
	t.Cleanup(func() { webRootForTest = previous })

	return newTestAPIConfigured(t, nil)
}

func newTestAPIConfigured(
	t *testing.T,
	origins []string,
	adjust ...func(*attachments.Service) *attachments.Service,
) http.Handler {
	t.Helper()

	url := dbtest.URL(t, "httpapi")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := db.Migrate(ctx, url, logger); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	pool, err := db.Open(ctx, db.Config{URL: url}, logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	clean := func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanCancel()
		for _, statement := range []string{
			"DELETE FROM audit_entries", "DELETE FROM rooms", "DELETE FROM users", "DELETE FROM roles",
			// Settings are global rather than owned by a user or a room, so
			// unlike everything else here they survive into the next test and
			// silently change its policy.
			"DELETE FROM server_config", "DELETE FROM invitations",
			"DELETE FROM channel_bans", "DELETE FROM posting_restrictions",
			"DELETE FROM user_blocks", "DELETE FROM user_privacy",
		} {
			if _, err := pool.Exec(cleanCtx, statement); err != nil {
				t.Errorf("clean: %v", err)
			}
		}
	}
	clean()

	memory := cache.NewMemory()
	queries := dbgen.New(pool)

	signingKey, err := events.NewKeyPair("test-key")
	if err != nil {
		t.Fatalf("NewKeyPair: %v", err)
	}

	server := httpapi.New(httpapi.Config{
		Address:         "127.0.0.1:0",
		ReadTimeout:     5 * time.Second,
		WriteTimeout:    5 * time.Second,
		IdleTimeout:     10 * time.Second,
		ShutdownTimeout: 2 * time.Second,
		ServerName:      "Test Server",
		DefaultLanguage: "en",
		SessionLifetime: time.Hour,
		WebRoot:         webRootForTest,
		// Off so the test client's cookie jar behaves as a browser would over
		// plain HTTP.
		SecureCookies:  false,
		AllowedOrigins: origins,
	}, pool, httpapi.Services{
		Identity: identity.NewService(queries, logger),
		Authz:    authz.NewService(queries),
		Audit:    audit.NewRecorder(queries, logger),
		Limiter:  cache.NewLimiter(memory),
		Rooms:    rooms.NewService(queries, events.NewKeyring(signingKey), "tdc.test", logger),
		// The local store, in a directory the test framework removes: the
		// object-store backends have their own tests, and what matters here is
		// the behaviour above the interface.
		Attachments: configureAttachments(
			attachments.NewService(pool, testStore(t), attachments.DefaultLimits(), logger),
			adjust,
		),
		Search:      search.NewService(pool),
		Notify:      notify.NewService(pool, logger),
		Moderation:  moderation.NewService(pool, logger),
		Settings:    settings.NewService(pool, logger),
		Invitations: invitations.NewService(pool, logger),
		Presence:    presence.NewService(queries),
	}, logger)

	t.Cleanup(func() {
		clean()
		_ = memory.Close()
		pool.Close()
	})

	// Notifications and search indexing run detached from the request, so a
	// test asserting on either would otherwise race the work it is checking.
	// The handler waits for that work before returning.
	return settledHandler{handler: server.Handler(), server: server}
}

// configureAttachments applies whatever a test asked for, so the common case
// needs no argument at all.
func configureAttachments(
	service *attachments.Service,
	adjust []func(*attachments.Service) *attachments.Service,
) *attachments.Service {
	for _, apply := range adjust {
		service = apply(service)
	}
	return service
}

// testStore opens a throwaway attachment store for one test.
func testStore(t *testing.T) storage.Store {
	t.Helper()

	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("opening the attachment store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	return store
}

// call issues a request and returns the recorder.
func call(t *testing.T, handler http.Handler, method, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}

	request := httptest.NewRequest(method, path, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func decode(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
	}
	return body
}

// completeSetup performs first-run setup and returns the administrator's token.
func completeSetup(t *testing.T, handler http.Handler) string {
	t.Helper()

	recorder := call(t, handler, http.MethodPost, "/api/setup", map[string]string{
		"username": "founder",
		"password": testPassword,
	}, "")
	if recorder.Code != http.StatusCreated {
		t.Fatalf("setup: status %d, body %s", recorder.Code, recorder.Body.String())
	}

	return signIn(t, handler, "founder", testPassword)
}

func signIn(t *testing.T, handler http.Handler, username, password string) string {
	t.Helper()

	recorder := call(t, handler, http.MethodPost, "/api/auth/signin", map[string]string{
		"username": username, "password": password,
	}, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("sign in as %q: status %d, body %s", username, recorder.Code, recorder.Body.String())
	}

	token, ok := decode(t, recorder)["token"].(string)
	if !ok || token == "" {
		t.Fatal("sign-in returned no token")
	}
	return token
}

func TestSetupCreatesAdministratorThenCloses(t *testing.T) {
	handler := newTestAPI(t)

	status := decode(t, call(t, handler, http.MethodGet, "/api/setup", nil, ""))
	if status["setupRequired"] != true {
		t.Fatal("a fresh server did not report that setup is required")
	}

	completeSetup(t, handler)

	status = decode(t, call(t, handler, http.MethodGet, "/api/setup", nil, ""))
	if status["setupRequired"] != false {
		t.Error("setup is still reported as required after completion")
	}

	// The path must close, or it could be used later to mint an administrator.
	recorder := call(t, handler, http.MethodPost, "/api/setup", map[string]string{
		"username": "interloper", "password": testPassword,
	}, "")
	if recorder.Code != http.StatusConflict {
		t.Errorf("second setup: status %d, want %d", recorder.Code, http.StatusConflict)
	}
}

func TestRegisterAndSignIn(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)

	recorder := call(t, handler, http.MethodPost, "/api/auth/register", map[string]string{
		"username": "amina", "password": testPassword,
	}, "")
	if recorder.Code != http.StatusCreated {
		t.Fatalf("register: status %d, body %s", recorder.Code, recorder.Body.String())
	}

	created := decode(t, recorder)
	// Addresses carry the home server, so they stay unambiguous once servers
	// federate.
	if address, _ := created["address"].(string); !strings.Contains(address, "@") {
		t.Errorf("address = %q, want it to name the home server", address)
	}

	token := signIn(t, handler, "amina", testPassword)

	me := decode(t, call(t, handler, http.MethodGet, "/api/me", nil, token))
	if me["username"] != "amina" {
		t.Errorf("username = %v, want amina", me["username"])
	}
}

// The registration response must not carry anything that is not the
// requester's business.
func TestUserResponseWithholdsPrivateFields(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)

	recorder := call(t, handler, http.MethodPost, "/api/auth/register", map[string]string{
		"username": "private", "email": "private@example.com", "password": testPassword,
	}, "")
	if recorder.Code != http.StatusCreated {
		t.Fatalf("register: status %d", recorder.Code)
	}

	body := recorder.Body.String()
	for _, leaked := range []string{"private@example.com", "password", "suspend"} {
		if strings.Contains(strings.ToLower(body), leaked) {
			t.Errorf("the response leaks %q: %s", leaked, body)
		}
	}
}

func TestSignInFailsWithoutRevealingWhy(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)

	wrongPassword := call(t, handler, http.MethodPost, "/api/auth/signin", map[string]string{
		"username": "founder", "password": "not the password",
	}, "")
	unknownAccount := call(t, handler, http.MethodPost, "/api/auth/signin", map[string]string{
		"username": "nobody", "password": "not the password",
	}, "")

	if wrongPassword.Code != http.StatusUnauthorized {
		t.Errorf("wrong password: status %d, want 401", wrongPassword.Code)
	}
	if unknownAccount.Code != http.StatusUnauthorized {
		t.Errorf("unknown account: status %d, want 401", unknownAccount.Code)
	}
	// The two must be indistinguishable, or an unauthenticated caller can
	// enumerate accounts.
	if wrongPassword.Body.String() != unknownAccount.Body.String() {
		t.Errorf("responses differ:\n wrong password: %s\n unknown account: %s",
			wrongPassword.Body.String(), unknownAccount.Body.String())
	}
}

// Enforcement is server-side. A client that skips the interface must still be
// refused.
func TestProtectedRoutesRefuseUnauthenticatedRequests(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)

	for _, route := range []struct {
		method, path string
	}{
		{http.MethodGet, "/api/me"},
		{http.MethodGet, "/api/me/sessions"},
		{http.MethodPost, "/api/auth/signout"},
		{http.MethodGet, "/api/admin/audit"},
		{http.MethodGet, "/api/admin/permissions"},
	} {
		recorder := call(t, handler, route.method, route.path, nil, "")
		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a session: status %d, want 401",
				route.method, route.path, recorder.Code)
		}
	}
}

// An authenticated user without the permission must be refused, and told which
// permission was needed.
func TestAdminRoutesRefuseInsufficientPermission(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)

	if recorder := call(t, handler, http.MethodPost, "/api/auth/register", map[string]string{
		"username": "ordinary", "password": testPassword,
	}, ""); recorder.Code != http.StatusCreated {
		t.Fatalf("register: status %d", recorder.Code)
	}
	token := signIn(t, handler, "ordinary", testPassword)

	recorder := call(t, handler, http.MethodGet, "/api/admin/audit", nil, token)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403", recorder.Code)
	}
	// The refusal names the permission, so a person can ask for the right
	// thing rather than guessing.
	if !strings.Contains(recorder.Body.String(), "server.audit.read") {
		t.Errorf("the refusal does not name the permission: %s", recorder.Body.String())
	}
}

// The administrator created by setup must actually hold authority.
func TestAdministratorHoldsAuthority(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)

	if recorder := call(t, handler, http.MethodGet, "/api/admin/audit", nil, token); recorder.Code != http.StatusOK {
		t.Errorf("audit: status %d, want 200, body %s", recorder.Code, recorder.Body.String())
	}
	if recorder := call(t, handler, http.MethodGet, "/api/admin/permissions", nil, token); recorder.Code != http.StatusOK {
		t.Errorf("permissions: status %d, want 200", recorder.Code)
	}
}

func TestSessionsAreListedAndRevocable(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)

	// A second sign-in, so there is another session to end.
	other := signIn(t, handler, "founder", testPassword)

	var sessions []map[string]any
	recorder := call(t, handler, http.MethodGet, "/api/me/sessions", nil, token)
	if err := json.Unmarshal(recorder.Body.Bytes(), &sessions); err != nil {
		t.Fatalf("decode sessions: %v", err)
	}
	if len(sessions) < 2 {
		t.Fatalf("listed %d sessions, want at least 2", len(sessions))
	}

	// Exactly one must be marked current, so a person can tell which one they
	// are about to end.
	current := 0
	var otherID string
	for _, session := range sessions {
		if session["current"] == true {
			current++
		} else {
			otherID, _ = session["id"].(string)
		}
	}
	if current != 1 {
		t.Errorf("%d sessions marked current, want exactly 1", current)
	}

	if recorder := call(t, handler, http.MethodDelete, "/api/me/sessions/"+otherID, nil, token); recorder.Code != http.StatusNoContent {
		t.Fatalf("revoke: status %d", recorder.Code)
	}

	// The revoked session must stop working at once.
	if recorder := call(t, handler, http.MethodGet, "/api/me", nil, other); recorder.Code != http.StatusUnauthorized {
		t.Errorf("the revoked session still works: status %d", recorder.Code)
	}
	if recorder := call(t, handler, http.MethodGet, "/api/me", nil, token); recorder.Code != http.StatusOK {
		t.Error("revoking one session ended the other")
	}
}

// Repeated failures must be throttled.
func TestSignInIsRateLimited(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)

	limited := false
	for range 20 {
		recorder := call(t, handler, http.MethodPost, "/api/auth/signin", map[string]string{
			"username": "founder", "password": "wrong password",
		}, "")
		if recorder.Code == http.StatusTooManyRequests {
			limited = true
			// A refusal has to say when to try again.
			if recorder.Header().Get("Retry-After") == "" {
				t.Error("the refusal gives no Retry-After")
			}
			break
		}
	}
	if !limited {
		t.Error("repeated failed sign-ins were never throttled")
	}
}

// Varying the case of an identifier must not buy a fresh allowance.
func TestRateLimitIgnoresIdentifierCase(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)

	for range 10 {
		call(t, handler, http.MethodPost, "/api/auth/signin", map[string]string{
			"username": "founder", "password": "wrong password",
		}, "")
	}

	recorder := call(t, handler, http.MethodPost, "/api/auth/signin", map[string]string{
		"username": "FOUNDER", "password": "wrong password",
	}, "")
	if recorder.Code != http.StatusTooManyRequests {
		t.Errorf("a differently-cased identifier got a fresh allowance: status %d", recorder.Code)
	}
}

// Signing in and reading the log must both leave a record.
func TestActionsAreAudited(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)

	call(t, handler, http.MethodPost, "/api/auth/signin", map[string]string{
		"username": "founder", "password": "wrong password",
	}, "")

	var entries []map[string]any
	recorder := call(t, handler, http.MethodGet, "/api/admin/audit", nil, token)
	if err := json.Unmarshal(recorder.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decode audit: %v", err)
	}

	seen := map[string]bool{}
	for _, entry := range entries {
		if action, ok := entry["action"].(string); ok {
			seen[action] = true
		}
	}

	for _, wanted := range []string{
		string(audit.ActionSetupComplete),
		string(audit.ActionSignInSucceeded),
		string(audit.ActionSignInFailed),
	} {
		if !seen[wanted] {
			t.Errorf("%s was not recorded; saw %v", wanted, seen)
		}
	}
}

// The session cookie must be inaccessible to scripts, so an injected script
// cannot read it.
func TestSessionCookieIsHttpOnly(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)

	recorder := call(t, handler, http.MethodPost, "/api/auth/signin", map[string]string{
		"username": "founder", "password": testPassword,
	}, "")

	cookies := recorder.Result().Cookies()
	var session *http.Cookie
	for _, cookie := range cookies {
		if cookie.Name == "tdc_session" {
			session = cookie
		}
	}
	if session == nil {
		t.Fatal("no session cookie was set")
	}
	if !session.HttpOnly {
		t.Error("the session cookie is readable by scripts")
	}
	if session.SameSite != http.SameSiteLaxMode {
		t.Error("the session cookie has no SameSite protection")
	}
}

// mustCall issues a request and fails the test unless the status matches.
func mustCall(
	t *testing.T,
	handler http.Handler,
	method, path string,
	body any,
	token string,
	wantStatus int,
) *httptest.ResponseRecorder {
	t.Helper()

	recorder := call(t, handler, method, path, body, token)
	if recorder.Code != wantStatus {
		t.Fatalf("%s %s: status %d, want %d, body %s",
			method, path, recorder.Code, wantStatus, recorder.Body.String())
	}
	return recorder
}

/*
settledHandler waits for a request's background work before returning.

Notifications and search indexing are deliberately detached from the request,
so the response does not wait for them. That makes every test asserting on
either racy: the assertion runs before the work does.

Waiting here rather than sleeping in each test keeps the tests about behaviour
rather than about timing, and fails fast if that work ever stops finishing.
*/
type settledHandler struct {
	handler http.Handler
	server  *httpapi.Server
}

func (h settledHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.handler.ServeHTTP(w, r)
	h.server.WaitForBackground()
}
