package updater

import (
	"os"
	"path/filepath"
	"testing"
)

/*
The trust root this build actually ships with.

Until there is a release pipeline it is a placeholder, and the whole safety of
that arrangement rests on the placeholder being unusable rather than merely
empty-looking. A file that happened to parse would leave this build accepting
signatures it should not.

Read from the same file main.go embeds, so the thing tested is the thing that
ships.
*/
func TestTheShippedTrustRootDisablesUpdates(t *testing.T) {
	shipped, err := os.ReadFile(filepath.Join("..", "..", "build", "trust", "sigstore-root.json"))
	if err != nil {
		t.Fatalf("the embedded trust root is missing: %v", err)
	}

	SetTrustedRoot(shipped)
	t.Cleanup(func() { SetTrustedRoot(nil) })

	if Enabled() {
		t.Error("the placeholder trust root parses as a real one, so this build " +
			"believes it can verify updates that nobody has signed")
	}
}
