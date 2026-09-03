package identity_test

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/identity"
)

func testDevice() identity.DeviceContext {
	return identity.DeviceContext{
		UserAgent: "TestClient/1.0",
		IPAddress: netip.MustParseAddr("203.0.113.7"),
	}
}

func mustCreateUser(t *testing.T, service *identity.Service, username string) uuid.UUID {
	t.Helper()
	user, err := service.CreateUser(context.Background(), identity.NewUser{
		Username: username,
		Password: goodPassword,
	})
	if err != nil {
		t.Fatalf("CreateUser(%q): %v", username, err)
	}
	return user.ID
}

func TestAuthenticateIssuesUsableSession(t *testing.T) {
	service, _ := newTestService(t)
	ctx := context.Background()
	userID := mustCreateUser(t, service, "asha")

	session, err := service.Authenticate(ctx,
		identity.Credentials{Username: "asha", Password: goodPassword},
		testDevice(), time.Hour)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if session.Token == "" {
		t.Fatal("no session token was returned")
	}

	authenticated, _, err := service.AuthenticatedUser(ctx, session.Token)
	if err != nil {
		t.Fatalf("AuthenticatedUser: %v", err)
	}
	if authenticated.ID != userID {
		t.Error("the session resolved to a different user")
	}
}

// The token must never be recoverable from the database: only its hash is
// stored, so a disclosure does not hand over live sessions.
func TestSessionTokenIsNotStored(t *testing.T) {
	service, pool := newTestService(t)
	ctx := context.Background()
	mustCreateUser(t, service, "hamisi")

	session, err := service.Authenticate(ctx,
		identity.Credentials{Username: "hamisi", Password: goodPassword},
		testDevice(), time.Hour)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	var matches int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM sessions WHERE encode(token_hash, 'escape') LIKE '%' || $1 || '%'",
		session.Token,
	).Scan(&matches); err != nil {
		t.Fatalf("query sessions: %v", err)
	}
	if matches != 0 {
		t.Error("the raw session token appears in the database")
	}
}

// Signing in must work with either the username or the email address, since
// people do not reliably remember which they registered with.
func TestAuthenticateAcceptsEmail(t *testing.T) {
	service, _ := newTestService(t)
	ctx := context.Background()

	if _, err := service.CreateUser(ctx, identity.NewUser{
		Username: "zawadi", Email: "zawadi@example.com", Password: goodPassword,
	}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if _, err := service.Authenticate(ctx,
		identity.Credentials{Username: "ZAWADI@Example.com", Password: goodPassword},
		testDevice(), time.Hour); err != nil {
		t.Errorf("Authenticate by email: %v", err)
	}
}

// Every failure must be the same error, so a caller cannot tell an unknown
// account from a wrong password.
func TestAuthenticationFailuresAreIndistinguishable(t *testing.T) {
	service, _ := newTestService(t)
	ctx := context.Background()
	mustCreateUser(t, service, "existing")

	cases := map[string]identity.Credentials{
		"wrong password":  {Username: "existing", Password: "definitely not the password"},
		"unknown account": {Username: "nobody-at-all", Password: goodPassword},
		"empty password":  {Username: "existing", Password: ""},
	}

	for name, creds := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := service.Authenticate(ctx, creds, testDevice(), time.Hour)
			if !errors.Is(err, identity.ErrAuthenticationFailed) {
				t.Errorf("error = %v, want ErrAuthenticationFailed", err)
			}
		})
	}
}

// Timing must not reveal whether an account exists. An unknown account still
// performs a password verification, so the two paths cost roughly the same.
func TestUnknownAccountCostsSimilarTimeToWrongPassword(t *testing.T) {
	service, _ := newTestService(t)
	ctx := context.Background()
	mustCreateUser(t, service, "timing")

	measure := func(creds identity.Credentials) time.Duration {
		// Take the median of several attempts: a single measurement on a busy
		// machine is noise.
		const attempts = 5
		samples := make([]time.Duration, 0, attempts)
		for range attempts {
			start := time.Now()
			_, _ = service.Authenticate(ctx, creds, testDevice(), time.Hour)
			samples = append(samples, time.Since(start))
		}
		slices := samples
		for i := range slices {
			for j := i + 1; j < len(slices); j++ {
				if slices[j] < slices[i] {
					slices[i], slices[j] = slices[j], slices[i]
				}
			}
		}
		return slices[len(slices)/2]
	}

	wrongPassword := measure(identity.Credentials{Username: "timing", Password: "wrong password here"})
	unknownAccount := measure(identity.Credentials{Username: "no-such-person", Password: "wrong password here"})

	ratio := float64(unknownAccount) / float64(wrongPassword)
	// A generous bound: the point is that the unknown-account path does real
	// hashing work rather than returning immediately, not that the two are
	// identical to the microsecond.
	if ratio < 0.2 || ratio > 5 {
		t.Errorf("unknown account took %v and wrong password took %v (ratio %.2f), which is distinguishable",
			unknownAccount, wrongPassword, ratio)
	}
}

func TestRevokedSessionStopsWorking(t *testing.T) {
	service, _ := newTestService(t)
	ctx := context.Background()
	userID := mustCreateUser(t, service, "revoker")

	session, err := service.Authenticate(ctx,
		identity.Credentials{Username: "revoker", Password: goodPassword},
		testDevice(), time.Hour)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	if err := service.RevokeSession(ctx, userID, session.Record.ID); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}

	if _, _, err := service.AuthenticatedUser(ctx, session.Token); !errors.Is(err, identity.ErrSessionInvalid) {
		t.Errorf("error = %v, want ErrSessionInvalid after revocation", err)
	}
}

// One person must not be able to end another's session by guessing its id.
func TestRevokeSessionIsScopedToOwner(t *testing.T) {
	service, _ := newTestService(t)
	ctx := context.Background()
	mustCreateUser(t, service, "owner")
	strangerID := mustCreateUser(t, service, "stranger")

	session, err := service.Authenticate(ctx,
		identity.Credentials{Username: "owner", Password: goodPassword},
		testDevice(), time.Hour)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	if err := service.RevokeSession(ctx, strangerID, session.Record.ID); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}

	if _, _, err := service.AuthenticatedUser(ctx, session.Token); err != nil {
		t.Error("another user's revocation ended this session")
	}
}

func TestExpiredSessionIsRejected(t *testing.T) {
	service, _ := newTestService(t)
	ctx := context.Background()
	userID := mustCreateUser(t, service, "expiring")

	session, err := service.StartSession(ctx, userID, testDevice(), time.Millisecond)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	time.Sleep(20 * time.Millisecond)

	if _, _, err := service.AuthenticatedUser(ctx, session.Token); !errors.Is(err, identity.ErrSessionInvalid) {
		t.Errorf("error = %v, want ErrSessionInvalid for an expired session", err)
	}
}

// Changing a password must evict whoever else is signed in, which is what
// someone expects after a suspected compromise.
func TestPasswordChangeEndsOtherSessions(t *testing.T) {
	service, _ := newTestService(t)
	ctx := context.Background()
	userID := mustCreateUser(t, service, "rotator")

	first, err := service.Authenticate(ctx,
		identity.Credentials{Username: "rotator", Password: goodPassword},
		testDevice(), time.Hour)
	if err != nil {
		t.Fatalf("first Authenticate: %v", err)
	}
	second, err := service.Authenticate(ctx,
		identity.Credentials{Username: "rotator", Password: goodPassword},
		testDevice(), time.Hour)
	if err != nil {
		t.Fatalf("second Authenticate: %v", err)
	}

	const newPassword = "an entirely different passphrase"
	if err := service.ChangePassword(ctx, userID, newPassword, second.Record.ID); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}

	if _, _, err := service.AuthenticatedUser(ctx, first.Token); !errors.Is(err, identity.ErrSessionInvalid) {
		t.Error("the other session survived a password change")
	}
	if _, _, err := service.AuthenticatedUser(ctx, second.Token); err != nil {
		t.Errorf("the session that requested the change was ended: %v", err)
	}

	if _, err := service.Authenticate(ctx,
		identity.Credentials{Username: "rotator", Password: newPassword},
		testDevice(), time.Hour); err != nil {
		t.Errorf("the new password does not work: %v", err)
	}
	if _, err := service.Authenticate(ctx,
		identity.Credentials{Username: "rotator", Password: goodPassword},
		testDevice(), time.Hour); !errors.Is(err, identity.ErrAuthenticationFailed) {
		t.Error("the old password still works after a change")
	}
}

func TestListSessionsShowsActiveOnes(t *testing.T) {
	service, _ := newTestService(t)
	ctx := context.Background()
	userID := mustCreateUser(t, service, "lister")

	for range 3 {
		if _, err := service.Authenticate(ctx,
			identity.Credentials{Username: "lister", Password: goodPassword},
			testDevice(), time.Hour); err != nil {
			t.Fatalf("Authenticate: %v", err)
		}
	}

	sessions, err := service.ListSessions(ctx, userID)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 3 {
		t.Fatalf("listed %d sessions, want 3", len(sessions))
	}
	// The listing has to carry enough context for a person to recognise a
	// session and decide whether to end it.
	if sessions[0].UserAgent != "TestClient/1.0" {
		t.Errorf("userAgent = %q, want the recorded client", sessions[0].UserAgent)
	}
}
