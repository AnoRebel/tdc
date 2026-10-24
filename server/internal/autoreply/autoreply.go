/*
Package autoreply holds the automatic replies people set for while they are
away, and decides which one, if any, answers a message.

What it decides is deliberately narrow: whether a rule applies now, given the
person's own status. Who may be answered, where and how often is the caller's,
because that needs rooms, blocks and the message itself.
*/
package autoreply

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

// MaxLength bounds a reply's text. Short on purpose: an automatic reply says
// when somebody is back and who to ask meanwhile, not an essay.
const MaxLength = 500

// MaxRules bounds how many replies one person may keep.
const MaxRules = 10

// QuietPeriod is how long after answering a sender in a conversation the
// same person's automatic reply stays silent to them there.
const QuietPeriod = 24 * time.Hour

// HourlyCeiling is how many conversations one person's automatic reply may
// answer in an hour, whatever happens. A ceiling rather than a rate: past it,
// people are not answered until the hour has moved on.
const HourlyCeiling = 20

// AppearOffline is the status key that also makes somebody appear offline.
const AppearOffline = "offline"

var (
	// ErrInvalid reports a rule that cannot be kept as given.
	ErrInvalid = errors.New("invalid automatic reply")
	// ErrNotFound reports a rule that is not the caller's or does not exist.
	ErrNotFound = errors.New("no such automatic reply")
	// ErrTooMany reports a person already at MaxRules.
	ErrTooMany = errors.New("too many automatic replies")
)

// Rule is one automatic reply as the person sets it.
type Rule struct {
	Enabled         bool
	Message         string
	MentionMessage  string
	Statuses        []string
	StartsAt        *time.Time
	EndsAt          *time.Time
	TimeZone        string
	ReplyToDirect   bool
	ReplyToGroups   bool
	ReplyToMentions bool
}

// Validate trims a rule and refuses one that cannot be kept.
func (r *Rule) Validate() error {
	r.Message = strings.TrimSpace(r.Message)
	r.MentionMessage = strings.TrimSpace(r.MentionMessage)
	if r.Message == "" {
		return fmt.Errorf("%w: the reply needs some text", ErrInvalid)
	}
	if utf8.RuneCountInString(r.Message) > MaxLength || utf8.RuneCountInString(r.MentionMessage) > MaxLength {
		return fmt.Errorf("%w: a reply is at most %d characters", ErrInvalid, MaxLength)
	}
	if r.StartsAt != nil && r.EndsAt != nil && !r.StartsAt.Before(*r.EndsAt) {
		return fmt.Errorf("%w: the window ends before it starts", ErrInvalid)
	}
	if r.TimeZone == "" {
		r.TimeZone = "UTC"
	}
	if _, err := time.LoadLocation(r.TimeZone); err != nil {
		return fmt.Errorf("%w: %q is not a time zone", ErrInvalid, r.TimeZone)
	}
	if !r.ReplyToDirect && !r.ReplyToGroups && !r.ReplyToMentions {
		return fmt.Errorf("%w: choose at least one kind of message to answer", ErrInvalid)
	}
	seen := map[string]bool{}
	statuses := make([]string, 0, len(r.Statuses))
	for _, status := range r.Statuses {
		status = strings.TrimSpace(status)
		if status != "" && !seen[status] {
			seen[status] = true
			statuses = append(statuses, status)
		}
	}
	r.Statuses = statuses
	return nil
}

/*
Applies reports whether a stored rule answers now.

status is the person's own status, not what others are shown — and appearing
offline counts as the Appear offline status whatever else is set. That is the
non-disclosure rule: while somebody appears offline their status is withheld
from everybody, so a reply tied to "busy" must not fire and announce it. A rule
tied to no status at all says nothing about one, and still applies.
*/
func Applies(rule dbgen.AutomaticReply, status string, invisible bool, now time.Time) bool {
	if !rule.Enabled {
		return false
	}
	if rule.StartsAt != nil && now.Before(*rule.StartsAt) {
		return false
	}
	if rule.EndsAt != nil && !now.Before(*rule.EndsAt) {
		return false
	}
	if len(rule.Statuses) == 0 {
		return true
	}
	if invisible {
		status = AppearOffline
	}
	for _, wanted := range rule.Statuses {
		if wanted == status {
			return true
		}
	}
	return false
}

// Kind is what a message was, from the person being answered's side.
type Kind int

const (
	// Direct is a one-to-one conversation.
	Direct Kind = iota
	// Group is a direct conversation with more than two people.
	Group
	// Mention is being named in a channel.
	Mention
)

// Answers reports whether a rule answers this kind of message.
func Answers(rule dbgen.AutomaticReply, kind Kind) bool {
	switch kind {
	case Direct:
		return rule.ReplyToDirect
	case Group:
		return rule.ReplyToGroups
	default:
		return rule.ReplyToMentions
	}
}

// Text is what a rule says for a kind of message.
func Text(rule dbgen.AutomaticReply, kind Kind) string {
	if kind == Mention && rule.MentionMessage != "" {
		return rule.MentionMessage
	}
	return rule.Message
}

// Choose returns the first rule that answers, in the order they were made.
func Choose(rules []dbgen.AutomaticReply, kind Kind, status string, invisible bool, now time.Time) (dbgen.AutomaticReply, bool) {
	for _, rule := range rules {
		if Answers(rule, kind) && Applies(rule, status, invisible, now) {
			return rule, true
		}
	}
	return dbgen.AutomaticReply{}, false
}

// Service keeps people's automatic replies.
type Service struct {
	store dbgen.Querier
}

// NewService builds the service.
func NewService(store dbgen.Querier) *Service {
	return &Service{store: store}
}

// List returns somebody's replies, oldest first.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]dbgen.AutomaticReply, error) {
	rules, err := s.store.ListAutomaticReplies(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list automatic replies: %w", err)
	}
	return rules, nil
}

// Create adds a reply.
func (s *Service) Create(ctx context.Context, userID uuid.UUID, rule Rule) (dbgen.AutomaticReply, error) {
	if err := rule.Validate(); err != nil {
		return dbgen.AutomaticReply{}, err
	}
	count, err := s.store.CountAutomaticReplies(ctx, userID)
	if err != nil {
		return dbgen.AutomaticReply{}, fmt.Errorf("count automatic replies: %w", err)
	}
	if count >= MaxRules {
		return dbgen.AutomaticReply{}, fmt.Errorf("%w: at most %d", ErrTooMany, MaxRules)
	}
	created, err := s.store.CreateAutomaticReply(ctx, dbgen.CreateAutomaticReplyParams{
		ID: uuid.New(), UserID: userID, Enabled: rule.Enabled, Message: rule.Message,
		MentionMessage: rule.MentionMessage, Statuses: rule.Statuses,
		StartsAt: rule.StartsAt, EndsAt: rule.EndsAt, TimeZone: rule.TimeZone,
		ReplyToDirect: rule.ReplyToDirect, ReplyToGroups: rule.ReplyToGroups,
		ReplyToMentions: rule.ReplyToMentions,
	})
	if err != nil {
		return dbgen.AutomaticReply{}, fmt.Errorf("create automatic reply: %w", err)
	}
	return created, nil
}

// Update replaces a reply of the caller's.
func (s *Service) Update(ctx context.Context, userID, id uuid.UUID, rule Rule) (dbgen.AutomaticReply, error) {
	if err := rule.Validate(); err != nil {
		return dbgen.AutomaticReply{}, err
	}
	updated, err := s.store.UpdateAutomaticReply(ctx, dbgen.UpdateAutomaticReplyParams{
		ID: id, UserID: userID, Enabled: rule.Enabled, Message: rule.Message,
		MentionMessage: rule.MentionMessage, Statuses: rule.Statuses,
		StartsAt: rule.StartsAt, EndsAt: rule.EndsAt, TimeZone: rule.TimeZone,
		ReplyToDirect: rule.ReplyToDirect, ReplyToGroups: rule.ReplyToGroups,
		ReplyToMentions: rule.ReplyToMentions,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.AutomaticReply{}, ErrNotFound
	}
	if err != nil {
		return dbgen.AutomaticReply{}, fmt.Errorf("update automatic reply: %w", err)
	}
	return updated, nil
}

// Delete removes a reply of the caller's.
func (s *Service) Delete(ctx context.Context, userID, id uuid.UUID) error {
	removed, err := s.store.DeleteAutomaticReply(ctx, dbgen.DeleteAutomaticReplyParams{ID: id, UserID: userID})
	if err != nil {
		return fmt.Errorf("delete automatic reply: %w", err)
	}
	if removed == 0 {
		return ErrNotFound
	}
	return nil
}

/*
Claim takes the right to answer one sender in one conversation.

False when the person was answered there within the quiet period, or when this
person's replies have reached the hourly ceiling. Checked and recorded in the
database rather than in memory, so every node of a server agrees.
*/
func (s *Service) Claim(ctx context.Context, userID, roomID, senderID uuid.UUID) bool {
	recent, err := s.store.CountRecentAutomaticReplies(ctx, dbgen.CountRecentAutomaticRepliesParams{
		UserID: userID, WindowSeconds: time.Hour.Seconds(),
	})
	if err != nil || recent >= HourlyCeiling {
		// Unsure means silent: a missed automatic reply costs nothing, a
		// flood of them is the failure this exists to prevent.
		return false
	}
	_, err = s.store.ClaimAutomaticReply(ctx, dbgen.ClaimAutomaticReplyParams{
		UserID: userID, RoomID: roomID, SenderID: senderID, QuietSeconds: QuietPeriod.Seconds(),
	})
	return err == nil
}

// Release gives back a claim whose reply was never posted.
func (s *Service) Release(ctx context.Context, userID, roomID, senderID uuid.UUID) {
	_ = s.store.ReleaseAutomaticReply(ctx, dbgen.ReleaseAutomaticReplyParams{
		UserID: userID, RoomID: roomID, SenderID: senderID,
	})
}
