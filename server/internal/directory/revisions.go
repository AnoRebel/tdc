package directory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
Revising an agreement that is in force.

The rules are the agreement's own, applied again. Either party opens a revision
with a reason and amended terms; that is a new version, never changed once
sent. Either may answer the latest version — accept it, decline it, or ask for
a change to one part — and either may send a counter-version. Only when both
have accepted the same version is a new agreement recorded, beside the earlier
ones; until then the agreement already in force is the one in force, and
nothing about the work changes.

The rules this adds:

  - A revision can be opened once there is an agreement and until the work is
    confirmed complete: in agreement, while the work is under way, and while
    completion awaits confirmation. Not after completion, and never on a
    withdrawn engagement.
  - One revision is open at a time. A second waits until the first ends.
  - It ends in one of three ways: both accepted the same version (a new
    agreement is in force), it was declined by either, or the person who
    opened it withdrew it.
  - While one is open, the work cannot be recorded as complete — neither asked
    for nor confirmed — because "complete" has to mean complete to terms both
    know.
*/

// RevisionOutcome is how a revision ended.
type RevisionOutcome string

const (
	RevisionAccepted  RevisionOutcome = "accepted"
	RevisionDeclined  RevisionOutcome = "declined"
	RevisionWithdrawn RevisionOutcome = "withdrawn"
)

const maxReason = 1000

var (
	// ErrRevisionOpen reports opening a revision while another is open, or
	// finishing the work while one is.
	ErrRevisionOpen = errors.New("a revision of the agreement is open; it has to be accepted, declined or withdrawn first")
	// ErrNoRevision reports a revision that is not open, or not there.
	ErrNoRevision = errors.New("there is no such open revision")
	// ErrNotOpener reports withdrawing a revision somebody else opened.
	ErrNotOpener = errors.New("only the person who opened a revision withdraws it; the other party declines it")
)

// Revision is one revision of an engagement's agreement.
type Revision struct {
	ID       uuid.UUID
	Number   int32
	OpenedBy string
	Reason   string
	OpenedAt time.Time
	// Outcome is empty while the revision is open.
	Outcome   RevisionOutcome
	OutcomeBy string
	// OutcomeNote is the reason given for declining or withdrawing, if any.
	OutcomeNote string
	OutcomeAt   *time.Time
}

// Open reports whether a revision is still being negotiated.
func (r Revision) Open() bool { return r.Outcome == "" }

// Revisable reports whether an agreement may be revised in this state.
func (s State) Revisable() bool {
	return s == StateAgreement || s == StateActive || s == StateCompletionPending
}

func revisionsOf(ctx context.Context, queries *dbgen.Queries, engagementID uuid.UUID) ([]Revision, error) {
	rows, err := queries.ListRevisions(ctx, engagementID)
	if err != nil {
		return nil, fmt.Errorf("read revisions: %w", err)
	}
	revisions := make([]Revision, 0, len(rows))
	for _, row := range rows {
		revision := Revision{
			ID: row.ID, Number: row.Number, OpenedBy: row.OpenedByLabel, Reason: row.Reason,
			OpenedAt: row.OpenedAt, OutcomeAt: row.OutcomeAt,
		}
		if row.Outcome != nil {
			revision.Outcome = RevisionOutcome(*row.Outcome)
		}
		if row.OutcomeBy != nil {
			revision.OutcomeBy = *row.OutcomeBy
		}
		if row.OutcomeNote != nil {
			revision.OutcomeNote = *row.OutcomeNote
		}
		revisions = append(revisions, revision)
	}
	return revisions, nil
}

// lockedEngagement reads an engagement for a party and holds it for the rest
// of the transaction, so steps on one engagement happen one at a time.
func lockedEngagement(ctx context.Context, queries *dbgen.Queries, id, actor uuid.UUID) (Engagement, error) {
	if err := queries.LockEngagement(ctx, id); err != nil {
		return Engagement{}, fmt.Errorf("hold an engagement: %w", err)
	}
	return engagementWith(ctx, queries, id, actor)
}

// openRevision returns the revision being negotiated, or nil.
func openRevision(ctx context.Context, queries *dbgen.Queries, engagement Engagement) (*dbgen.AgreementRevision, error) {
	row, err := queries.GetOpenRevision(ctx, engagement.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the open revision: %w", err)
	}
	return &row, nil
}

func refuseWhileRevising(ctx context.Context, queries *dbgen.Queries, engagement Engagement) error {
	open, err := openRevision(ctx, queries, engagement)
	if err != nil {
		return err
	}
	if open != nil {
		return ErrRevisionOpen
	}
	return nil
}

func sameTerms(input ProposalInput, terms ProposalVersion) bool {
	return input.Scope == terms.Scope && input.Timeline == terms.Timeline &&
		input.Currency == terms.Currency && slices.Equal(input.LineItems, terms.LineItems)
}

/*
OpenRevision opens a revision of the agreement in force, with the reason for
it and the amended terms as its first version.

The terms must differ from the agreement in force: a revision that changes
nothing would ask both people to agree again to what they already agreed.
*/
func (s *Service) OpenRevision(
	ctx context.Context,
	engagementID, actor uuid.UUID,
	actorLabel, reason string,
	input ProposalInput,
) (Revision, ProposalVersion, Engagement, error) {
	reason = strings.TrimSpace(reason)
	switch {
	case reason == "":
		return Revision{}, ProposalVersion{}, Engagement{}, invalid("reason", "say why the agreement should change")
	case utf8.RuneCountInString(reason) > maxReason:
		return Revision{}, ProposalVersion{}, Engagement{}, invalid("reason", fmt.Sprintf("at most %d characters", maxReason))
	}
	if err := input.Normalise(); err != nil {
		return Revision{}, ProposalVersion{}, Engagement{}, err
	}
	items, err := json.Marshal(input.LineItems)
	if err != nil {
		return Revision{}, ProposalVersion{}, Engagement{}, fmt.Errorf("encode a revision: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Revision{}, ProposalVersion{}, Engagement{}, fmt.Errorf("begin revising: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)

	engagement, err := lockedEngagement(ctx, queries, engagementID, actor)
	if err != nil {
		return Revision{}, ProposalVersion{}, Engagement{}, err
	}
	if !engagement.State.Revisable() {
		return Revision{}, ProposalVersion{}, Engagement{}, ErrNotNow
	}
	if err := refuseWhileRevising(ctx, queries, engagement); err != nil {
		return Revision{}, ProposalVersion{}, Engagement{}, err
	}
	inForce, err := queries.GetAgreement(ctx, engagementID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Revision{}, ProposalVersion{}, Engagement{}, ErrNoAgreement
	}
	if err != nil {
		return Revision{}, ProposalVersion{}, Engagement{}, fmt.Errorf("read the agreement in force: %w", err)
	}
	if sameTerms(input, agreementOf(inForce).Terms) {
		return Revision{}, ProposalVersion{}, Engagement{}, invalid("terms", "the revision changes nothing in the agreement in force")
	}

	row, err := queries.InsertRevision(ctx, dbgen.InsertRevisionParams{
		ID: uuid.New(), EngagementID: engagementID, OpenedBy: &actor, OpenedByLabel: actorLabel, Reason: reason,
	})
	if isUniqueViolation(err) {
		return Revision{}, ProposalVersion{}, Engagement{}, ErrChanged
	}
	if err != nil {
		return Revision{}, ProposalVersion{}, Engagement{}, fmt.Errorf("open a revision: %w", err)
	}
	version, err := queries.InsertProposalVersion(ctx, dbgen.InsertProposalVersionParams{
		ID: uuid.New(), EngagementID: engagementID, SentBy: &actor, SentByLabel: actorLabel,
		Scope: input.Scope, Timeline: input.Timeline, LineItems: items,
		Currency: input.Currency, TotalMinor: input.Total(), RevisionID: &row.ID,
	})
	if isUniqueViolation(err) {
		return Revision{}, ProposalVersion{}, Engagement{}, ErrChanged
	}
	if err != nil {
		return Revision{}, ProposalVersion{}, Engagement{}, fmt.Errorf("send a revision's terms: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Revision{}, ProposalVersion{}, Engagement{}, fmt.Errorf("commit a revision: %w", err)
	}

	sent := versionOf(version)
	sent.Revision = row.Number
	revision := Revision{
		ID: row.ID, Number: row.Number, OpenedBy: row.OpenedByLabel, Reason: row.Reason, OpenedAt: row.OpenedAt,
	}
	updated, err := s.Engagement(ctx, engagementID, actor)
	return revision, sent, updated, err
}

/*
answerRevision records an answer to the latest version of an open revision.

Accepting counts towards an agreement on that version; the second party's
acceptance records it. Declining ends the revision, leaving the agreement in
force as it was. Asking for a change keeps the revision open and waiting for a
new version, and the version asked about can no longer be accepted.
*/
func answerRevision(
	ctx context.Context,
	queries *dbgen.Queries,
	engagement Engagement,
	latest dbgen.ProposalVersion,
	actor uuid.UUID,
	actorLabel string,
	kind ResponseKind,
	part, comment string,
) (Answer, error) {
	if !engagement.State.Revisable() {
		return Answer{}, ErrNotNow
	}
	revision, err := openRevision(ctx, queries, engagement)
	if err != nil {
		return Answer{}, err
	}
	if revision == nil || revision.ID != *latest.RevisionID {
		return Answer{}, ErrNotNow
	}
	answer := Answer{Revision: revision.Number}

	if kind == Accept {
		responses, err := queries.ListProposalResponses(ctx, engagement.ID)
		if err != nil {
			return Answer{}, fmt.Errorf("read responses: %w", err)
		}
		for _, response := range responses {
			if response.VersionID == latest.ID && response.Kind == string(RequestChanges) {
				return Answer{}, ErrNotNow
			}
		}
	}
	if err := recordResponse(ctx, queries, latest, actor, actorLabel, kind, part, comment); err != nil {
		return Answer{}, err
	}

	switch kind {
	case Decline:
		if err := queries.InsertRevisionOutcome(ctx, dbgen.InsertRevisionOutcomeParams{
			RevisionID: revision.ID, EngagementID: engagement.ID, Outcome: string(RevisionDeclined),
			ActorID: &actor, ActorLabel: actorLabel, Note: comment,
		}); err != nil {
			return Answer{}, fmt.Errorf("close a declined revision: %w", err)
		}
		answer.Closed = true
	case Accept:
		answer.Agreed, err = recordAgreementIfBoth(ctx, queries, engagement, latest, revision, actor, actorLabel)
		if err != nil {
			return Answer{}, err
		}
	case RequestChanges:
	}
	return answer, nil
}

/*
WithdrawRevision ends an open revision, by the person who opened it, with an
optional reason. The agreement in force stays as it was.
*/
func (s *Service) WithdrawRevision(
	ctx context.Context,
	engagementID, actor uuid.UUID,
	actorLabel string,
	number int32,
	reason string,
) (Revision, Engagement, error) {
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(reason) > maxReason {
		return Revision{}, Engagement{}, invalid("reason", fmt.Sprintf("at most %d characters", maxReason))
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Revision{}, Engagement{}, fmt.Errorf("begin withdrawing a revision: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)

	engagement, err := lockedEngagement(ctx, queries, engagementID, actor)
	if err != nil {
		return Revision{}, Engagement{}, err
	}
	revision, err := openRevision(ctx, queries, engagement)
	if err != nil {
		return Revision{}, Engagement{}, err
	}
	if revision == nil || revision.Number != number {
		return Revision{}, Engagement{}, ErrNoRevision
	}
	if revision.OpenedBy == nil || *revision.OpenedBy != actor {
		return Revision{}, Engagement{}, ErrNotOpener
	}
	if err := queries.InsertRevisionOutcome(ctx, dbgen.InsertRevisionOutcomeParams{
		RevisionID: revision.ID, EngagementID: engagement.ID, Outcome: string(RevisionWithdrawn),
		ActorID: &actor, ActorLabel: actorLabel, Note: reason,
	}); err != nil {
		return Revision{}, Engagement{}, fmt.Errorf("withdraw a revision: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Revision{}, Engagement{}, fmt.Errorf("commit withdrawing a revision: %w", err)
	}
	now := time.Now()
	withdrawn := Revision{
		ID: revision.ID, Number: revision.Number, OpenedBy: revision.OpenedByLabel, Reason: revision.Reason,
		OpenedAt: revision.OpenedAt, Outcome: RevisionWithdrawn, OutcomeBy: actorLabel, OutcomeNote: reason,
		OutcomeAt: &now,
	}
	updated, err := s.Engagement(ctx, engagementID, actor)
	return withdrawn, updated, err
}
