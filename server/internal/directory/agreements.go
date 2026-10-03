package directory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
Proposals, negotiation, the agreement, and finishing.

A proposal states the scope, the deliverables each with a price, a timeline and
the currency. Sending one is final: a revision is a new version, and every
earlier version stays readable by both people. Either person may answer the
latest version — accept it, decline it, or ask for changes to one named part of
it — and when both have accepted the same version, the agreement is recorded:
a copy of those terms, both names and both acceptance times.

Then the work, and its end: either person says it is complete, and it is only
recorded as completed when the other agrees. Until they do it stays visibly
outstanding, for as long as that takes. Nothing expires it, and nothing here
judges whether the work was done.
*/

// Disclosure is what the server says about every agreement, wherever one is
// shown or exported: the same words, from this one constant, every time.
const Disclosure = "This is a record of what the two parties agreed, kept by this server so both " +
	"can refer to it. The server and its operator are not a party to this agreement. They do not hold " +
	"or move any money, provide escrow or payment, guarantee that either party will perform, or " +
	"resolve disputes about it. No payment, escrow or dispute-resolution service is connected to " +
	"this server."

// AgreementExportFormat names the shape of an exported agreement.
const AgreementExportFormat = "tdc.directory.agreement/1"

var (
	// ErrNotLatest reports a response to a version that has been superseded.
	ErrNotLatest = errors.New("that is not the latest version of the proposal")
	// ErrNoProposal reports a response before anything was proposed.
	ErrNoProposal = errors.New("nothing has been proposed yet")
	// ErrAlreadyAccepted reports a second acceptance of one version by one person.
	ErrAlreadyAccepted = errors.New("you have already accepted this version")
	// ErrOwnCompletion reports confirming the completion one asked for oneself.
	ErrOwnCompletion = errors.New("the other party confirms completion, not the one who asked for it")
	// ErrNoAgreement reports an engagement with no agreement to show.
	ErrNoAgreement = errors.New("there is no agreement on this engagement")
)

const (
	maxScope        = 4000
	maxTimeline     = 500
	maxLineItems    = 30
	maxItemDetail   = 300
	maxComment      = 1000
	maxAmountMinor  = 1_000_000_000_000_00
	defaultCurrency = "TZS"
)

var currencyCode = regexp.MustCompile(`^[A-Z]{3}$`)

// LineItem is one deliverable and its price.
type LineItem struct {
	Description string `json:"description"`
	AmountMinor int64  `json:"amountMinor"`
}

// ProposalInput is a proposal as somebody writes it.
type ProposalInput struct {
	Scope     string
	Timeline  string
	LineItems []LineItem
	Currency  string
}

// Normalise trims a proposal and refuses one that cannot be sent.
func (input *ProposalInput) Normalise() error {
	input.Scope = strings.TrimSpace(input.Scope)
	input.Timeline = strings.TrimSpace(input.Timeline)
	input.Currency = strings.ToUpper(strings.TrimSpace(input.Currency))
	if input.Currency == "" {
		input.Currency = defaultCurrency
	}
	switch {
	case input.Scope == "":
		return invalid("scope", "describe the work")
	case utf8.RuneCountInString(input.Scope) > maxScope:
		return invalid("scope", fmt.Sprintf("at most %d characters", maxScope))
	case input.Timeline == "":
		return invalid("timeline", "say when")
	case utf8.RuneCountInString(input.Timeline) > maxTimeline:
		return invalid("timeline", fmt.Sprintf("at most %d characters", maxTimeline))
	case !currencyCode.MatchString(input.Currency):
		return invalid("currency", "a three-letter currency code, such as TZS")
	case len(input.LineItems) == 0:
		return invalid("lineItems", "list at least one deliverable")
	case len(input.LineItems) > maxLineItems:
		return invalid("lineItems", fmt.Sprintf("at most %d deliverables", maxLineItems))
	}
	for index := range input.LineItems {
		item := &input.LineItems[index]
		item.Description = strings.TrimSpace(item.Description)
		field := fmt.Sprintf("lineItems[%d]", index)
		switch {
		case item.Description == "":
			return invalid(field+".description", "say what it is")
		case utf8.RuneCountInString(item.Description) > maxItemDetail:
			return invalid(field+".description", fmt.Sprintf("at most %d characters", maxItemDetail))
		case item.AmountMinor < 0 || item.AmountMinor > maxAmountMinor:
			return invalid(field+".amountMinor", "a whole number of minor units, not negative")
		}
	}
	return nil
}

// Total is the sum of a proposal's prices.
func (input ProposalInput) Total() int64 {
	var total int64
	for _, item := range input.LineItems {
		total += item.AmountMinor
	}
	return total
}

// ProposalVersion is one sent version.
type ProposalVersion struct {
	ID         uuid.UUID
	Version    int32
	SentBy     string
	Scope      string
	Timeline   string
	LineItems  []LineItem
	Currency   string
	TotalMinor int64
	SentAt     time.Time
}

// ResponseKind is how somebody answered a version.
type ResponseKind string

const (
	Accept         ResponseKind = "accept"
	Decline        ResponseKind = "decline"
	RequestChanges ResponseKind = "request_changes"
)

// Response is one answer to one version.
type Response struct {
	VersionID uuid.UUID
	Version   int32
	Responder string
	Kind      ResponseKind
	Part      string
	Comment   string
	At        time.Time
}

// Agreement is the record of both people accepting one version.
type Agreement struct {
	ID                  uuid.UUID
	EngagementID        uuid.UUID
	Version             int32
	ClientLabel         string
	DeveloperLabel      string
	ClientAcceptedAt    time.Time
	DeveloperAcceptedAt time.Time
	Terms               ProposalVersion
	RecordedAt          time.Time
}

// Negotiation is everything proposed and answered on one engagement.
type Negotiation struct {
	Versions  []ProposalVersion
	Responses []Response
	Agreement *Agreement
}

func versionOf(row dbgen.ProposalVersion) ProposalVersion {
	items := []LineItem{}
	_ = json.Unmarshal(row.LineItems, &items)
	return ProposalVersion{
		ID: row.ID, Version: row.Version, SentBy: row.SentByLabel, Scope: row.Scope,
		Timeline: row.Timeline, LineItems: items, Currency: row.Currency,
		TotalMinor: row.TotalMinor, SentAt: row.SentAt,
	}
}

func agreementOf(row dbgen.EngagementAgreement) Agreement {
	var terms ProposalVersion
	_ = json.Unmarshal(row.Terms, &terms)
	return Agreement{
		ID: row.ID, EngagementID: row.EngagementID, Version: row.Version,
		ClientLabel: row.ClientLabel, DeveloperLabel: row.DeveloperLabel,
		ClientAcceptedAt: row.ClientAcceptedAt, DeveloperAcceptedAt: row.DeveloperAcceptedAt,
		Terms: terms, RecordedAt: row.RecordedAt,
	}
}

// Negotiation reads what has been proposed and answered, for a party.
func (s *Service) Negotiation(ctx context.Context, engagementID, viewer uuid.UUID) (Negotiation, error) {
	if _, err := s.Engagement(ctx, engagementID, viewer); err != nil {
		return Negotiation{}, err
	}
	return negotiationWith(ctx, s.queries, engagementID)
}

func negotiationWith(ctx context.Context, queries *dbgen.Queries, engagementID uuid.UUID) (Negotiation, error) {
	negotiation := Negotiation{Versions: []ProposalVersion{}, Responses: []Response{}}

	versions, err := queries.ListProposalVersions(ctx, engagementID)
	if err != nil {
		return Negotiation{}, fmt.Errorf("read proposals: %w", err)
	}
	numbers := map[uuid.UUID]int32{}
	for _, row := range versions {
		negotiation.Versions = append(negotiation.Versions, versionOf(row))
		numbers[row.ID] = row.Version
	}

	responses, err := queries.ListProposalResponses(ctx, engagementID)
	if err != nil {
		return Negotiation{}, fmt.Errorf("read responses: %w", err)
	}
	for _, row := range responses {
		part := ""
		if row.Part != nil {
			part = *row.Part
		}
		negotiation.Responses = append(negotiation.Responses, Response{
			VersionID: row.VersionID, Version: numbers[row.VersionID], Responder: row.ResponderLabel,
			Kind: ResponseKind(row.Kind), Part: part, Comment: row.Comment, At: row.CreatedAt,
		})
	}

	agreement, err := queries.GetAgreement(ctx, engagementID)
	if err == nil {
		recorded := agreementOf(agreement)
		negotiation.Agreement = &recorded
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Negotiation{}, fmt.Errorf("read an agreement: %w", err)
	}
	return negotiation, nil
}

// Other returns the party who is not the actor.
func (e Engagement) Other(actor uuid.UUID) *uuid.UUID {
	if e.ClientID != nil && *e.ClientID == actor {
		return e.DeveloperID
	}
	return e.ClientID
}

/*
Propose sends a new version of the proposal.

Allowed while the engagement is being discussed or negotiated, and while a
proposal stands unanswered — a revision supersedes it. Sending moves the
engagement to proposal; a version is never changed once sent.
*/
func (s *Service) Propose(ctx context.Context, engagementID, actor uuid.UUID, actorLabel string, input ProposalInput) (ProposalVersion, Engagement, error) {
	if err := input.Normalise(); err != nil {
		return ProposalVersion{}, Engagement{}, err
	}
	items, err := json.Marshal(input.LineItems)
	if err != nil {
		return ProposalVersion{}, Engagement{}, fmt.Errorf("encode a proposal: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ProposalVersion{}, Engagement{}, fmt.Errorf("begin proposing: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)

	engagement, err := engagementWith(ctx, queries, engagementID, actor)
	if err != nil {
		return ProposalVersion{}, Engagement{}, err
	}
	switch engagement.State {
	case StateDiscussion, StateNegotiation:
		if err := move(ctx, queries, engagement, StateProposal, actor, actorLabel, ""); err != nil {
			return ProposalVersion{}, Engagement{}, err
		}
	case StateProposal:
		// A revision of a proposal nobody has answered: the state stands, and
		// the new version is the one now open.
	default:
		return ProposalVersion{}, Engagement{}, ErrNotNow
	}

	row, err := queries.InsertProposalVersion(ctx, dbgen.InsertProposalVersionParams{
		ID: uuid.New(), EngagementID: engagementID, SentBy: &actor, SentByLabel: actorLabel,
		Scope: input.Scope, Timeline: input.Timeline, LineItems: items,
		Currency: input.Currency, TotalMinor: input.Total(),
	})
	if isUniqueViolation(err) {
		return ProposalVersion{}, Engagement{}, ErrChanged
	}
	if err != nil {
		return ProposalVersion{}, Engagement{}, fmt.Errorf("send a proposal: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ProposalVersion{}, Engagement{}, fmt.Errorf("commit a proposal: %w", err)
	}
	updated, err := s.Engagement(ctx, engagementID, actor)
	return versionOf(row), updated, err
}

// partName is how a change request names what it is about.
var partName = regexp.MustCompile(`^(scope|timeline|currency|item:[0-9]{1,2})$`)

/*
Respond answers the latest version of the proposal.

Declining or asking for changes moves the engagement to negotiation, where
either person may propose again or withdraw. Accepting moves nothing by itself;
it is the second acceptance of the same version that records the agreement,
with both acceptance times, in the same transaction.

Returns whether an agreement was recorded.
*/
func (s *Service) Respond(
	ctx context.Context,
	engagementID, actor uuid.UUID,
	actorLabel string,
	versionID uuid.UUID,
	kind ResponseKind,
	part, comment string,
) (bool, Engagement, error) {
	part = strings.TrimSpace(part)
	comment = strings.TrimSpace(comment)
	switch {
	case kind != Accept && kind != Decline && kind != RequestChanges:
		return false, Engagement{}, invalid("kind", `"accept", "decline" or "request_changes"`)
	case utf8.RuneCountInString(comment) > maxComment:
		return false, Engagement{}, invalid("comment", fmt.Sprintf("at most %d characters", maxComment))
	case kind == RequestChanges && !partName.MatchString(part):
		return false, Engagement{}, invalid("part", `name what the change is about: "scope", "timeline", "currency" or "item:N"`)
	case kind == RequestChanges && comment == "":
		return false, Engagement{}, invalid("comment", "say what should change")
	}
	if kind != RequestChanges {
		part = ""
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, Engagement{}, fmt.Errorf("begin responding: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)

	engagement, err := engagementWith(ctx, queries, engagementID, actor)
	if err != nil {
		return false, Engagement{}, err
	}
	latest, err := queries.LatestProposalVersion(ctx, engagementID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, Engagement{}, ErrNoProposal
	}
	if err != nil {
		return false, Engagement{}, fmt.Errorf("read the latest proposal: %w", err)
	}
	if latest.ID != versionID {
		return false, Engagement{}, ErrNotLatest
	}
	// Only an open proposal is answered: one already declined, or asked to
	// change, waits for a new version.
	if engagement.State != StateProposal {
		return false, Engagement{}, ErrNotNow
	}
	if kind == RequestChanges && strings.HasPrefix(part, "item:") {
		index, _ := strconv.Atoi(strings.TrimPrefix(part, "item:"))
		if index < 1 || index > len(versionOf(latest).LineItems) {
			return false, Engagement{}, invalid("part", "that line is not in the proposal")
		}
	}

	var partValue *string
	if part != "" {
		partValue = &part
	}
	if _, err := queries.InsertProposalResponse(ctx, dbgen.InsertProposalResponseParams{
		ID: uuid.New(), VersionID: latest.ID, EngagementID: engagementID,
		ResponderID: &actor, ResponderLabel: actorLabel, Kind: string(kind),
		Part: partValue, Comment: comment,
	}); isUniqueViolation(err) {
		return false, Engagement{}, ErrAlreadyAccepted
	} else if err != nil {
		return false, Engagement{}, fmt.Errorf("record a response: %w", err)
	}

	agreed := false
	switch kind {
	case Decline, RequestChanges:
		if err := move(ctx, queries, engagement, StateNegotiation, actor, actorLabel, comment); err != nil {
			return false, Engagement{}, err
		}
	case Accept:
		agreed, err = recordAgreementIfBoth(ctx, queries, engagement, latest, actor, actorLabel)
		if err != nil {
			return false, Engagement{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return false, Engagement{}, fmt.Errorf("commit a response: %w", err)
	}
	updated, err := s.Engagement(ctx, engagementID, actor)
	return agreed, updated, err
}

/*
recordAgreementIfBoth records the agreement once both parties have accepted the
same version, and not before. One acceptance moves nothing.
*/
func recordAgreementIfBoth(
	ctx context.Context,
	queries *dbgen.Queries,
	engagement Engagement,
	version dbgen.ProposalVersion,
	actor uuid.UUID,
	actorLabel string,
) (bool, error) {
	acceptances, err := queries.AcceptancesOf(ctx, version.ID)
	if err != nil {
		return false, fmt.Errorf("read acceptances: %w", err)
	}
	var clientAt, developerAt *time.Time
	for _, acceptance := range acceptances {
		at := acceptance.CreatedAt
		switch {
		case acceptance.ResponderID == nil:
		case engagement.ClientID != nil && *acceptance.ResponderID == *engagement.ClientID:
			clientAt = &at
		case engagement.DeveloperID != nil && *acceptance.ResponderID == *engagement.DeveloperID:
			developerAt = &at
		}
	}
	if clientAt == nil || developerAt == nil {
		return false, nil
	}

	terms, err := json.Marshal(versionOf(version))
	if err != nil {
		return false, fmt.Errorf("encode agreed terms: %w", err)
	}
	if _, err := queries.InsertAgreement(ctx, dbgen.InsertAgreementParams{
		ID: uuid.New(), EngagementID: engagement.ID, VersionID: version.ID, Version: version.Version,
		ClientID: engagement.ClientID, DeveloperID: engagement.DeveloperID,
		ClientLabel: engagement.ClientLabel, DeveloperLabel: engagement.DeveloperLabel,
		ClientAcceptedAt: *clientAt, DeveloperAcceptedAt: *developerAt, Terms: terms,
	}); err != nil {
		return false, fmt.Errorf("record an agreement: %w", err)
	}
	if err := move(ctx, queries, engagement, StateAgreement, actor, actorLabel, ""); err != nil {
		return false, err
	}
	return true, nil
}

// StartWork moves an agreed engagement to work under way, by either party.
func (s *Service) StartWork(ctx context.Context, engagementID, actor uuid.UUID, actorLabel string) (Engagement, error) {
	return s.step(ctx, engagementID, actor, actorLabel, StateActive, "")
}

/*
RequestCompletion is one party saying the work is complete. It stays only
requested — visibly, to both, and for as long as it takes — until the other
confirms.
*/
func (s *Service) RequestCompletion(ctx context.Context, engagementID, actor uuid.UUID, actorLabel, note string) (Engagement, error) {
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > maxTransitionNote {
		return Engagement{}, invalid("note", fmt.Sprintf("at most %d characters", maxTransitionNote))
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Engagement{}, fmt.Errorf("begin asking for completion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)

	engagement, err := engagementWith(ctx, queries, engagementID, actor)
	if err != nil {
		return Engagement{}, err
	}
	if engagement.State != StateActive {
		return Engagement{}, ErrNotNow
	}
	if _, err := queries.RequestCompletion(ctx, dbgen.RequestCompletionParams{
		ID: engagementID, RequestedBy: &actor,
	}); err != nil {
		return Engagement{}, fmt.Errorf("ask for completion: %w", err)
	}
	if err := move(ctx, queries, engagement, StateCompletionPending, actor, actorLabel, note); err != nil {
		return Engagement{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Engagement{}, fmt.Errorf("commit asking for completion: %w", err)
	}
	return s.Engagement(ctx, engagementID, actor)
}

// ConfirmCompletion is the other party agreeing the work is complete.
func (s *Service) ConfirmCompletion(ctx context.Context, engagementID, actor uuid.UUID, actorLabel string) (Engagement, error) {
	engagement, err := s.Engagement(ctx, engagementID, actor)
	if err != nil {
		return Engagement{}, err
	}
	if engagement.State != StateCompletionPending {
		return Engagement{}, ErrNotNow
	}
	if engagement.CompletionRequestedBy != nil && *engagement.CompletionRequestedBy == actor {
		return Engagement{}, ErrOwnCompletion
	}
	return s.step(ctx, engagementID, actor, actorLabel, StateCompleted, "")
}

// step makes one move in its own transaction.
func (s *Service) step(ctx context.Context, engagementID, actor uuid.UUID, actorLabel string, to State, note string) (Engagement, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Engagement{}, fmt.Errorf("begin a step: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)

	engagement, err := engagementWith(ctx, queries, engagementID, actor)
	if err != nil {
		return Engagement{}, err
	}
	if err := move(ctx, queries, engagement, to, actor, actorLabel, note); err != nil {
		return Engagement{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Engagement{}, fmt.Errorf("commit a step: %w", err)
	}
	return s.Engagement(ctx, engagementID, actor)
}

// AgreementExport is an agreement in the documented format.
type AgreementExport struct {
	Format       string             `json:"format"`
	ExportedAt   string             `json:"exportedAt"`
	Server       string             `json:"server"`
	Disclosure   string             `json:"disclosure"`
	EngagementID string             `json:"engagementId"`
	Title        string             `json:"title"`
	Version      int32              `json:"version"`
	Client       ExportParty        `json:"client"`
	Developer    ExportParty        `json:"developer"`
	Terms        ExportTerms        `json:"terms"`
	RecordedAt   string             `json:"recordedAt"`
	History      []ExportTransition `json:"history"`
}

// ExportParty is one party and when they accepted.
type ExportParty struct {
	Username   string `json:"username"`
	AcceptedAt string `json:"acceptedAt"`
}

// ExportTerms are the agreed terms.
type ExportTerms struct {
	Scope      string     `json:"scope"`
	Timeline   string     `json:"timeline"`
	LineItems  []LineItem `json:"lineItems"`
	Currency   string     `json:"currency"`
	TotalMinor int64      `json:"totalMinor"`
	SentBy     string     `json:"sentBy"`
	SentAt     string     `json:"sentAt"`
}

// ExportTransition is one step of the engagement's history.
type ExportTransition struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Actor string `json:"actor"`
	Note  string `json:"note,omitempty"`
	At    string `json:"at"`
}

// ExportAgreement returns an engagement's agreement for one of its parties.
func (s *Service) ExportAgreement(ctx context.Context, engagementID, viewer uuid.UUID, server string) (AgreementExport, error) {
	engagement, err := s.Engagement(ctx, engagementID, viewer)
	if err != nil {
		return AgreementExport{}, err
	}
	row, err := s.queries.GetAgreement(ctx, engagementID)
	if errors.Is(err, pgx.ErrNoRows) {
		return AgreementExport{}, ErrNoAgreement
	}
	if err != nil {
		return AgreementExport{}, fmt.Errorf("read an agreement: %w", err)
	}
	agreement := agreementOf(row)
	stamp := func(at time.Time) string { return at.UTC().Format(time.RFC3339) }

	export := AgreementExport{
		Format: AgreementExportFormat, ExportedAt: stamp(time.Now()), Server: server,
		Disclosure: Disclosure, EngagementID: engagement.ID.String(), Title: engagement.Title,
		Version:   agreement.Version,
		Client:    ExportParty{Username: agreement.ClientLabel, AcceptedAt: stamp(agreement.ClientAcceptedAt)},
		Developer: ExportParty{Username: agreement.DeveloperLabel, AcceptedAt: stamp(agreement.DeveloperAcceptedAt)},
		Terms: ExportTerms{
			Scope: agreement.Terms.Scope, Timeline: agreement.Terms.Timeline,
			LineItems: agreement.Terms.LineItems, Currency: agreement.Terms.Currency,
			TotalMinor: agreement.Terms.TotalMinor, SentBy: agreement.Terms.SentBy,
			SentAt: stamp(agreement.Terms.SentAt),
		},
		RecordedAt: stamp(agreement.RecordedAt),
		History:    []ExportTransition{},
	}
	for _, transition := range engagement.Transitions {
		export.History = append(export.History, ExportTransition{
			From: string(transition.From), To: string(transition.To), Actor: transition.Actor,
			Note: transition.Note, At: stamp(transition.OccurredAt),
		})
	}
	return export, nil
}
