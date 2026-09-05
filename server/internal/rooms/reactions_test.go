package rooms_test

import (
	"testing"

	"github.com/AnoRebel/tdc/server/internal/rooms"
)

// TestReactionsRefuseText is the check that keeps the column an emoji field.
//
// Without it a reaction is a second message body: unbounded text, rendered
// beside every message, with none of the length limits or moderation that a
// message goes through.
func TestReactionsRefuseText(t *testing.T) {
	refused := []struct {
		name  string
		value string
	}{
		{"a word", "lol"},
		{"a sentence", "this is not an emoji"},
		{"a single letter", "a"},
		{"a digit", "1"},
		{"empty", ""},
		{"only a space", " "},
		{"an emoji with a word", "👍 nice"},
		{"a long sequence", "👍👍👍👍👍👍👍👍👍👍👍👍👍👍👍👍👍👍👍👍👍👍👍👍👍👍👍👍"},
	}

	for _, test := range refused {
		t.Run(test.name, func(t *testing.T) {
			if rooms.ValidEmojiForTest(test.value) {
				t.Errorf("%q was accepted as a reaction", test.value)
			}
		})
	}
}

// TestReactionsAcceptEmoji covers what people actually send, including the
// awkward ones.
//
// A flag is two regional indicators, a family is several people joined by
// zero-width joiners, and a skin tone is a modifier — all of which are one
// emoji to a reader and several code points to a program.
func TestReactionsAcceptEmoji(t *testing.T) {
	accepted := []struct {
		name  string
		value string
	}{
		{"a plain emoji", "👍"},
		{"a flag", "🇹🇿"},
		{"a skin tone", "👍🏾"},
		{"a family joined by zero-width joiners", "👨‍👩‍👧‍👦"},
		{"a symbol with a variation selector", "❤️"},
		{"a punctuation-shaped emoji", "‼️"},
	}

	for _, test := range accepted {
		t.Run(test.name, func(t *testing.T) {
			if !rooms.ValidEmojiForTest(test.value) {
				t.Errorf("%q was refused as a reaction", test.value)
			}
		})
	}
}
