package credentials

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/99designs/keyring"
)

/*
Where a password may and may not be kept.

Both a session and a password are stored, so an expiring session is invisible
rather than a sign-in form appearing every few weeks. That makes where they
live the whole question: a session can be revoked and expires on its own, but a
password is the account until it is changed.
*/

/*
The property this package exists for.

keyring's own default backend list ends with a file backend, chosen
automatically when no keychain is present — which would write a password to
disk on exactly the machines least able to protect it, and do it silently. The
allowed list here must contain no such backend.
*/
func TestTheKeychainNeverFallsBackToAFile(t *testing.T) {
	// Read from the source rather than by opening a store, because on a
	// machine that has a keychain the fallback would never be reached and a
	// test that ran the code would pass while the list was wrong.
	source, err := os.ReadFile("credentials.go")
	if err != nil {
		t.Fatalf("reading the source: %v", err)
	}

	allowed := between(string(source), "AllowedBackends: []keyring.BackendType{", "},")
	if allowed == "" {
		t.Fatal("no allowed-backend list was found; keyring would use its own defaults")
	}

	for _, forbidden := range []string{"FileBackend", "PassBackend"} {
		if strings.Contains(allowed, forbidden) {
			t.Errorf("Open allows %s, which writes to disk when no keychain is present", forbidden)
		}
	}
}

/*
And the absence of a keychain is reported rather than worked around.

Somebody who thinks they are being remembered and is not will conclude the
application is broken. The caller can only say so if this says so first.
*/
func TestNoKeychainIsAnError(t *testing.T) {
	if !errors.Is(ErrNoStore, ErrNoStore) {
		t.Fatal("ErrNoStore is not comparable")
	}

	// A nil store is what a caller holds when Open failed. Every operation on
	// it has to refuse rather than pretend to have worked.
	var absent *Store

	if absent.Kind() != KindNone {
		t.Errorf("a store that could not be opened reports %q", absent.Kind())
	}
	if err := absent.Save("https://tdc.or.tz", Credential{Password: "hunter2"}); !errors.Is(err, ErrNoStore) {
		t.Errorf("saving without a store gave %v", err)
	}
	if _, _, err := absent.Load("https://tdc.or.tz"); !errors.Is(err, ErrNoStore) {
		t.Errorf("loading without a store gave %v", err)
	}
}

/*
The advice names what to do, not only what is wrong.

Somebody meeting this is being told their machine cannot remember them, which
is alarming without a next step.
*/
func TestTheAdviceSaysWhatToDo(t *testing.T) {
	advice := Advice()

	for _, expected := range []string{
		"gnome-keyring", "KDE Wallet",
		// Both ways forward, so nobody is left at a dead end.
		"sign in", "passphrase",
		/*
			Not running is the common case rather than not installed — this
			machine has kwalletd6 present and no session running it, and
			advice to install what is already there sends somebody nowhere.
		*/
		"not running",
	} {
		if !strings.Contains(advice, expected) {
			t.Errorf("the advice does not mention %q: %s", expected, advice)
		}
	}
}

/*
A round trip, against a real backend.

The file backend rather than a keychain, because a test must not write to the
keychain of whoever runs it — and because this is the one backend whose
behaviour this package configures rather than inherits.
*/
func TestACredentialSurvivesBeingSaved(t *testing.T) {
	store := fileStore(t)

	saved := Credential{Session: "session-value", Password: "hunter2", Username: "amina"}
	if err := store.Save("https://tdc.or.tz", saved); err != nil {
		t.Fatalf("saving: %v", err)
	}

	loaded, found, err := store.Load("https://tdc.or.tz")
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if !found {
		t.Fatal("what was just saved was not found")
	}
	if loaded != saved {
		t.Errorf("what came back is %+v", loaded)
	}
}

/*
Not signed in is not an error.

A first launch has no credential for any server, and reporting that as a
failure would make the ordinary case look like a broken one.
*/
func TestAMissingCredentialIsNotAnError(t *testing.T) {
	store := fileStore(t)

	credential, found, err := store.Load("https://never-used.example")
	if err != nil {
		t.Errorf("a server never signed in to gave an error: %v", err)
	}
	if found {
		t.Errorf("a credential appeared from nowhere: %+v", credential)
	}
}

/* One server's credential is not another's. */
func TestCredentialsAreKeptPerServer(t *testing.T) {
	store := fileStore(t)

	_ = store.Save("https://one.example", Credential{Password: "first"})
	_ = store.Save("https://two.example", Credential{Password: "second"})

	loaded, _, _ := store.Load("https://one.example")
	if loaded.Password != "first" {
		t.Errorf("one server's credential is %q", loaded.Password)
	}
}

func TestForgettingRemovesACredential(t *testing.T) {
	store := fileStore(t)
	_ = store.Save("https://tdc.or.tz", Credential{Password: "hunter2"})

	if err := store.Forget("https://tdc.or.tz"); err != nil {
		t.Fatalf("forgetting: %v", err)
	}

	if _, found, _ := store.Load("https://tdc.or.tz"); found {
		t.Error("a forgotten credential is still there")
	}
}

/*
Forgetting what is not there is success: the intent is that nothing be

	held, and nothing is.
*/
func TestForgettingWhatIsNotThereSucceeds(t *testing.T) {
	store := fileStore(t)

	if err := store.Forget("https://never-used.example"); err != nil {
		t.Errorf("forgetting an absent credential gave %v", err)
	}
}

/*
The encrypted file is encrypted.

The reason it is offered at all is that it is not plaintext. A file with a
password readable in it would be worse than not remembering anything, because
somebody would have been told it was safe.
*/
func TestTheEncryptedFileHoldsNoReadablePassword(t *testing.T) {
	directory := t.TempDir()
	store := fileStoreIn(t, directory)

	if err := store.Save("https://tdc.or.tz", Credential{
		Session: "session-value", Password: "hunter2-unmistakable", Username: "amina",
	}); err != nil {
		t.Fatalf("saving: %v", err)
	}

	var found bool
	err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		found = true

		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(contents), "hunter2-unmistakable") {
			t.Errorf("%s holds the password in the clear", path)
		}
		if strings.Contains(string(contents), "session-value") {
			t.Errorf("%s holds the session in the clear", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the store: %v", err)
	}
	if !found {
		t.Fatal("nothing was written, so this proved nothing")
	}
}

/*
An unreadable entry is reported rather than treated as absent.

An entry written by an older version, or corrupted. Saying "not signed in"
would leave somebody wondering why they are asked every time with nothing to
go on.
*/
func TestAnUnreadableCredentialIsReported(t *testing.T) {
	directory := t.TempDir()
	store := fileStoreIn(t, directory)

	// Written through the same backend, so it is encrypted correctly and
	// merely holds something this package cannot read.
	if err := store.ring.Set(keyring.Item{Key: "https://tdc.or.tz", Data: []byte("not json")}); err != nil {
		t.Fatalf("writing a broken entry: %v", err)
	}

	if _, _, err := store.Load("https://tdc.or.tz"); err == nil {
		t.Error("an unreadable credential was reported as absent")
	}
}

// A store backed by an encrypted file in a directory that goes away with the
// test, so nothing touches the keychain of whoever runs this.
func fileStore(t *testing.T) *Store {
	t.Helper()
	return fileStoreIn(t, t.TempDir())
}

func fileStoreIn(t *testing.T, directory string) *Store {
	t.Helper()

	store, err := OpenEncryptedFile(directory, func() (string, error) {
		return "a passphrase for the test", nil
	})
	if err != nil {
		t.Fatalf("opening an encrypted file store: %v", err)
	}
	if store.Kind() != KindEncryptedFile {
		t.Fatalf("the store reports %q", store.Kind())
	}
	return store
}

/** The text between two markers, or empty when either is missing. */
func between(source, start, end string) string {
	from := strings.Index(source, start)
	if from < 0 {
		return ""
	}
	rest := source[from+len(start):]

	to := strings.Index(rest, end)
	if to < 0 {
		return ""
	}
	return rest[:to]
}
