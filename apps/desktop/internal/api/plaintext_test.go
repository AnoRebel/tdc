package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The server's cases, which are the web client's too: this copy of the rules
// is only worth having while it agrees with theirs.
func TestPlainTextAgreesWithTheServer(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "packages", "ui", "app", "lib", "plaintext.cases.json"))
	if err != nil {
		t.Fatalf("reading the shared cases: %v", err)
	}
	var cases []struct{ Name, Source, Plain string }
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("parsing the shared cases: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("no cases")
	}

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			if got := plainText(c.Source); got != c.Plain {
				t.Errorf("plainText(%q)\n got %q\nwant %q", c.Source, got, c.Plain)
			}
		})
	}
}

// A banner shows the words of a message, not its Markdown.
func TestABannerShowsTheWordsNotTheMarkdown(t *testing.T) {
	bridge, spy := bridgeWith(t, "me@example.test")
	bridge.notify(messageFrame("someone@example.test", "**Build** passed: `go test`", nil))

	if spy.count() != 1 {
		t.Fatalf("expected one banner, got %d", spy.count())
	}
	if got, want := spy.shown[0].body, "Build passed: go test"; got != want {
		t.Errorf("banner body = %q, want %q", got, want)
	}
}
