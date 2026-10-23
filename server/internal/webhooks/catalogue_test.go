package webhooks_test

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/AnoRebel/tdc/server/internal/rooms"
	"github.com/AnoRebel/tdc/server/internal/webhooks"
)

// Every type is named once, can be asked for somewhere, and the one named
// here by its string is the type the rooms package stores.
func TestTheCatalogueIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	scopes := []string{webhooks.ScopeChannel, webhooks.ScopeProject, webhooks.ScopeServer}
	for _, kind := range webhooks.Catalogue {
		if seen[kind.Name] {
			t.Errorf("%s is in the catalogue twice", kind.Name)
		}
		seen[kind.Name] = true
		if kind.Group == "" || len(kind.Scopes) == 0 {
			t.Errorf("%s has no group or no scope", kind.Name)
		}
		for _, scope := range kind.Scopes {
			if !slices.Contains(scopes, scope) {
				t.Errorf("%s names an unknown scope %q", kind.Name, scope)
			}
		}
		// An event in no room can only be heard server-wide, and only with
		// a permission; nothing else needs one.
		if kind.Permission != "" && !slices.Equal(kind.Scopes, []string{webhooks.ScopeServer}) {
			t.Errorf("%s needs a permission but can be asked for in a room", kind.Name)
		}
	}
	if !seen[string(rooms.TypeEngagementChanged)] {
		t.Errorf("the catalogue does not offer %s", rooms.TypeEngagementChanged)
	}
}

// The published catalogue lists every type, so a receiver's author can find
// what they will be sent.
func TestEveryTypeIsDocumented(t *testing.T) {
	document, err := os.ReadFile("../../../docs/webhooks-and-apps.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range webhooks.Catalogue {
		if !strings.Contains(string(document), "`"+kind.Name+"`") {
			t.Errorf("docs/webhooks-and-apps.md does not document %s", kind.Name)
		}
	}
}
