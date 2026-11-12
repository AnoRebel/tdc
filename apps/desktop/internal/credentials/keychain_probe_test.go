package credentials

import (
	"os"
	"testing"
)

/*
The real keychain on the machine this runs on.

Skipped unless asked for, because it writes to the keychain of whoever runs it
and because CI has no desktop session to provide one — a test that failed there
would be reporting on the runner rather than on this code.

	TDC_TEST_KEYCHAIN=1 go test ./internal/credentials/ -run TestTheRealKeychain -v

What it demonstrates is the part the file-backed tests cannot: that the backend
list actually finds this machine's keychain, and that a round trip through it
works. Everything else about this package is exercised without touching it.
*/
func TestTheRealKeychain(t *testing.T) {
	if os.Getenv("TDC_TEST_KEYCHAIN") == "" {
		t.Skip("set TDC_TEST_KEYCHAIN=1 to use this machine's own keychain")
	}

	store, err := Open()
	if err != nil {
		t.Fatalf("this machine's keychain could not be opened: %v\n\n%s", err, Advice())
	}
	t.Logf("opened a store of kind %q", store.Kind())

	const server = "https://keychain-probe.invalid"
	t.Cleanup(func() { _ = store.Forget(server) })

	want := Credential{Session: "session", Password: "password", Username: "probe"}
	if err := store.Save(server, want); err != nil {
		t.Fatalf("saving to the keychain: %v", err)
	}

	got, found, err := store.Load(server)
	if err != nil || !found || got != want {
		t.Fatalf("round trip: found=%v err=%v got=%+v", found, err, got)
	}

	if err := store.Forget(server); err != nil {
		t.Fatalf("removing from the keychain: %v", err)
	}
	if _, stillThere, _ := store.Load(server); stillThere {
		t.Error("a credential removed from the keychain is still there")
	}
}
