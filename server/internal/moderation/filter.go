package moderation

import (
	"regexp"
	"strings"
	"sync"
	"unicode"
)

/*
Content filtering against an operator's own word list.

No classifier, no external service, no account anywhere: a list of words the
operator wrote, matched literally. That is a deliberate limit rather than a
first step towards something cleverer. A server that cannot filter without
somebody's paid API is a server whose moderation stops when a bill goes unpaid,
and every automated classifier worth the name is somebody's paid API.

What this buys is the case operators actually ask for — a handful of slurs, a
spam domain, a phrase that keeps being pasted — and it does that reliably.
What it does not buy is judgement. It cannot tell a slur from a discussion of
one, so the default action is flagging rather than blocking: a human decides,
and the filter only decides what a human is shown.
*/

// Action is what happens to a message that matches.
type Action string

const (
	// ActionFlag delivers the message and puts it in the moderation queue.
	// The default, because a filter without judgement should hand the
	// judgement to somebody who has it rather than act on its own.
	ActionFlag Action = "flag"
	// ActionMask delivers the message with the matched words replaced.
	ActionMask Action = "mask"
	// ActionBlock refuses the message. The author is told a content policy
	// refused it — and not which word, since that turns the filter into an
	// oracle somebody can probe to learn the list.
	ActionBlock Action = "block"
)

// KnownAction reports whether a name is an action a filter can take.
func KnownAction(action Action) bool {
	switch action {
	case ActionFlag, ActionMask, ActionBlock:
		return true
	}
	return false
}

// Actions lists every action, for a client that would rather ask than hard-code
// a list that can drift.
func Actions() []Action {
	return []Action{ActionFlag, ActionMask, ActionBlock}
}

// Match is what a filter found.
type Match struct {
	// Matched are the words that hit, in the form they appear in the list
	// rather than as typed. Shown to moderators, never to the author.
	Matched []string
	// Masked is the message with matched words replaced, set only when the
	// action is masking.
	Masked string
}

/*
Filter matches text against a word list.

Compiled once and reused: building a pattern per message would put a regexp
compilation on the path of every message sent, which is the busiest path there
is.

Safe for concurrent use — the compiled pattern is read-only after
construction, and Rebuild swaps a whole new one in under the lock rather than
mutating the one in use.
*/
type Filter struct {
	mu      sync.RWMutex
	pattern *regexp.Regexp
	words   []string
}

// NewFilter compiles a filter from a list of words.
func NewFilter(words []string) *Filter {
	filter := &Filter{}
	filter.Rebuild(words)
	return filter
}

/*
Rebuild replaces the word list.

Called when an operator changes the list, so a change takes effect without a
restart — the difference between a setting and a deployment, which is the whole
reason runtime settings exist.
*/
func (f *Filter) Rebuild(words []string) {
	cleaned := make([]string, 0, len(words))
	seen := map[string]bool{}

	for _, word := range words {
		word = strings.TrimSpace(word)
		if word == "" {
			continue
		}
		// Lowercased for the duplicate check as well as the match: a list
		// containing both "Spam" and "spam" is one word twice.
		lower := strings.ToLower(word)
		if seen[lower] {
			continue
		}
		seen[lower] = true
		cleaned = append(cleaned, word)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.words = cleaned
	f.pattern = compile(cleaned)
}

/*
compile builds one pattern matching any word in the list.

One alternation rather than a pattern per word, so matching costs one pass over
the message however long the list grows.

Every word is quoted, so a list is a list of words and not a list of regular
expressions. An operator typing "c++" or "(a)" means those characters; letting
the list be patterns would also mean a mistyped entry could match everything, or
take exponential time to decide it matches nothing.
*/
func compile(words []string) *regexp.Regexp {
	if len(words) == 0 {
		return nil
	}

	quoted := make([]string, 0, len(words))
	for _, word := range words {
		quoted = append(quoted, boundedPattern(word))
	}

	// Case-insensitive: an operator listing a word means it however it is
	// capitalised, and requiring them to list every casing would be a list
	// nobody can maintain.
	pattern, err := regexp.Compile(`(?i)(?:` + strings.Join(quoted, "|") + `)`)
	if err != nil {
		// Every part is quoted, so this cannot fail on operator input. If it
		// somehow does, no filter is the safe outcome: messages go through and
		// a human is still moderating, where a filter that refused everything
		// would take the server down.
		return nil
	}
	return pattern
}

/*
boundedPattern wraps a word so it matches as a word rather than inside one.

Without this, "ass" in the list matches "class" and "passage" — the failure
every naive word filter is known for, and the reason people stop trusting one.

Go's regexp has no lookaround, so the boundary is expressed with \b, which
works where the word starts and ends with a word character. A listed entry
starting or ending with punctuation — a spam domain like "buy.example" — gets
the boundary only on the sides where it means anything, since \b beside a
non-word character asserts the opposite of what is wanted.
*/
func boundedPattern(word string) string {
	quoted := regexp.QuoteMeta(word)

	runes := []rune(word)
	if len(runes) == 0 {
		return quoted
	}

	if isWordRune(runes[0]) {
		quoted = `\b` + quoted
	}
	if isWordRune(runes[len(runes)-1]) {
		quoted += `\b`
	}
	return quoted
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// Words returns the current list, for an operator inspecting it.
func (f *Filter) Words() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()

	words := make([]string, len(f.words))
	copy(words, f.words)
	return words
}

/*
Check tests a message against the list.

Returns whether anything matched, and what. An empty list matches nothing,
which is what makes "disabled by default" the absence of configuration rather
than a separate switch to forget.
*/
func (f *Filter) Check(text string) (Match, bool) {
	f.mu.RLock()
	pattern := f.pattern
	f.mu.RUnlock()

	if pattern == nil || text == "" {
		return Match{}, false
	}

	found := pattern.FindAllString(text, -1)
	if len(found) == 0 {
		return Match{}, false
	}

	// Deduplicated, so a word repeated eleven times reads as one thing to
	// review rather than eleven.
	seen := map[string]bool{}
	matched := make([]string, 0, len(found))
	for _, word := range found {
		lower := strings.ToLower(word)
		if seen[lower] {
			continue
		}
		seen[lower] = true
		matched = append(matched, word)
	}

	return Match{
		Matched: matched,
		Masked:  mask(pattern, text),
	}, true
}

/*
mask replaces every match with asterisks of the same length.

Same length so the shape of the sentence survives — a reader can see something
was removed and roughly how much, which is the difference between a masked
message and a corrupted one.
*/
func mask(pattern *regexp.Regexp, text string) string {
	return pattern.ReplaceAllStringFunc(text, func(match string) string {
		return strings.Repeat("*", len([]rune(match)))
	})
}
