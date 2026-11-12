package moderation

import (
	"testing"
	"time"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

// existing builds a stored lifetime for the comparison tests.
func existing(kind LifetimeKind, seconds int32) dbgen.MessageLifetime {
	row := dbgen.MessageLifetime{Kind: string(kind)}
	if kind != LifetimeOnceSeen {
		row.Seconds = &seconds
	}
	return row
}

/*
TestALifetimeCannotBeExtended is the rule the requirement turns on.

Shortening is somebody deciding they want it gone sooner, which is theirs to
decide. Lengthening reaches back into a conversation where everybody was told
this would be gone, and the people who read it on that basis are not there to
be asked again.
*/
func TestALifetimeCannotBeExtended(t *testing.T) {
	sent := time.Now()

	tests := []struct {
		name    string
		from    dbgen.MessageLifetime
		toKind  LifetimeKind
		toSecs  int32
		extends bool
	}{
		{"a shorter duration", existing(LifetimeAfterSending, 3600),
			LifetimeAfterSending, 600, false},
		{"a longer duration", existing(LifetimeAfterSending, 600),
			LifetimeAfterSending, 3600, true},
		{"the same duration", existing(LifetimeAfterSending, 600),
			LifetimeAfterSending, 600, false},

		/*
			Once seen is the shortest thing there is: it goes the moment
			everybody has read it, which is never later than any duration.
		*/
		{"to once seen from a duration", existing(LifetimeAfterSending, 60),
			LifetimeOnceSeen, 0, false},
		{"away from once seen", existing(LifetimeOnceSeen, 0),
			LifetimeAfterSending, 60, true},
		{"away from once seen to after seeing", existing(LifetimeOnceSeen, 0),
			LifetimeAfterSeeing, 60, true},

		/*
			After seeing is unbounded — somebody who never reads it never
			starts their clock — so it can outlast any fixed duration however
			small its own number looks. Five minutes after seeing is not
			shorter than an hour after sending.
		*/
		{"from a duration to after seeing", existing(LifetimeAfterSending, 3600),
			LifetimeAfterSeeing, 300, true},
		/*
			Also an extension, which an earlier draft had backwards.

			After seeing is unbounded above but not below: a reader who looked
			a moment ago is five minutes from losing it, and replacing that
			with any fixed duration longer than what remains of their window
			extends it for them. Since the earliest reader's window cannot be
			known from the kinds alone, the move is refused either way.
		*/
		{"from after seeing to a duration", existing(LifetimeAfterSeeing, 3600),
			LifetimeAfterSending, 300, true},
		{"a shorter after-seeing window", existing(LifetimeAfterSeeing, 3600),
			LifetimeAfterSeeing, 600, false},
		{"a longer after-seeing window", existing(LifetimeAfterSeeing, 600),
			LifetimeAfterSeeing, 3600, true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := extends(test.from, test.toKind, test.toSecs, sent)
			if got != test.extends {
				t.Errorf("extends = %v, want %v (from %s/%v to %s/%d)",
					got, test.extends, test.from.Kind, test.from.Seconds,
					test.toKind, test.toSecs)
			}
		})
	}
}

/*
TestAFixedDurationIsNotTreatedAsShorterThanAnUnboundedOne is the case a naive
comparison gets wrong, kept separate because it is the one worth failing on.

Somebody wanting to keep a message alive could otherwise ask for "five minutes
after seeing" on an hour-old message and have it accepted as a reduction, then
simply never read it.
*/
func TestAFixedDurationIsNotTreatedAsShorterThanAnUnboundedOne(t *testing.T) {
	oneHourFromSending := existing(LifetimeAfterSending, 3600)

	if !extends(oneHourFromSending, LifetimeAfterSeeing, 1, time.Now()) {
		t.Error("a one-second after-seeing lifetime was accepted as shorter " +
			"than an hour from sending; it is unbounded, so it is not")
	}
}

// TestKnownLifetimeKinds keeps a client from offering one the server refuses.
func TestKnownLifetimeKinds(t *testing.T) {
	for _, kind := range LifetimeKinds() {
		if !knownLifetime(kind) {
			t.Errorf("%q is listed but not known", kind)
		}
	}
	if knownLifetime("when_i_say_so") {
		t.Error("a kind the server does not have was accepted")
	}
}
