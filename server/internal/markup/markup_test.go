package markup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The cases the web client and the desktop application are tested against
// too, so the three cannot drift apart on what a message says without its
// formatting.
func TestPlainText(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "packages", "ui", "app", "lib", "plaintext.cases.json"))
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
			if got := PlainText(c.Source); got != c.Plain {
				t.Errorf("PlainText(%q)\n got %q\nwant %q", c.Source, got, c.Plain)
			}
		})
	}
}

func TestWithoutCode(t *testing.T) {
	got := WithoutCode("hi @amina `@juma`\n```\n@baraka\n```\nbye")
	if want := "hi @amina \nbye"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
