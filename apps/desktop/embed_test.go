package main

import (
	"io/fs"
	"strings"
	"testing"
)

/*
Whether the binary carries a working application or an empty shell.

A Wails build succeeds whether or not the frontend made it in, and the failure
shows as a window opening onto a blank page — at which point the obvious
suspicion is the frontend, which built fine. These check the embed itself.
*/

// The shell, without which there is no application at all.
func TestTheBinaryCarriesTheApplication(t *testing.T) {
	if _, err := assets.ReadFile("frontend/dist/index.html"); err != nil {
		t.Fatalf("no index.html was embedded, so the window would open onto nothing: %v", err)
	}
}

/*
Everything index.html asks for, which is the part that quietly goes missing.

Nuxt writes its chunks to _nuxt and its translations to _locales, and Go's
embed skips names beginning with an underscore unless the directive says
`all:`. Without it the binary carries a page and none of the code it refers
to — a build that succeeds, ships, and opens blank.
*/
func TestTheBinaryCarriesWhatThePageNeeds(t *testing.T) {
	wanted := map[string]bool{"_nuxt": false, "_locales": false}

	err := fs.WalkDir(assets, "frontend/dist", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		for name := range wanted {
			if entry.IsDir() && entry.Name() == name {
				wanted[name] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the embedded files: %v", err)
	}

	for name, found := range wanted {
		if !found {
			t.Errorf("%s was not embedded — the embed directive is missing its `all:` prefix", name)
		}
	}
}

/*
The page reaches the assets by the paths the embed actually holds.

An asset server rooted somewhere other than where the files are is the same
blank window with a different cause, and neither says anything in a log.
*/
func TestThePageAndTheAssetsAgree(t *testing.T) {
	page, err := assets.ReadFile("frontend/dist/index.html")
	if err != nil {
		t.Fatalf("reading the embedded page: %v", err)
	}

	if !strings.Contains(string(page), "/_nuxt/") {
		t.Skip("this page references no chunks, so there is nothing to check")
	}

	entries, err := fs.ReadDir(assets, "frontend/dist/_nuxt")
	if err != nil {
		t.Fatalf("the page asks for /_nuxt/ and the binary has no such directory: %v", err)
	}
	if len(entries) == 0 {
		t.Error("the _nuxt directory was embedded empty")
	}
}
