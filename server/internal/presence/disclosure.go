package presence

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
What a person lets a member list say about them.

A channel's members can see who else is in it. A name and an address are not
negotiable — they are how a message is attributed and a conversation is opened
— and everything past that is the person's own to withhold.

These are standing preferences, distinct from appearing invisible. Invisible is
a choice about this moment and lapses when it is turned off; these hold until
changed. Somebody who has withheld their presence has not gone quiet for the
afternoon, they have said the question is not answered for them.
*/
type Disclosure struct {
	// Presence is whether others may see them as online or offline at all.
	Presence bool
	// Status is the status they have chosen and the text explaining it. One
	// field for both: a status without its text discloses the fact while
	// withholding the part that made it useful.
	Status bool
	// LastSeen is when they were last here. The most revealing of the three,
	// and separate from Presence because "online now" and "last here at 03:12"
	// disclose quite different things.
	LastSeen bool
}

// DisclosesEverything is what somebody who has never changed these gets.
//
// Disclosing, because a server whose members cannot see who is around is not
// this product, and a default of silence would have made the member list
// useless on the day it shipped. The setting exists so somebody can choose
// otherwise, and the choosing is what it records.
func DisclosesEverything() Disclosure {
	return Disclosure{Presence: true, Status: true, LastSeen: true}
}

// DisclosureFor reads what a person has chosen to disclose.
//
// The absence of a row is not an error: user_privacy is written on the first
// change, so most people have none and disclose everything.
func (s *Service) DisclosureFor(ctx context.Context, userID uuid.UUID) (Disclosure, error) {
	row, err := s.store.GetDisclosurePreferences(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return DisclosesEverything(), nil
	}
	if err != nil {
		return Disclosure{}, fmt.Errorf("read disclosure preferences: %w", err)
	}

	return Disclosure{
		Presence: row.ShowsPresence,
		Status:   row.ShowsStatus,
		LastSeen: row.ShowsLastSeen,
	}, nil
}

// SetDisclosure records what a person is willing to have shown.
//
// All three together, because the interface presents them together: writing
// them in one statement means two concurrent changes cannot leave a mixture of
// old and new.
func (s *Service) SetDisclosure(ctx context.Context, userID uuid.UUID, disclosure Disclosure) error {
	err := s.store.SetDisclosurePreferences(ctx, dbgen.SetDisclosurePreferencesParams{
		UserID:        userID,
		ShowsPresence: disclosure.Presence,
		ShowsStatus:   disclosure.Status,
		ShowsLastSeen: disclosure.LastSeen,
	})
	if err != nil {
		return fmt.Errorf("write disclosure preferences: %w", err)
	}
	return nil
}
