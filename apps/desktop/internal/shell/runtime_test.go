package shell

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

/*
The Go side and the JavaScript side are the same version of Wails.

They are two halves of one bridge, released together and versioned together.
When they disagree the application builds, starts, opens a window — and the
window is blank, with nothing in any log to say why. That is a costly hour, and
it has already cost one.

Read from the files rather than at runtime, because by the time a mismatch
shows itself there is no runtime left to ask.
*/
func TestTheGoAndJavaScriptRuntimesMatch(t *testing.T) {
	root := filepath.Join("..", "..")

	goMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("reading go.mod: %v", err)
	}

	goVersion := regexp.MustCompile(`github\.com/wailsapp/wails/v3 (v[\w.\-]+)`).
		FindStringSubmatch(string(goMod))
	if goVersion == nil {
		t.Fatal("go.mod names no Wails version")
	}

	manifest, err := os.ReadFile(filepath.Join(root, "frontend", "package.json"))
	if err != nil {
		t.Fatalf("reading the frontend manifest: %v", err)
	}

	var parsed struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal(manifest, &parsed); err != nil {
		t.Fatalf("the frontend manifest could not be read: %v", err)
	}

	js := parsed.Dependencies["@wailsio/runtime"]
	if js == "" {
		t.Fatal("the frontend depends on no Wails runtime")
	}

	/*
		Compared as bare versions. Go writes v3.0.0-beta.19 and npm writes
		3.0.0-beta.19, and a range like ^3.0.0-beta.18 would silently resolve
		to something else again — so a caret is refused rather than
		interpreted.
	*/
	if strings.ContainsAny(js, "^~*") {
		t.Errorf("the frontend allows a range of Wails runtimes (%q); pin it, "+
			"because a runtime newer or older than the Go side gives a blank window", js)
	}

	want := strings.TrimPrefix(goVersion[1], "v")
	if strings.TrimPrefix(js, "v") != want {
		t.Errorf("Go uses Wails %s and the frontend uses %s — "+
			"the window will open blank with nothing in any log", want, js)
	}
}
