/*
Package directory is the opt-in layer that makes a person findable by somebody
who does not already know them.

Everything else on this server discloses to people who share a conversation.
The directory discloses to strangers, which is the opposite instinct, so it is
built as a separate layer rather than a loosening of the first: its own tables,
its own queries, its own visibility settings, and nothing that reads or writes
user_privacy, user_presence or the member-list search.

An account with no profile is a complete account. Creating one is the decision
to be found, and every section of it is disclosed only as its owner chose.
*/
package directory

import (
	"errors"
	"fmt"
)

// Visibility is who may see a profile, or one section of it.
type Visibility string

const (
	// Public is anybody permitted to browse the directory — including somebody
	// not signed in, where the server lets strangers discover it at all.
	Public Visibility = "public"
	// Members is anybody signed in to this server.
	Members Visibility = "members"
	// Hidden is nobody but the owner, and a moderator acting as one.
	Hidden Visibility = "hidden"
)

// Valid reports whether this is one of the three settings.
func (v Visibility) Valid() bool {
	return v == Public || v == Members || v == Hidden
}

/*
Rank is how much of the directory a viewer may see.

A number rather than a role, because the queries compare it with each section's
own rank and a comparison is the whole rule: a field is shown when its rank is
at most the viewer's. The generated columns in the migration hold the other
half.
*/
type Rank int16

const (
	// Anonymous is somebody not signed in.
	Anonymous Rank = 0
	// Member is somebody signed in to this server.
	Member Rank = 1
	// Owner is the person the profile belongs to, or a moderator acting with
	// the permission to see what they see.
	Owner Rank = 2
)

// ErrNoProfile reports that there is no profile the caller may see.
//
// One error for "no such person", "no profile" and "not yours to see", because
// telling them apart would answer the question a hidden profile declines to.
var ErrNoProfile = errors.New("no such profile")

/*
InvalidError is a refusal the person can act on: which field, and what is wrong
with it.

Returned rather than logged, and phrased for somebody filling in a form,
because the handler passes it through as the response.
*/
type InvalidError struct {
	Field  string
	Reason string
}

func (e *InvalidError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Reason)
}

func invalid(field, reason string) error {
	return &InvalidError{Field: field, Reason: reason}
}
