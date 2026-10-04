package directory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
Engagements: what an enquiry becomes when it is taken up.

Enquiry → discussion → proposal → negotiation (revised, accepted or declined, as
often as it takes) → agreement → active → completion pending → completed, with
either person able to withdraw at any point before an agreement. Each step is
one person doing one thing, recorded with who and when, and both people always
read the same state from the same row.

The server records; it does not decide. Nothing here infers that work was done
well, or done at all — completed means both people said so — and after an
agreement there is no state the server could rule an engagement into: the two
of them carry on, or one says the work is complete and the other does not, and
that stays visible as outstanding rather than being resolved for them.
*/

// State is where an engagement stands.
type State string

const (
	// StateEnquiry is the enquiry before it was taken up. It is never an
	// engagement's state, only where its first transition comes from.
	StateEnquiry           State = "enquiry"
	StateDiscussion        State = "discussion"
	StateProposal          State = "proposal"
	StateNegotiation       State = "negotiation"
	StateAgreement         State = "agreement"
	StateActive            State = "active"
	StateCompletionPending State = "completion_pending"
	StateCompleted         State = "completed"
	StateWithdrawn         State = "withdrawn"
)

/*
allowed is every move a state can make.

The database holds the same list as a constraint on the history table, so a
step that skips another fails twice: here, with a refusal a person can read,
and there, for any code path that forgot to ask.
*/
var allowed = map[State][]State{
	StateEnquiry:           {StateDiscussion},
	StateDiscussion:        {StateProposal, StateWithdrawn},
	StateProposal:          {StateNegotiation, StateAgreement, StateWithdrawn},
	StateNegotiation:       {StateProposal, StateAgreement, StateWithdrawn},
	StateAgreement:         {StateActive},
	StateActive:            {StateCompletionPending},
	StateCompletionPending: {StateCompleted},
}

// Withdrawable reports whether an engagement may still be withdrawn from:
// only before an agreement exists.
func (s State) Withdrawable() bool {
	for _, next := range allowed[s] {
		if next == StateWithdrawn {
			return true
		}
	}
	return false
}

var (
	// ErrNoEngagement reports an engagement the caller is not a party to, or
	// none. The same answer for both: nobody outside an engagement may learn
	// it exists.
	ErrNoEngagement = errors.New("no such engagement")
	// ErrNotNow reports a step the engagement's state does not allow.
	ErrNotNow = errors.New("that cannot be done at this stage of the engagement")
	// ErrChanged reports a step taken against a state somebody else has since
	// moved on from.
	ErrChanged = errors.New("the engagement changed while you were looking at it; reload it")
)

const maxTransitionNote = 500

// Engagement is one engagement as either of its parties sees it.
type Engagement struct {
	ID             uuid.UUID
	EnquiryID      *uuid.UUID
	ClientID       *uuid.UUID
	DeveloperID    *uuid.UUID
	ClientLabel    string
	DeveloperLabel string
	Title          string
	RoomID         *uuid.UUID
	State          State
	// CompletionRequestedBy is who asked for the work to be recorded as
	// complete, while it waits for the other.
	CompletionRequestedBy *uuid.UUID
	CompletionRequestedAt *time.Time
	// ProjectID is the project the agreed work is tracked in, if linked.
	ProjectID   *uuid.UUID
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Transitions []Transition
}

// Transition is one change of state.
type Transition struct {
	From       State
	To         State
	Actor      string
	Note       string
	OccurredAt time.Time
}

// Party reports whether somebody is the client or the developer.
func (e Engagement) Party(userID uuid.UUID) bool {
	return (e.ClientID != nil && *e.ClientID == userID) || (e.DeveloperID != nil && *e.DeveloperID == userID)
}

// IsDeveloper reports whether somebody is the developer, who was asked.
func (e Engagement) IsDeveloper(userID uuid.UUID) bool {
	return e.DeveloperID != nil && *e.DeveloperID == userID
}

func engagementOf(row dbgen.Engagement) Engagement {
	return Engagement{
		ID: row.ID, EnquiryID: row.EnquiryID, ClientID: row.ClientID, DeveloperID: row.DeveloperID,
		ClientLabel: row.ClientLabel, DeveloperLabel: row.DeveloperLabel, Title: row.Title,
		RoomID: row.RoomID, State: State(row.State),
		CompletionRequestedBy: row.CompletionRequestedBy, CompletionRequestedAt: row.CompletionRequestedAt,
		ProjectID: row.ProjectID,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

// Engagement reads one engagement, with its history, for one of its parties.
func (s *Service) Engagement(ctx context.Context, id, viewer uuid.UUID) (Engagement, error) {
	return engagementWith(ctx, s.queries, id, viewer)
}

func engagementWith(ctx context.Context, queries *dbgen.Queries, id, viewer uuid.UUID) (Engagement, error) {
	row, err := queries.GetEngagementFor(ctx, dbgen.GetEngagementForParams{ID: id, Viewer: viewer})
	if errors.Is(err, pgx.ErrNoRows) {
		return Engagement{}, ErrNoEngagement
	}
	if err != nil {
		return Engagement{}, fmt.Errorf("read an engagement: %w", err)
	}
	engagement := engagementOf(row)

	transitions, err := queries.ListTransitions(ctx, id)
	if err != nil {
		return Engagement{}, fmt.Errorf("read an engagement's history: %w", err)
	}
	for _, transition := range transitions {
		engagement.Transitions = append(engagement.Transitions, Transition{
			From: State(transition.FromState), To: State(transition.ToState),
			Actor: transition.ActorLabel, Note: transition.Note, OccurredAt: transition.OccurredAt,
		})
	}
	return engagement, nil
}

/*
LinkProject links an engagement to the project its work is tracked in, or
unlinks it when projectID is nil.

Only a party may, and whether they may use that project is the caller's to
have checked: this package knows engagements, not projects.
*/
func (s *Service) LinkProject(ctx context.Context, id, actor uuid.UUID, projectID *uuid.UUID) (Engagement, error) {
	changed, err := s.queries.SetEngagementProject(ctx, dbgen.SetEngagementProjectParams{
		ID: id, Actor: actor, ProjectID: projectID,
	})
	if err != nil {
		return Engagement{}, fmt.Errorf("link an engagement to a project: %w", err)
	}
	if changed == 0 {
		return Engagement{}, ErrNoEngagement
	}
	return engagementWith(ctx, s.queries, id, actor)
}

// LinkedSummary is an engagement as a project shows it to one of its parties:
// what it is called and where it stands, and nothing of its terms.
type LinkedSummary struct {
	ID    uuid.UUID
	Title string
	State State
}

// LinkedTo lists the engagements linked to a project that the viewer is a
// party to.
func (s *Service) LinkedTo(ctx context.Context, projectID, viewer uuid.UUID) ([]LinkedSummary, error) {
	rows, err := s.queries.ListProjectEngagementsFor(ctx, dbgen.ListProjectEngagementsForParams{
		ProjectID: &projectID, Viewer: viewer,
	})
	if err != nil {
		return nil, fmt.Errorf("list a project's engagements: %w", err)
	}
	linked := make([]LinkedSummary, 0, len(rows))
	for _, row := range rows {
		linked = append(linked, LinkedSummary{ID: row.ID, Title: row.Title, State: State(row.State)})
	}
	return linked, nil
}

// Engagements lists somebody's engagements, most recently changed first.
func (s *Service) Engagements(ctx context.Context, viewer uuid.UUID) ([]Engagement, error) {
	rows, err := s.queries.ListEngagementsFor(ctx, viewer)
	if err != nil {
		return nil, fmt.Errorf("list engagements: %w", err)
	}
	engagements := make([]Engagement, 0, len(rows))
	for _, row := range rows {
		engagements = append(engagements, engagementOf(row))
	}
	return engagements, nil
}

/*
move changes an engagement's state and records who did it, in one transaction.

Refused when the move is not one the state allows, or when somebody else moved
the engagement first: the update names the state the caller saw, so two people
acting at once cannot both succeed.
*/
func move(
	ctx context.Context,
	queries *dbgen.Queries,
	engagement Engagement,
	to State,
	actor uuid.UUID,
	actorLabel string,
	note string,
) error {
	permitted := false
	for _, next := range allowed[engagement.State] {
		if next == to {
			permitted = true
		}
	}
	if !permitted {
		return ErrNotNow
	}

	moved, err := queries.MoveEngagement(ctx, dbgen.MoveEngagementParams{
		ID: engagement.ID, FromState: string(engagement.State), ToState: string(to),
	})
	if err != nil {
		return fmt.Errorf("move an engagement: %w", err)
	}
	if moved == 0 {
		return ErrChanged
	}
	if err := queries.RecordTransition(ctx, dbgen.RecordTransitionParams{
		EngagementID: engagement.ID, FromState: string(engagement.State), ToState: string(to),
		ActorID: &actor, ActorLabel: actorLabel, Note: note,
	}); err != nil {
		return fmt.Errorf("record an engagement's step: %w", err)
	}
	return nil
}

/*
AcceptEnquiry takes an enquiry up: it becomes an engagement in discussion, and
the sender joins the conversation the enquiry opened, so the two of them can
talk.

Only the recipient may, and only once.
*/
func (s *Service) AcceptEnquiry(ctx context.Context, enquiryID, recipient uuid.UUID) (Engagement, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Engagement{}, fmt.Errorf("begin taking up an enquiry: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)

	enquiry, err := queries.GetEnquiry(ctx, enquiryID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && enquiry.RecipientID != recipient) {
		return Engagement{}, ErrNoEnquiry
	}
	if err != nil {
		return Engagement{}, fmt.Errorf("read an enquiry: %w", err)
	}
	if enquiry.Status != string(EnquiryPending) {
		return Engagement{}, ErrAnswered
	}

	answered, err := queries.SetEnquiryStatus(ctx, dbgen.SetEnquiryStatusParams{ID: enquiryID, Status: string(EnquiryAccepted)})
	if err != nil {
		return Engagement{}, fmt.Errorf("answer an enquiry: %w", err)
	}
	if answered == 0 {
		return Engagement{}, ErrAnswered
	}

	client, err := queries.GetUserByID(ctx, enquiry.SenderID)
	if err != nil {
		return Engagement{}, fmt.Errorf("read an enquiry's sender: %w", err)
	}
	developer, err := queries.GetUserByID(ctx, enquiry.RecipientID)
	if err != nil {
		return Engagement{}, fmt.Errorf("read an enquiry's recipient: %w", err)
	}

	row, err := queries.InsertEngagement(ctx, dbgen.InsertEngagementParams{
		ID: uuid.New(), EnquiryID: &enquiry.ID, ClientID: &client.ID, DeveloperID: &developer.ID,
		ClientLabel: client.Username, DeveloperLabel: developer.Username,
		Title: enquiry.Subject, RoomID: enquiry.RoomID,
	})
	if err != nil {
		return Engagement{}, fmt.Errorf("start an engagement: %w", err)
	}
	if err := queries.RecordTransition(ctx, dbgen.RecordTransitionParams{
		EngagementID: row.ID, FromState: string(StateEnquiry), ToState: string(StateDiscussion),
		ActorID: &developer.ID, ActorLabel: developer.Username,
	}); err != nil {
		return Engagement{}, fmt.Errorf("record taking up an enquiry: %w", err)
	}

	if enquiry.RoomID != nil {
		if err := queries.AddRoomMember(ctx, dbgen.AddRoomMemberParams{
			RoomID: *enquiry.RoomID, UserID: client.ID,
		}); err != nil {
			return Engagement{}, fmt.Errorf("open the conversation to the client: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Engagement{}, fmt.Errorf("commit taking up an enquiry: %w", err)
	}
	return s.Engagement(ctx, row.ID, recipient)
}

/*
CloseEnquiry answers an enquiry without taking it up: declined by its
recipient, or withdrawn by its sender. Its conversation is closed to new
messages and kept.
*/
func (s *Service) CloseEnquiry(ctx context.Context, enquiryID, actor uuid.UUID, status EnquiryStatus) (Enquiry, error) {
	enquiry, err := s.queries.GetEnquiry(ctx, enquiryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Enquiry{}, ErrNoEnquiry
	}
	if err != nil {
		return Enquiry{}, fmt.Errorf("read an enquiry: %w", err)
	}
	switch {
	case status == EnquiryDeclined && enquiry.RecipientID != actor,
		status == EnquiryWithdrawn && enquiry.SenderID != actor:
		return Enquiry{}, ErrNoEnquiry
	case enquiry.Status != string(EnquiryPending):
		return Enquiry{}, ErrAnswered
	}

	answered, err := s.queries.SetEnquiryStatus(ctx, dbgen.SetEnquiryStatusParams{ID: enquiryID, Status: string(status)})
	if err != nil {
		return Enquiry{}, fmt.Errorf("answer an enquiry: %w", err)
	}
	if answered == 0 {
		return Enquiry{}, ErrAnswered
	}
	if enquiry.RoomID != nil {
		if err := s.queries.ArchiveEngagementRoom(ctx, dbgen.ArchiveEngagementRoomParams{
			ID: *enquiry.RoomID, Actor: &actor,
		}); err != nil {
			return Enquiry{}, fmt.Errorf("close an enquiry's conversation: %w", err)
		}
	}
	enquiry.Status = string(status)
	return enquiryOf(enquiry, "", ""), nil
}

/*
Withdraw ends an engagement before an agreement, by either party.

Its conversation is closed to new messages and kept, and no agreement exists or
can come to: withdrawn is a state nothing moves out of.
*/
func (s *Service) Withdraw(ctx context.Context, id, actor uuid.UUID, actorLabel, reason string) (Engagement, error) {
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(reason) > maxTransitionNote {
		return Engagement{}, invalid("reason", fmt.Sprintf("at most %d characters", maxTransitionNote))
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Engagement{}, fmt.Errorf("begin withdrawing: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)

	engagement, err := engagementWith(ctx, queries, id, actor)
	if err != nil {
		return Engagement{}, err
	}
	if !engagement.State.Withdrawable() {
		return Engagement{}, ErrNotNow
	}
	if err := move(ctx, queries, engagement, StateWithdrawn, actor, actorLabel, reason); err != nil {
		return Engagement{}, err
	}
	if engagement.RoomID != nil {
		if err := queries.ArchiveEngagementRoom(ctx, dbgen.ArchiveEngagementRoomParams{
			ID: *engagement.RoomID, Actor: &actor,
		}); err != nil {
			return Engagement{}, fmt.Errorf("close an engagement's conversation: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Engagement{}, fmt.Errorf("commit withdrawing: %w", err)
	}
	return s.Engagement(ctx, id, actor)
}
