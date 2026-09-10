package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

/*
Whether the two halves of the menu still agree.

An event name is a string in Go and a string in TypeScript, and nothing at
either compiler catches them drifting apart. What it looks like when they do is
a menu item that opens nothing, with no error anywhere — so this reads the
generated file and checks every name is in it.
*/
func TestTheFrontendKnowsEveryMenuEvent(t *testing.T) {
	generated := readGenerated(t)

	for _, event := range []string{
		EventSearch,
		EventPreferences,
		EventNewChannel,
		EventNotifications,
		EventKeyboardHelp,
		EventOpenConversation,
	} {
		if !strings.Contains(generated, `"`+event+`"`) {
			t.Errorf("%q is emitted by the menu and unknown to the frontend — "+
				"the item would do nothing; run `go generate ./internal/shell`", event)
		}
	}
}

/*
And that nothing is left in the generated file that Go no longer sends.

The opposite drift, and quieter: the frontend waits for an event that will
never arrive, which looks like a feature that was never finished rather than
one that was renamed.
*/
func TestTheFrontendWaitsForNothingUnsent(t *testing.T) {
	generated := readGenerated(t)

	sent := map[string]bool{
		EventSearch: true, EventPreferences: true, EventNewChannel: true,
		EventNotifications: true, EventKeyboardHelp: true,
		EventOpenConversation: true, EventQuitRequested: true,
		EventMuteFor: true, EventUnmute: true, EventAddServer: true,
		EventNewChannelFromTray: true, EventOpenRoom: true,
		EventAskCloseBehaviour: true, EventConfirmQuit: true,
		EventSetCloseBehaviour: true, EventShowAbout: true,
		EventQuitConfirmed: true,
	}

	for _, line := range strings.Split(generated, "\n") {
		_, value, found := strings.Cut(line, `: "`)
		if !found {
			continue
		}
		name, _, _ := strings.Cut(value, `"`)
		if !sent[name] {
			t.Errorf("the frontend listens for %q, which the menu no longer sends", name)
		}
	}
}

/** The generated event names, as the frontend reads them. */
func readGenerated(t *testing.T) string {
	t.Helper()

	generated, err := os.ReadFile(filepath.Join("..", "..", "frontend", "app", "lib", "menu-events.ts"))
	if err != nil {
		t.Fatalf("the generated event names are missing; run `go generate ./internal/shell`: %v", err)
	}
	return string(generated)
}
