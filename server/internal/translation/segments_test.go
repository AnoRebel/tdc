package translation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type segmentCase struct {
	Name     string              `json:"name"`
	Source   string              `json:"source"`
	Segments []map[string]string `json:"segments"`
}

/*
TestSplitMatchesTheSharedCases holds this implementation to the same answers
as the client's.

Translation runs here for providers reached through the server, and in the
reader's browser for the on-device one; the same message must be cut the same
way in both, or what is sent depends on where it runs.
*/
func TestSplitMatchesTheSharedCases(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "packages", "ui", "app", "lib", "translation.cases.json"))
	if err != nil {
		t.Fatalf("reading the shared cases: %v", err)
	}
	var cases []segmentCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("decoding the shared cases: %v", err)
	}

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			got := Split(c.Source)
			if len(got) != len(c.Segments) {
				t.Fatalf("%d segments, want %d: %#v", len(got), len(c.Segments), got)
			}
			for i, want := range c.Segments {
				text, translate := want["translate"], true
				if kept, ok := want["keep"]; ok {
					text, translate = kept, false
				}
				if got[i].Text != text || got[i].Translate != translate {
					t.Errorf("segment %d = %#v, want %q (translate %v)", i, got[i], text, translate)
				}
			}
			if Join(got) != c.Source {
				t.Errorf("joined back = %q, want the source unchanged", Join(got))
			}
		})
	}
}

// TestReplaceKeepsWhatWasNotSent is the guarantee the segments exist for.
func TestReplaceKeepsWhatWasNotSent(t *testing.T) {
	segments := Split("run `make test` @amina")
	texts := Translatable(segments)
	if len(texts) != 1 || texts[0] != "run" {
		t.Fatalf("sent %q, want only the words", texts)
	}
	if got := Replace(segments, []string{"endesha"}); got != "endesha `make test` @amina" {
		t.Errorf("replaced = %q", got)
	}
}
