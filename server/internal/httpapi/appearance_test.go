package httpapi

import (
	"math"
	"testing"
)

/*
Whether an accent can be read, checked against values the specification fixes.

Not against this implementation's own output, which would pass whatever it
computed. Black on white is 21:1 by definition, and #767676 on white is the
canonical colour that just clears AA — which pins the arithmetic at the only
threshold that changes an outcome.

The same values are pinned in the TypeScript that checks this in a browser.
Two implementations of one rule will drift, and a shared set of fixed answers
is what makes the drift visible.
*/
func TestContrastAgreesWithTheSpecification(t *testing.T) {
	cases := []struct {
		one, other string
		want       float64
		why        string
	}{
		{"#000000", "#ffffff", 21, "black on white is the maximum"},
		{"#ffffff", "#ffffff", 1, "a colour against itself is the minimum"},
		{"#0000ff", "#ffffff", 8.59, "blue on white"},
		{"#767676", "#ffffff", 4.54, "the grey that just clears AA"},
		{"#777777", "#ffffff", 4.48, "and the one just under it"},
	}

	for _, test := range cases {
		one, ok := parseColour(test.one)
		if !ok {
			t.Fatalf("%s: could not read %s", test.why, test.one)
		}
		other, ok := parseColour(test.other)
		if !ok {
			t.Fatalf("%s: could not read %s", test.why, test.other)
		}

		got := contrastRatio(one, other)
		if math.Abs(got-test.want) > 0.05 {
			t.Errorf("%s: expected about %.2f:1, got %.2f:1", test.why, test.want, got)
		}
	}
}

/*
A colour is accepted when a readable pair can be made from it.

The client does not apply a chosen colour directly — one value cannot be read
in both appearances — so what this accepts has to match what the client can
use: a hue whose lightness can be moved until each appearance has a shade its
own text reads on. Accepting a colour the client then cannot use would put the
refusal somewhere nobody sees it.
*/
func TestAColourWithAReadablePairIsAccepted(t *testing.T) {
	for _, accent := range []string{"#4f46e5", "#3730a3", "#facc15", "#b45309", "#0ea5e9"} {
		if err := readableAccent(accent); err != nil {
			t.Errorf("%s can be made readable and was refused: %v", accent, err)
		}
	}
}

/*
The palettes' own accents are accepted, which is the floor.

A check that refused the colours this application ships would be wrong about
the rule rather than strict about it — and that is exactly what the first
version did, requiring one value to clear both foregrounds.
*/
func TestThePalettesOwnAccentsAreAccepted(t *testing.T) {
	for _, accent := range []string{
		"oklch(0.53 0.16 269)", "oklch(0.66 0.16 268)",
		"oklch(0.449 0.15 35)", "oklch(0.738 0.172 46)",
	} {
		if err := readableAccent(accent); err != nil {
			t.Errorf("the application ships %s and this refuses it: %v", accent, err)
		}
	}
}

/*
The Go rule has to agree with the TypeScript that derives the pair.

Two implementations of one rule drift, and the visible failure is a colour the
server accepts and the client then cannot apply — a setting that saves and
does nothing.
*/
func TestTheServerAcceptsWhatTheClientCanDerive(t *testing.T) {
	// Colours the TypeScript derivation is known to handle, checked in
	// packages/ui/app/lib/theme.test.ts against the same foregrounds.
	for _, accent := range []string{"#4f46e5", "#b45309", "#facc15", "#0ea5e9"} {
		if err := readableAccent(accent); err != nil {
			t.Errorf("the client can derive a pair for %s and this refuses it: %v", accent, err)
		}
	}
}

func TestAColourThatCannotBeMeasuredIsRefused(t *testing.T) {
	for _, value := range []string{"rebeccapurple", "rgb(1 2 3)", "not a colour"} {
		if err := readableAccent(value); err == nil {
			t.Errorf("%q was accepted without being measured", value)
		}
	}
}

// The palettes are written in oklch, so a parser that only did hex would
// refuse the application's own colours.
func TestOklchIsRead(t *testing.T) {
	for _, value := range []string{"oklch(0.53 0.16 269)", "oklch(53% 0.16 269)"} {
		if _, ok := parseColour(value); !ok {
			t.Errorf("%q could not be read", value)
		}
	}
}
