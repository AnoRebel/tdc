package moderation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

var (
	// ErrBanned is returned when somebody is banned from a channel.
	ErrBanned = errors.New("you are banned from this channel")
	// ErrRestricted is returned when somebody may read but not post.
	ErrRestricted = errors.New("you cannot post here at the moment")
	// ErrReasonRequired is returned when a ban or restriction carries none.
	ErrReasonRequired = errors.New("a reason is required")
	// ErrUnknownPolicy is returned for a direct-message setting that is not
	// one of the three.
	ErrUnknownPolicy = errors.New("that is not a direct message setting")
)

/*
maxReason bounds why somebody was banned.

Long enough for a paragraph explaining a decision, short enough that the field
is not somewhere to paste a conversation. The person banned is entitled to the
reason, so this is written to be read by them.
*/
const maxReason = 1000

// Ban is somebody kept out of one channel.
type Ban struct {
	RoomID   uuid.UUID
	UserID   uuid.UUID
	Username string
	// DisplayName is empty for somebody who has not set one.
	DisplayName string
	BannedBy    string
	Reason      string
	ExpiresAt   *time.Time
	CreatedAt   time.Time
}

/*
BanFromChannel keeps somebody out of one conversation.

Confined to that channel, which is the difference between this and a
suspension: every other membership is untouched, and somebody banned from one
channel carries on elsewhere as before.

A reason is required. A ban nobody can explain is a ban nobody can review, and
the person banned is entitled to know why.
*/
func (s *Service) BanFromChannel(
	ctx context.Context,
	roomID, userID, by uuid.UUID,
	byAddress, reason string,
	until *time.Time,
) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ErrReasonRequired
	}

	err := s.queries.BanFromChannel(ctx, dbgen.BanFromChannelParams{
		RoomID:          roomID,
		UserID:          userID,
		BannedBy:        &by,
		BannedByAddress: byAddress,
		Reason:          truncate(reason, maxReason),
		ExpiresAt:       until,
	})
	if err != nil {
		return fmt.Errorf("moderation: banning: %w", err)
	}

	s.log.Info("somebody was banned from a channel",
		"roomId", roomID, "userId", userID, "by", byAddress, "until", until)

	return nil
}

// LiftBan readmits somebody before their ban would have expired.
func (s *Service) LiftBan(ctx context.Context, roomID, userID uuid.UUID) error {
	_, err := s.queries.LiftChannelBan(ctx, dbgen.LiftChannelBanParams{
		RoomID: roomID,
		UserID: userID,
	})
	if err != nil {
		return fmt.Errorf("moderation: lifting a ban: %w", err)
	}
	return nil
}

/*
IsBanned reports whether somebody is currently banned from a channel.

The expiry is evaluated in the query rather than by a background sweep, so a
temporary ban lifts at its own moment rather than whenever a job next runs —
"banned for an hour" has to mean an hour.

A failure reports not banned. Refusing everybody because a check failed would
turn a database hiccup into an outage of the channel.
*/
func (s *Service) IsBanned(ctx context.Context, roomID, userID uuid.UUID) bool {
	banned, err := s.queries.IsBannedFromChannel(ctx, dbgen.IsBannedFromChannelParams{
		RoomID: roomID,
		UserID: userID,
	})
	if err != nil {
		s.log.Warn("cannot check a channel ban", "roomId", roomID, "error", err)
		return false
	}
	return banned
}

// BansIn lists who is currently kept out of a channel.
func (s *Service) BansIn(ctx context.Context, roomID uuid.UUID) ([]Ban, error) {
	rows, err := s.queries.ListChannelBans(ctx, roomID)
	if err != nil {
		return nil, fmt.Errorf("moderation: listing bans: %w", err)
	}

	bans := make([]Ban, 0, len(rows))
	for _, row := range rows {
		bans = append(bans, Ban{
			RoomID:      row.RoomID,
			UserID:      row.UserID,
			Username:    row.Username,
			DisplayName: row.DisplayName,
			BannedBy:    row.BannedByAddress,
			Reason:      row.Reason,
			ExpiresAt:   row.ExpiresAt,
			CreatedAt:   row.CreatedAt,
		})
	}
	return bans, nil
}

/*
RestrictPosting silences somebody without removing them.

The proportionate response to somebody arguing rather than attacking: they keep
reading the conversation they are part of and stop adding to it. A nil room
restricts them across the server.
*/
func (s *Service) RestrictPosting(
	ctx context.Context,
	userID uuid.UUID,
	roomID *uuid.UUID,
	by uuid.UUID,
	byAddress, reason string,
	until *time.Time,
) (uuid.UUID, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return uuid.Nil, ErrReasonRequired
	}

	row, err := s.queries.RestrictPosting(ctx, dbgen.RestrictPostingParams{
		ID:                  uuid.New(),
		UserID:              userID,
		RoomID:              roomID,
		RestrictedBy:        &by,
		RestrictedByAddress: byAddress,
		Reason:              truncate(reason, maxReason),
		ExpiresAt:           until,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("moderation: restricting: %w", err)
	}

	return row.ID, nil
}

// ErrNoSuchRestriction is returned for lifting a restriction that is not in
// force in the channel named.
var ErrNoSuchRestriction = errors.New("moderation: no such restriction")

// Restriction is somebody silenced in a channel.
type Restriction struct {
	ID           uuid.UUID
	UserID       uuid.UUID
	Username     string
	RestrictedBy string
	Reason       string
	ExpiresAt    *time.Time
	CreatedAt    time.Time
}

// LiftRestriction ends a restriction early, within the scope it was made in: a
// channel, or the whole server when roomID is nil.
func (s *Service) LiftRestriction(ctx context.Context, id uuid.UUID, roomID *uuid.UUID, by uuid.UUID) error {
	lifted, err := s.queries.LiftPostingRestriction(ctx, dbgen.LiftPostingRestrictionParams{
		ID:       id,
		LiftedBy: &by,
		RoomID:   roomID,
	})
	if err != nil {
		return fmt.Errorf("moderation: lifting a restriction: %w", err)
	}
	if lifted == 0 {
		return ErrNoSuchRestriction
	}
	return nil
}

// RestrictionsIn lists who is currently silenced in a channel, or across the
// whole server when roomID is nil.
func (s *Service) RestrictionsIn(ctx context.Context, roomID *uuid.UUID) ([]Restriction, error) {
	var rows []restrictionRow
	if roomID == nil {
		found, err := s.queries.ListServerPostingRestrictions(ctx)
		if err != nil {
			return nil, fmt.Errorf("moderation: listing restrictions: %w", err)
		}
		for _, row := range found {
			rows = append(rows, restrictionRow(row))
		}
	} else {
		found, err := s.queries.ListPostingRestrictionsInRoom(ctx, roomID)
		if err != nil {
			return nil, fmt.Errorf("moderation: listing restrictions: %w", err)
		}
		for _, row := range found {
			rows = append(rows, restrictionRow(row))
		}
	}

	restrictions := make([]Restriction, 0, len(rows))
	for _, row := range rows {
		restrictions = append(restrictions, Restriction{
			ID:           row.ID,
			UserID:       row.UserID,
			Username:     row.Username,
			RestrictedBy: row.RestrictedByAddress,
			Reason:       row.Reason,
			ExpiresAt:    row.ExpiresAt,
			CreatedAt:    row.CreatedAt,
		})
	}
	return restrictions, nil
}

// restrictionRow is the shape both listing queries return, named once so the
// two can be read the same way.
type restrictionRow dbgen.ListPostingRestrictionsInRoomRow

/*
IsPostingRestricted reports whether somebody may post in a channel.

Checks a restriction on this channel and one across the server in a single
query: this runs on the path every message takes, and asking twice would be two
round trips per message.
*/
func (s *Service) IsPostingRestricted(ctx context.Context, userID, roomID uuid.UUID) bool {
	restricted, err := s.queries.IsPostingRestricted(ctx, dbgen.IsPostingRestrictedParams{
		UserID: userID,
		RoomID: &roomID,
	})
	if err != nil {
		s.log.Warn("cannot check a posting restriction", "userId", userID, "error", err)
		return false
	}
	return restricted
}

/*
Block ends contact from one person to another, without telling them.

Never disclosed. The person blocked is not told, their messages are not refused
in a way that reveals it, and nothing in the interface says so — somebody who
learns they have been blocked frequently escalates, and the point is to end
contact rather than to announce it.
*/
func (s *Service) Block(ctx context.Context, blocker, blocked uuid.UUID) error {
	if blocker == blocked {
		// Not an error worth reporting: nobody means to do this.
		return nil
	}

	err := s.queries.BlockUser(ctx, dbgen.BlockUserParams{
		BlockerID: blocker,
		BlockedID: blocked,
	})
	if err != nil {
		return fmt.Errorf("moderation: blocking: %w", err)
	}
	return nil
}

// Unblock restores contact.
func (s *Service) Unblock(ctx context.Context, blocker, blocked uuid.UUID) error {
	err := s.queries.UnblockUser(ctx, dbgen.UnblockUserParams{
		BlockerID: blocker,
		BlockedID: blocked,
	})
	if err != nil {
		return fmt.Errorf("moderation: unblocking: %w", err)
	}
	return nil
}

// HasBlocked reports whether one person has blocked another.
func (s *Service) HasBlocked(ctx context.Context, blocker, blocked uuid.UUID) bool {
	has, err := s.queries.HasBlocked(ctx, dbgen.HasBlockedParams{
		BlockerID: blocker,
		BlockedID: blocked,
	})
	if err != nil {
		return false
	}
	return has
}

// BlockedBy lists who somebody has blocked, which only they may see.
func (s *Service) BlockedBy(ctx context.Context, blocker uuid.UUID) ([]dbgen.ListBlockedRow, error) {
	rows, err := s.queries.ListBlocked(ctx, blocker)
	if err != nil {
		return nil, fmt.Errorf("moderation: listing blocks: %w", err)
	}
	return rows, nil
}

/*
DirectMessagePolicy is who may open a conversation with somebody.

Three settings, because two are not enough: "anyone" and "nobody" leave out the
case most people actually want, which is that colleagues can reach them and
strangers cannot.
*/
type DirectMessagePolicy string

const (
	// DirectAnyone lets anybody on the server start a conversation.
	DirectAnyone DirectMessagePolicy = "anyone"
	// DirectShared limits it to people already in a channel with them.
	DirectShared DirectMessagePolicy = "shared"
	// DirectNobody accepts none.
	DirectNobody DirectMessagePolicy = "nobody"
)

// SetDirectMessagePolicy records who may write to somebody.
func (s *Service) SetDirectMessagePolicy(
	ctx context.Context,
	userID uuid.UUID,
	policy DirectMessagePolicy,
) error {
	switch policy {
	case DirectAnyone, DirectShared, DirectNobody:
	default:
		return ErrUnknownPolicy
	}

	err := s.queries.SetDirectMessagePolicy(ctx, dbgen.SetDirectMessagePolicyParams{
		UserID:         userID,
		DirectMessages: string(policy),
	})
	if err != nil {
		return fmt.Errorf("moderation: setting a direct message policy: %w", err)
	}
	return nil
}

// DirectMessagePolicyFor reads somebody's setting, defaulting to open.
//
// Open is the default because a server where nobody can reach anybody until
// they have configured it is a server that appears broken.
func (s *Service) DirectMessagePolicyFor(ctx context.Context, userID uuid.UUID) DirectMessagePolicy {
	policy, err := s.queries.GetDirectMessagePolicy(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) || err != nil {
		return DirectAnyone
	}
	return DirectMessagePolicy(policy)
}

/*
MayDirectMessage reports whether one person may open a conversation with
another.

Deliberately returns only yes or no. The caller refuses without saying which
rule applied, because "they have blocked you" and "they accept messages only
from colleagues" are different disclosures and one of them must never be made.

A moderator is not exempt here — that is the caller's decision, since only the
caller knows what permissions the person holds.
*/
func (s *Service) MayDirectMessage(ctx context.Context, from, to uuid.UUID) bool {
	if from == to {
		// Somebody's conversation with themselves is their own business.
		return true
	}

	// A block is checked first and silently: the blocked person must not learn
	// that they were blocked rather than refused by a setting.
	if s.HasBlocked(ctx, to, from) {
		return false
	}

	switch s.DirectMessagePolicyFor(ctx, to) {
	case DirectNobody:
		return false

	case DirectShared:
		shared, err := s.queries.SharesAChannel(ctx, dbgen.SharesAChannelParams{
			UserID:   from,
			UserID_2: to,
		})
		if err != nil {
			// Refusing on a failed check is the safe direction: it withholds a
			// message rather than delivering one somebody did not want.
			s.log.Warn("cannot check a shared channel", "error", err)
			return false
		}
		return shared

	default:
		return true
	}
}

/*
CallPolicy is who may ring somebody or start a call with them: the same three
answers as direct messages, and separately whether people on other servers may.
*/
type CallPolicy struct {
	FromThisServer   DirectMessagePolicy
	FromOtherServers bool
}

// SetCallPolicy records who may call somebody.
func (s *Service) SetCallPolicy(ctx context.Context, userID uuid.UUID, policy CallPolicy) error {
	switch policy.FromThisServer {
	case DirectAnyone, DirectShared, DirectNobody:
	default:
		return ErrUnknownPolicy
	}
	if err := s.queries.SetCallPolicy(ctx, dbgen.SetCallPolicyParams{
		UserID: userID, Calls: string(policy.FromThisServer), CallsFromOtherServers: policy.FromOtherServers,
	}); err != nil {
		return fmt.Errorf("moderation: setting a call policy: %w", err)
	}
	return nil
}

// CallPolicyFor reads somebody's setting, defaulting to open as direct
// messages do.
func (s *Service) CallPolicyFor(ctx context.Context, userID uuid.UUID) CallPolicy {
	row, err := s.queries.GetCallPolicy(ctx, userID)
	if err != nil {
		return CallPolicy{FromThisServer: DirectAnyone, FromOtherServers: true}
	}
	return CallPolicy{FromThisServer: DirectMessagePolicy(row.Calls), FromOtherServers: row.CallsFromOtherServers}
}

/*
MayCall reports whether one person on this server may ring another.

Only yes or no, for the reason MayDirectMessage gives: which rule refused is
itself something the caller must not learn. A block is checked first.
*/
func (s *Service) MayCall(ctx context.Context, from, to uuid.UUID) bool {
	if from == to {
		return false
	}
	if s.HasBlocked(ctx, to, from) {
		return false
	}
	switch s.CallPolicyFor(ctx, to).FromThisServer {
	case DirectNobody:
		return false
	case DirectShared:
		shared, err := s.queries.SharesAChannel(ctx, dbgen.SharesAChannelParams{UserID: from, UserID_2: to})
		if err != nil {
			s.log.Warn("cannot check a shared channel", "error", err)
			return false
		}
		return shared
	default:
		return true
	}
}
