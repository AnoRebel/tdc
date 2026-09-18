package recovery_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/dbtest"
	"github.com/AnoRebel/tdc/server/internal/identity"
	"github.com/AnoRebel/tdc/server/internal/mail"
	"github.com/AnoRebel/tdc/server/internal/recovery"
)

/*
Two rules, and most of this is them.

A token is good once, and asking discloses nothing. The second is the easier one
to lose: it is not a property of any single line but of every path answering
alike, and a change that made one branch return an error would break it without
breaking anything that looks like a test.
*/

// postbox is a mail sender that delivers to a slice.
type postbox struct {
	mu   sync.Mutex
	sent []mail.Message
	// configured is false for the tests about a server with no mail.
	configured bool
}

func (p *postbox) Send(_ context.Context, message mail.Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent = append(p.sent, message)
	return nil
}

func (p *postbox) Configured() bool { return p.configured }

func (p *postbox) delivered() []mail.Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]mail.Message(nil), p.sent...)
}

type harness struct {
	service *recovery.Service
	people  *identity.Service
	queries *dbgen.Queries
	pool    *db.Pool
	post    *postbox
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	url := dbtest.URL(t, "recovery")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := db.Migrate(ctx, url, quiet); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	pool, err := db.Open(ctx, db.Config{URL: url}, quiet)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	clean := func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := pool.Exec(cleanCtx, "DELETE FROM users"); err != nil {
			t.Errorf("clean: %v", err)
		}
	}
	clean()
	t.Cleanup(func() {
		clean()
		pool.Close()
	})

	queries := dbgen.New(pool)
	post := &postbox{configured: true}

	return &harness{
		service: recovery.NewService(queries, post, "chat.example.test", "Test Server", quiet),
		people:  identity.NewService(queries, quiet),
		queries: queries,
		pool:    pool,
		post:    post,
	}
}

// withAccount creates somebody whose address is verified, which is the only
// kind recovery will mail.
func (h *harness) withAccount(t *testing.T, username, email string) dbgen.User {
	t.Helper()

	user, err := h.people.CreateUser(context.Background(), identity.NewUser{
		Username:      username,
		Email:         email,
		Password:      "correct horse battery staple",
		EmailVerified: true,
	})
	if err != nil {
		t.Fatalf("creating %s: %v", username, err)
	}
	return user
}

// request asks for recovery and posts whatever came back, as the routes do.
func (h *harness) request(t *testing.T, address string) recovery.Pending {
	t.Helper()

	pending, err := h.service.Request(context.Background(), address, netip.MustParseAddr("192.0.2.10"))
	if err != nil {
		t.Fatalf("requesting recovery for %q: %v", address, err)
	}
	if !pending.Nothing() {
		if err := h.post.Send(context.Background(), pending.Message); err != nil {
			t.Fatalf("sending: %v", err)
		}
	}
	return pending
}

// tokenFrom pulls the link's token out of the message somebody received, which
// is the only place it exists.
func tokenFrom(t *testing.T, message mail.Message) string {
	t.Helper()

	_, after, found := strings.Cut(message.Body, "?token=")
	if !found {
		t.Fatalf("no link in the message:\n%s", message.Body)
	}
	token, _, _ := strings.Cut(after, "\n")
	return strings.TrimSpace(token)
}

/*
TestRecoveryDoesNotSayWhetherAnAccountExists.

The rule the specification states, and the one a refactor is most likely to
break: it is not a line of code but every path answering alike. An address with
an account and one without must be indistinguishable — no error, no different
return, and no mail to the second.
*/
func TestRecoveryDoesNotSayWhetherAnAccountExists(t *testing.T) {
	h := newHarness(t)
	h.withAccount(t, "amina", "amina@example.test")

	known, err := h.service.Request(context.Background(), "amina@example.test", netip.Addr{})
	if err != nil {
		t.Fatalf("a known address: %v", err)
	}

	unknown, err := h.service.Request(context.Background(), "nobody@example.test", netip.Addr{})
	if err != nil {
		t.Errorf("an unknown address produced an error, which a known one did not: %v", err)
	}

	// The caller must not be able to tell them apart by what it can see.
	if known.Nothing() {
		t.Error("a known address produced nothing to send")
	}
	if !unknown.Nothing() {
		t.Error("an unknown address produced something to send")
	}

	// Nor by what arrives: nothing is mailed to an address with no account.
	if len(h.post.delivered()) != 0 {
		t.Error("something was sent before the caller posted anything")
	}
}

// TestAnAddressThatIsNotAnAddressIsAlsoSilent, because a caller branching on
// "that is not an email" answers differently and so discloses.
func TestAnAddressThatIsNotAnAddressIsAlsoSilent(t *testing.T) {
	h := newHarness(t)

	for _, nonsense := range []string{"", "   ", "not-an-address", "@", "amina"} {
		pending, err := h.service.Request(context.Background(), nonsense, netip.Addr{})
		if err != nil {
			t.Errorf("%q produced an error: %v", nonsense, err)
		}
		if !pending.Nothing() {
			t.Errorf("%q produced something to send", nonsense)
		}
	}
}

/*
TestAnUnverifiedAddressIsNotMailed.

An unverified address is one somebody typed into a form. Mailing a way into the
account to it would make "put somebody else's address in your profile" an
account takeover — and silently, because saying so would disclose the account.
*/
func TestAnUnverifiedAddressIsNotMailed(t *testing.T) {
	h := newHarness(t)

	if _, err := h.people.CreateUser(context.Background(), identity.NewUser{
		Username: "juma",
		Email:    "juma@example.test",
		Password: "correct horse battery staple",
		// Deliberately not verified.
	}); err != nil {
		t.Fatalf("creating the account: %v", err)
	}

	pending, err := h.service.Request(context.Background(), "juma@example.test", netip.Addr{})
	if err != nil {
		t.Fatalf("requesting: %v", err)
	}
	if !pending.Nothing() {
		t.Error("an unverified address was sent a way into the account")
	}
}

// TestATokenIsGoodOnce, which is the other rule the specification states.
func TestATokenIsGoodOnce(t *testing.T) {
	h := newHarness(t)
	user := h.withAccount(t, "amina", "amina@example.test")

	h.request(t, "amina@example.test")
	token := tokenFrom(t, h.post.delivered()[0])

	recovered, err := h.service.Redeem(context.Background(), token, "a whole new passphrase here", h.people)
	if err != nil {
		t.Fatalf("redeeming: %v", err)
	}
	if recovered.ID != user.ID {
		t.Error("recovered the wrong account")
	}

	if _, err := h.service.Redeem(context.Background(), token, "another passphrase entirely", h.people); !errors.Is(err, recovery.ErrTokenInvalid) {
		t.Errorf("reusing a token gave %v, want a refusal", err)
	}
}

/*
TestANewRequestInvalidatesTheOld.

Asked for by the specification, and the reason is somebody who receives a
recovery mail they did not ask for: requesting one themselves is how they make
the first link stop working.
*/
func TestANewRequestInvalidatesTheOld(t *testing.T) {
	h := newHarness(t)
	h.withAccount(t, "amina", "amina@example.test")

	h.request(t, "amina@example.test")
	first := tokenFrom(t, h.post.delivered()[0])

	h.request(t, "amina@example.test")
	second := tokenFrom(t, h.post.delivered()[1])

	if first == second {
		t.Fatal("two requests produced the same token")
	}

	if _, err := h.service.Redeem(context.Background(), first, "a whole new passphrase here", h.people); !errors.Is(err, recovery.ErrTokenInvalid) {
		t.Errorf("the first token still worked: %v", err)
	}
	if _, err := h.service.Redeem(context.Background(), second, "a whole new passphrase here", h.people); err != nil {
		t.Errorf("the newest token did not work: %v", err)
	}
}

/*
TestRecoveringEndsEverySession.

Somebody recovering an account is ending a situation, and a session that
outlived the reset would leave whoever caused it still signed in.
*/
func TestRecoveringEndsEverySession(t *testing.T) {
	h := newHarness(t)
	user := h.withAccount(t, "amina", "amina@example.test")

	session, err := h.people.StartSession(context.Background(), user.ID,
		identity.DeviceContext{UserAgent: "somebody else"}, time.Hour)
	if err != nil {
		t.Fatalf("starting a session: %v", err)
	}

	h.request(t, "amina@example.test")
	token := tokenFrom(t, h.post.delivered()[0])

	if _, err := h.service.Redeem(context.Background(), token, "a whole new passphrase here", h.people); err != nil {
		t.Fatalf("redeeming: %v", err)
	}

	if _, _, err := h.people.AuthenticatedUser(context.Background(), session.Token); !errors.Is(err, identity.ErrSessionInvalid) {
		t.Errorf("a session survived the recovery: %v", err)
	}
}

/*
TestAWeakPasswordDoesNotBurnTheLink.

Somebody typing something the server will refuse should be told and allowed to
try again — not sent back to the start with a link that no longer works, which
is how a recovery becomes an email to an administrator.
*/
func TestAWeakPasswordDoesNotBurnTheLink(t *testing.T) {
	h := newHarness(t)
	h.withAccount(t, "amina", "amina@example.test")

	h.request(t, "amina@example.test")
	token := tokenFrom(t, h.post.delivered()[0])

	if _, err := h.service.Redeem(context.Background(), token, "short", h.people); err == nil {
		t.Fatal("a password the server refuses was accepted")
	}

	if _, err := h.service.Redeem(context.Background(), token, "a whole new passphrase here", h.people); err != nil {
		t.Errorf("the link was spent by the refused attempt: %v", err)
	}
}

/*
TestATokenSentToAnAddressTheAccountNoLongerHasIsRefused.

A token mailed to an old address must not be a way into what the new one now
protects — which matters most for the account whose owner changed their address
because the old one was compromised.
*/
func TestATokenSentToAnAddressTheAccountNoLongerHasIsRefused(t *testing.T) {
	h := newHarness(t)
	user := h.withAccount(t, "amina", "amina@example.test")

	h.request(t, "amina@example.test")
	token := tokenFrom(t, h.post.delivered()[0])

	/*
		The address moves before the link is used.

		Written straight to the row: there is no route for changing an address
		yet, and skipping the test until there is would leave the rule
		unguarded for exactly as long as it takes somebody to add one.
	*/
	if _, err := h.pool.Exec(context.Background(),
		"UPDATE users SET email = $2, email_folded = $2 WHERE id = $1",
		user.ID, "amina@newplace.test"); err != nil {
		t.Fatalf("moving the address: %v", err)
	}

	if _, err := h.service.Redeem(context.Background(), token, "a whole new passphrase here", h.people); !errors.Is(err, recovery.ErrTokenInvalid) {
		t.Errorf("a token sent to the old address still worked: %v", err)
	}
}

// TestAServerWithNoMailOffersNothing: recovery needs somewhere to send, and a
// client asks before drawing the link.
func TestAServerWithNoMailOffersNothing(t *testing.T) {
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := recovery.NewService(nil, &postbox{configured: false},
		"chat.example.test", "Test Server", quiet)

	if service.Available() {
		t.Error("a server with no mail says it can recover accounts")
	}
	if _, err := service.Request(context.Background(), "amina@example.test", netip.Addr{}); !errors.Is(err, recovery.ErrNotAvailable) {
		t.Errorf("requesting on a server with no mail gave %v", err)
	}
}
