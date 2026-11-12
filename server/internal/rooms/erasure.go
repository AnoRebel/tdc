package rooms

import (
	"context"
	"fmt"
	"uuid"
)

/*
Erasing a person's data on request.

Bounded and documented, which the specification asks for by name — and the
documentation is the harder half. An operator has to be able to say exactly what
was removed and exactly what remains, because under Tanzania's Personal Data
Protection Act and the GDPR a partial erasure described as a complete one is
worse than an honest partial one.

What remains, and why:

  - The events themselves, emptied. Removing the rows would break the ordering
    and the verifiability of every conversation this person took part in, which
    is other people's data as much as theirs.
  - That an account once existed, in the form 'erased@thisserver' on those
    events. A conversation where one participant's messages have no author at
    all reads as corrupted rather than as redacted.
  - Audit entries, with the name removed. An audit log with holes is not one.
  - Reports made by other people about this person's messages, with the copied
    content removed but the report intact — the complaint belongs to whoever
    made it.

Everything else goes with the account: credentials, sessions, push
subscriptions, notifications, memberships, reactions, blocks, preferences.
*/

// Erasure is what was removed, so a request can be answered with specifics.
type Erasure struct {
	// MessagesEmptied is how many of this person's events lost their content.
	MessagesEmptied int64
	// AuthorshipRemoved is how many events no longer name them.
	AuthorshipRemoved int64
	// SearchEntriesRemoved is how many copies the index held.
	SearchEntriesRemoved int64
	// ReportedCopiesRemoved is how many reports held a copy of their words.
	ReportedCopiesRemoved int64
	// AuditEntriesAnonymised is how many log entries no longer name them.
	AuditEntriesAnonymised int64
}

/*
Erase removes a person's data and reports what it removed.

The order matters. Content goes before authorship, because the authorship
statement finds events by sender and the content statement finds them the same
way — doing it the other way round works, but reads as though it might not. The
account goes last, because everything cascading from it would otherwise take the
rows the earlier statements need.
*/
func (s *Service) Erase(ctx context.Context, userID uuid.UUID) (Erasure, error) {
	var erased Erasure
	var err error

	if erased.SearchEntriesRemoved, err = s.store.ErasePersonsSearchIndex(ctx, &userID); err != nil {
		return Erasure{}, fmt.Errorf("erase the search index: %w", err)
	}
	if erased.ReportedCopiesRemoved, err = s.store.ErasePersonsReportedContent(ctx, &userID); err != nil {
		return Erasure{}, fmt.Errorf("erase the reported copies: %w", err)
	}
	if erased.MessagesEmptied, err = s.store.ErasePersonsEvents(ctx, &userID); err != nil {
		return Erasure{}, fmt.Errorf("erase the messages: %w", err)
	}
	if erased.AuthorshipRemoved, err = s.store.ErasePersonsAuthorship(ctx, &userID); err != nil {
		return Erasure{}, fmt.Errorf("erase the authorship: %w", err)
	}
	if erased.AuditEntriesAnonymised, err = s.store.ErasePersonsAuditLabel(ctx, &userID); err != nil {
		return Erasure{}, fmt.Errorf("anonymise the audit entries: %w", err)
	}

	if err := s.store.EraseAccount(ctx, userID); err != nil {
		return Erasure{}, fmt.Errorf("remove the account: %w", err)
	}

	return erased, nil
}
