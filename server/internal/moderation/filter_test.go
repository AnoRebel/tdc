package moderation_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/AnoRebel/tdc/server/internal/moderation"
)

// TestAnEmptyListMatchesNothing is what makes filtering off by default.
//
// Disabled is the absence of configuration rather than a separate switch,
// which is one fewer thing for an operator to set and one fewer thing to
// forget.
func TestAnEmptyListMatchesNothing(t *testing.T) {
	filter := moderation.NewFilter(nil)

	if _, matched := filter.Check("anything at all"); matched {
		t.Error("an empty filter matched something")
	}
}

/*
TestMatchingIsBoundedToWholeWords is the failure every naive word filter is
known for.

A list containing "ass" that flags "class" and "passage" is a filter people
stop trusting, and then stop using. The Scunthorpe problem has a name because
it keeps being shipped.
*/
func TestMatchingIsBoundedToWholeWords(t *testing.T) {
	filter := moderation.NewFilter([]string{"ass", "spam"})

	shouldMatch := []string{
		"that is spam",
		"Spam again",
		"ass",
		"an ass, truly",
		"spam.",
		"(spam)",
	}
	for _, text := range shouldMatch {
		if _, matched := filter.Check(text); !matched {
			t.Errorf("%q did not match but should have", text)
		}
	}

	shouldNotMatch := []string{
		"a class of students",
		"the passage was long",
		"Scunthorpe",
		"assassin",
		"spammer",
		"unspammable",
	}
	for _, text := range shouldNotMatch {
		if _, matched := filter.Check(text); matched {
			t.Errorf("%q matched but should not have", text)
		}
	}
}

/*
TestListEntriesAreWordsNotPatterns keeps a list a list.

An operator typing "c++" means those three characters. Treating entries as
regular expressions would make a mistyped one match everything, and a
pathological one take exponential time to decide it matches nothing — a denial
of service written by the person the filter belongs to.
*/
func TestListEntriesAreWordsNotPatterns(t *testing.T) {
	filter := moderation.NewFilter([]string{"c++", ".*", "(a)"})

	// The literal text matches.
	if _, matched := filter.Check("I write c++ daily"); !matched {
		t.Error("the literal word c++ did not match")
	}

	// And the pattern-looking entries match only themselves.
	if _, matched := filter.Check("harmless words here"); matched {
		t.Error(`".*" was treated as a pattern and matched everything`)
	}
	if _, matched := filter.Check("a"); matched {
		t.Error(`"(a)" was treated as a group and matched a bare a`)
	}
	if _, matched := filter.Check("this is (a) test"); !matched {
		t.Error(`the literal text "(a)" did not match`)
	}
}

// TestPunctuatedEntriesMatch covers a spam domain, which is what an operator
// reaches for this feature to stop.
func TestPunctuatedEntriesMatch(t *testing.T) {
	filter := moderation.NewFilter([]string{"buy.example.test"})

	if _, matched := filter.Check("visit buy.example.test now"); !matched {
		t.Error("a domain in the list did not match")
	}
	if _, matched := filter.Check("visit example.test now"); matched {
		t.Error("a different domain matched")
	}
}

// TestMaskingKeepsTheShapeOfTheSentence is what separates a masked message
// from a corrupted one.
func TestMaskingKeepsTheShapeOfTheSentence(t *testing.T) {
	filter := moderation.NewFilter([]string{"spam"})

	match, matched := filter.Check("this is spam, truly spam")
	if !matched {
		t.Fatal("the message did not match")
	}

	if match.Masked != "this is ****, truly ****" {
		t.Errorf("masked to %q, expected the words replaced and the rest kept",
			match.Masked)
	}
}

// TestMaskingReplacesEveryOccurrence keeps a second use from surviving the
// first being masked.
func TestMaskingReplacesEveryOccurrence(t *testing.T) {
	filter := moderation.NewFilter([]string{"spam"})

	match, _ := filter.Check("spam spam spam")
	if strings.Contains(strings.ToLower(match.Masked), "spam") {
		t.Errorf("masking left an occurrence behind: %q", match.Masked)
	}
}

// TestMatchesAreDeduplicated keeps a review readable.
//
// A word used eleven times is one thing for a moderator to consider, not
// eleven.
func TestMatchesAreDeduplicated(t *testing.T) {
	filter := moderation.NewFilter([]string{"spam"})

	match, _ := filter.Check("spam Spam SPAM spam")
	if len(match.Matched) != 1 {
		t.Errorf("matched %v, expected one entry", match.Matched)
	}
}

// TestTheListIsDeduplicated covers a list somebody edited twice.
func TestTheListIsDeduplicated(t *testing.T) {
	filter := moderation.NewFilter([]string{"spam", "Spam", " spam ", ""})

	if words := filter.Words(); len(words) != 1 {
		t.Errorf("the list holds %v, expected one word", words)
	}
}

/*
TestRebuildTakesEffectWithoutARestart is why the list is a runtime setting.

An operator dealing with a flood of one phrase at two in the morning should not
have to redeploy to stop it.
*/
func TestRebuildTakesEffectWithoutARestart(t *testing.T) {
	filter := moderation.NewFilter([]string{"spam"})

	if _, matched := filter.Check("this is nonsense"); matched {
		t.Fatal("matched before the word was added")
	}

	filter.Rebuild([]string{"spam", "nonsense"})

	if _, matched := filter.Check("this is nonsense"); !matched {
		t.Error("a word added to the list did not take effect")
	}
	if _, matched := filter.Check("this is spam"); !matched {
		t.Error("a word already in the list stopped matching after a rebuild")
	}
}

// TestRemovingAWordStopsItMatching covers the other direction, which is what
// an operator does after deciding a word was too broad.
func TestRemovingAWordStopsItMatching(t *testing.T) {
	filter := moderation.NewFilter([]string{"spam", "nonsense"})
	filter.Rebuild([]string{"spam"})

	if _, matched := filter.Check("this is nonsense"); matched {
		t.Error("a word removed from the list still matched")
	}
}

/*
TestConcurrentUseIsSafe covers the real deployment.

Messages are checked on every send, from every connection at once, while an
operator may be editing the list. A filter that raced here would be a filter
that crashed the server under exactly the load it exists to handle.

Run with -race, which is how the suite runs.
*/
func TestConcurrentUseIsSafe(t *testing.T) {
	filter := moderation.NewFilter([]string{"spam"})

	var waiting sync.WaitGroup
	for range 8 {
		waiting.Add(1)
		go func() {
			defer waiting.Done()
			for range 100 {
				filter.Check("is this spam or not")
			}
		}()
	}

	waiting.Add(1)
	go func() {
		defer waiting.Done()
		for i := range 50 {
			if i%2 == 0 {
				filter.Rebuild([]string{"spam", "nonsense"})
			} else {
				filter.Rebuild([]string{"spam"})
			}
		}
	}()

	waiting.Wait()
}

// TestKnownActions keeps a client from offering an action the server refuses.
func TestKnownActions(t *testing.T) {
	for _, action := range moderation.Actions() {
		if !moderation.KnownAction(action) {
			t.Errorf("%q is listed but not known", action)
		}
	}

	if moderation.KnownAction("delete") {
		t.Error("an action the server does not have was accepted")
	}
}
