package identity_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/dbtest"
	"github.com/AnoRebel/tdc/server/internal/identity"
)

// newTestService returns a service over a real database, with every account
// removed afterwards so tests do not see each other's data.
func newTestService(t *testing.T) (*identity.Service, *pgxpool.Pool) {
	t.Helper()

	url := dbtest.URL(t, "identity")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := db.Migrate(ctx, url, discardLogger()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	pool, err := db.Open(ctx, db.Config{URL: url}, discardLogger())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	clean := func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanCancel()
		// Credentials, sessions and role grants cascade from users.
		if _, err := pool.Exec(cleanCtx, "DELETE FROM users"); err != nil {
			t.Errorf("clean users: %v", err)
		}
	}
	clean()
	t.Cleanup(func() {
		clean()
		pool.Close()
	})

	return identity.NewService(dbgen.New(pool), discardLogger()), pool.Pool
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestCreateUserStoresAccount(t *testing.T) {
	service, _ := newTestService(t)
	ctx := context.Background()

	user, err := service.CreateUser(ctx, identity.NewUser{
		Username: "Amina",
		Email:    "Amina@Example.com",
		Password: goodPassword,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if user.Username != "Amina" {
		t.Errorf("username = %q, want the form the user typed", user.Username)
	}
	// The folded forms are what uniqueness and lookup use.
	if user.UsernameFolded != "amina" {
		t.Errorf("usernameFolded = %q, want %q", user.UsernameFolded, "amina")
	}
	if user.EmailFolded == nil || *user.EmailFolded != "amina@example.com" {
		t.Errorf("emailFolded = %v, want lower-cased", user.EmailFolded)
	}
	if user.DisplayName != "Amina" {
		t.Errorf("displayName = %q, want it to default to the username", user.DisplayName)
	}
}

// Two accounts must not differ only by case, or one could impersonate the
// other.
func TestUsernameUniquenessIgnoresCase(t *testing.T) {
	service, _ := newTestService(t)
	ctx := context.Background()

	if _, err := service.CreateUser(ctx, identity.NewUser{Username: "juma", Password: goodPassword}); err != nil {
		t.Fatalf("first CreateUser: %v", err)
	}

	_, err := service.CreateUser(ctx, identity.NewUser{Username: "JUMA", Password: goodPassword})
	if !errors.Is(err, identity.ErrUsernameTaken) {
		t.Fatalf("error = %v, want ErrUsernameTaken", err)
	}
}

func TestEmailUniquenessIgnoresCase(t *testing.T) {
	service, _ := newTestService(t)
	ctx := context.Background()

	if _, err := service.CreateUser(ctx, identity.NewUser{
		Username: "neema", Email: "neema@example.com", Password: goodPassword,
	}); err != nil {
		t.Fatalf("first CreateUser: %v", err)
	}

	_, err := service.CreateUser(ctx, identity.NewUser{
		Username: "neema2", Email: "NEEMA@EXAMPLE.COM", Password: goodPassword,
	})
	if !errors.Is(err, identity.ErrEmailTaken) {
		t.Fatalf("error = %v, want ErrEmailTaken", err)
	}
}

func TestLookupIsCaseInsensitive(t *testing.T) {
	service, _ := newTestService(t)
	ctx := context.Background()

	created, err := service.CreateUser(ctx, identity.NewUser{Username: "Baraka", Password: goodPassword})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	found, err := service.UserByUsername(ctx, "bArAkA")
	if err != nil {
		t.Fatalf("UserByUsername: %v", err)
	}
	if found.ID != created.ID {
		t.Error("lookup by a differently-cased username found a different account")
	}
}

func TestUnknownUserIsReported(t *testing.T) {
	service, _ := newTestService(t)

	_, err := service.UserByUsername(context.Background(), "nobody")
	if !errors.Is(err, identity.ErrUserNotFound) {
		t.Errorf("error = %v, want ErrUserNotFound", err)
	}
}

// A weak password must not leave a half-created account behind.
func TestWeakPasswordCreatesNoAccount(t *testing.T) {
	service, _ := newTestService(t)
	ctx := context.Background()

	if _, err := service.CreateUser(ctx, identity.NewUser{
		Username: "salma", Password: "short",
	}); !errors.Is(err, identity.ErrWeakPassword) {
		t.Fatalf("error = %v, want ErrWeakPassword", err)
	}

	if _, err := service.UserByUsername(ctx, "salma"); !errors.Is(err, identity.ErrUserNotFound) {
		t.Error("an account was created despite the password being rejected")
	}
}

func TestReservedUsernamesAreRefused(t *testing.T) {
	service, _ := newTestService(t)
	ctx := context.Background()

	for _, name := range []string{"admin", "system", "everyone"} {
		if _, err := service.CreateUser(ctx, identity.NewUser{
			Username: name, Password: goodPassword,
		}); !errors.Is(err, identity.ErrInvalidUsername) {
			t.Errorf("CreateUser(%q) error = %v, want ErrInvalidUsername", name, err)
		}
	}
}

// The first-run path must close once an administrator exists, or it could be
// used later to mint one.
func TestSetupClosesAfterFirstAdministrator(t *testing.T) {
	service, _ := newTestService(t)
	ctx := context.Background()

	needed, err := service.SetupNeeded(ctx)
	if err != nil {
		t.Fatalf("SetupNeeded: %v", err)
	}
	if !needed {
		t.Fatal("setup reported as unnecessary on an empty server")
	}

	if _, err := service.CompleteSetup(ctx, identity.NewUser{
		Username: "founder", Password: goodPassword,
	}); err != nil {
		t.Fatalf("CompleteSetup: %v", err)
	}

	needed, err = service.SetupNeeded(ctx)
	if err != nil {
		t.Fatalf("SetupNeeded after setup: %v", err)
	}
	if needed {
		t.Error("setup still reported as needed after an administrator was created")
	}

	_, err = service.CompleteSetup(ctx, identity.NewUser{
		Username: "interloper", Password: goodPassword,
	})
	if !errors.Is(err, identity.ErrSetupComplete) {
		t.Fatalf("second CompleteSetup error = %v, want ErrSetupComplete", err)
	}
}

// The first administrator must be able to sign in.
func TestSetupRequiresAPassword(t *testing.T) {
	service, _ := newTestService(t)

	if _, err := service.CompleteSetup(context.Background(), identity.NewUser{
		Username: "founder",
	}); err == nil {
		t.Error("CompleteSetup accepted an administrator with no password")
	}
}
