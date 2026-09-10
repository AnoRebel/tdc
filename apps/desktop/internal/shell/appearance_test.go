package shell

import (
	"os"
	"path/filepath"
	"testing"
)

/*
How solid the window is allowed to be.

Translucency is a taste; unreadability is not. The floor exists so a slider
dragged to its end still leaves an application somebody can use.
*/
func TestOpacityStaysReadable(t *testing.T) {
	for _, test := range []struct {
		given Opacity
		want  Opacity
		why   string
	}{
		{100, 100, "fully solid"},
		{85, 85, "a little translucent"},
		{50, 50, "as translucent as this goes"},

		// Below the floor, and above the ceiling.
		{0, 50, "invisible is not a choice this offers"},
		{20, 50, "too faint to read"},
		{-30, 50, "a negative from a slider at its end"},
		{140, 100, "more solid than solid"},
	} {
		if got := test.given.Clamp(); got != test.want {
			t.Errorf("%s: Opacity(%d).Clamp() = %d, want %d",
				test.why, test.given, got, test.want)
		}
	}
}

/*
The default is solid: somebody who has chosen nothing gets a window they can

	certainly read.
*/
func TestTheDefaultIsSolid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "window.json")

	if opacity, saved := readOpacity(path); saved || opacity != Solid {
		t.Errorf("with nothing saved, the opacity is %d (saved=%v)", opacity, saved)
	}
}

func TestAChosenOpacitySurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "window.json")

	writeOpacity(path, 80)

	opacity, saved := readOpacity(path)
	if !saved || opacity != 80 {
		t.Errorf("what came back is %d (saved=%v)", opacity, saved)
	}
}

/*
A file holding something unreadable is clamped, not obeyed.

Written by an older version, or edited by hand. A window at 5% opacity because
of a stray digit is one somebody cannot use to fix the setting.
*/
func TestAnUnreasonableSavedOpacityIsClamped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "window.json")
	if err := os.WriteFile(path, []byte(`{"opacity":3}`), 0o644); err != nil {
		t.Fatalf("writing: %v", err)
	}

	if opacity, _ := readOpacity(path); opacity < leastReadable {
		t.Errorf("a saved opacity of 3 was applied as %d", opacity)
	}
}

func TestAnUnreadableFileFallsBackToSolid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "window.json")
	_ = os.WriteFile(path, []byte("not json"), 0o644)

	if opacity, saved := readOpacity(path); saved || opacity != Solid {
		t.Errorf("a broken file gave %d (saved=%v)", opacity, saved)
	}
}
