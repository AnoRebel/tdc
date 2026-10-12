package rooms

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/identity"
)

/*
Links to a conversation's call.

A link is made by somebody allowed to, for one conversation, and admits whoever
holds it to that conversation's call — never to the conversation. It can be
public, or protected by a passcode, a lobby somebody in the call admits from,
or both; it can expire, be limited to a number of uses, and be revoked.

Using a link writes the same call-only invitation that merging writes, so a
person let in by a link is a guest of the call in exactly the way a merged
person is, and every check that keeps the conversation closed to one keeps it
closed to the other.
*/

var (
	// ErrLinkUnusable covers a link that is unknown, expired, used up or
	// revoked. They are refused alike, so a refusal does not say which.
	ErrLinkUnusable = errors.New("this link cannot be used")
	// ErrWrongPasscode is a protected link's passcode not matching.
	ErrWrongPasscode = errors.New("that passcode is not right")
	// ErrCallNotStarted is a link to a conversation with no call in progress.
	ErrCallNotStarted = errors.New("the call has not started")
)

// LinkOptions are how a link is made.
type LinkOptions struct {
	Label    string
	Passcode string
	Lobby    bool
	Guests   bool
	Expires  *time.Time
	MaxUses  *int32
}

// LinkSecretPrefix starts every call link's secret, so one is recognisable in
// a log or a leak report for what it is.
const LinkSecretPrefix = "tdcl_"

// CreateCallLink makes a link to a conversation's call and returns it with its
// secret, which is not kept and cannot be shown again.
func (s *Service) CreateCallLink(ctx context.Context, roomID, creator uuid.UUID, options LinkOptions) (dbgen.CallLink, string, error) {
	room, err := s.RequireMembership(ctx, roomID, creator)
	if err != nil {
		return dbgen.CallLink{}, "", err
	}
	if err := s.callable(room); err != nil {
		return dbgen.CallLink{}, "", err
	}
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return dbgen.CallLink{}, "", fmt.Errorf("generate call link: %w", err)
	}
	secret := LinkSecretPrefix + base64.RawURLEncoding.EncodeToString(raw)

	var passcode *string
	if options.Passcode != "" {
		hashed, err := identity.HashPasscode(options.Passcode)
		if err != nil {
			return dbgen.CallLink{}, "", err
		}
		passcode = &hashed
	}
	link, err := s.store.CreateCallLink(ctx, dbgen.CreateCallLinkParams{
		ID: uuid.New(), RoomID: roomID, TokenHash: linkHash(secret),
		Label: strings.TrimSpace(options.Label), PasscodeHash: passcode,
		Lobby: options.Lobby, Guests: options.Guests,
		ExpiresAt: options.Expires, MaxUses: options.MaxUses, CreatedBy: &creator,
	})
	if err != nil {
		return dbgen.CallLink{}, "", fmt.Errorf("create call link: %w", err)
	}
	return link, secret, nil
}

// CallLinks lists a conversation's links that have not been revoked.
func (s *Service) CallLinks(ctx context.Context, roomID uuid.UUID) ([]dbgen.CallLink, error) {
	return s.store.CallLinksInRoom(ctx, roomID)
}

// RevokeCallLink withdraws a link; whoever holds it can no longer use it.
func (s *Service) RevokeCallLink(ctx context.Context, roomID, linkID uuid.UUID) error {
	revoked, err := s.store.RevokeCallLink(ctx, dbgen.RevokeCallLinkParams{ID: linkID, RoomID: roomID})
	if err != nil {
		return fmt.Errorf("revoke call link: %w", err)
	}
	if revoked == 0 {
		return ErrLinkUnusable
	}
	return nil
}

/*
CallLink resolves a link's secret to the link, refusing one that cannot be
used now. It does not count a use; using does.
*/
func (s *Service) CallLink(ctx context.Context, secret string) (dbgen.CallLink, error) {
	if !strings.HasPrefix(secret, LinkSecretPrefix) {
		return dbgen.CallLink{}, ErrLinkUnusable
	}
	link, err := s.store.CallLinkByHash(ctx, linkHash(secret))
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.CallLink{}, ErrLinkUnusable
	}
	if err != nil {
		return dbgen.CallLink{}, fmt.Errorf("read call link: %w", err)
	}
	if !usable(link, time.Now()) {
		return dbgen.CallLink{}, ErrLinkUnusable
	}
	return link, nil
}

func usable(link dbgen.CallLink, now time.Time) bool {
	if link.RevokedAt != nil {
		return false
	}
	if link.ExpiresAt != nil && !link.ExpiresAt.After(now) {
		return false
	}
	return link.MaxUses == nil || link.Uses < *link.MaxUses
}

// Passcode failures a link tolerates in a window before refusing every
// passcode until the window passes.
const (
	PasscodeFailureLimit  = 20
	PasscodeFailureWindow = 15 * time.Minute
)

// ErrLinkLocked is a protected link refusing passcodes after too many wrong ones.
var ErrLinkLocked = errors.New("too many wrong passcodes for this link; try again later")

/*
CheckPasscode compares a passcode with a protected link's. The limit is
checked first, so past it even the right passcode is refused — otherwise the
limit would only slow a guesser down, not stop the guess that lands. A wrong
passcode is counted against the link.

A link without a passcode accepts anything.
*/
func (s *Service) CheckPasscode(ctx context.Context, link dbgen.CallLink, passcode string) error {
	if link.PasscodeHash == nil {
		return nil
	}
	if link.PasscodeFailures >= PasscodeFailureLimit &&
		link.PasscodeFailuresFrom.After(time.Now().Add(-PasscodeFailureWindow)) {
		return ErrLinkLocked
	}
	ok, err := identity.VerifyPassword(passcode, *link.PasscodeHash)
	if err == nil && ok {
		return nil
	}
	if _, err := s.store.RecordCallLinkFailure(ctx, dbgen.RecordCallLinkFailureParams{
		ID: link.ID, WindowSeconds: PasscodeFailureWindow.Seconds(),
	}); err != nil {
		return fmt.Errorf("record passcode failure: %w", err)
	}
	return ErrWrongPasscode
}

/*
UseCallLink lets somebody into the call a link leads to, once its passcode has
been checked: straight in where the link has no lobby, into the lobby where it
has. It returns the call and whether they are waiting.
*/
func (s *Service) UseCallLink(ctx context.Context, link dbgen.CallLink, userID uuid.UUID) (Call, bool, error) {
	call, err := s.openCall(ctx, link.RoomID)
	if errors.Is(err, ErrNoCall) {
		return Call{}, false, ErrCallNotStarted
	}
	if err != nil {
		return Call{}, false, err
	}
	if link.Lobby {
		if err := s.store.EnterCallLobby(ctx, dbgen.EnterCallLobbyParams{
			CallID: call.ID, UserID: userID, LinkID: link.ID,
		}); err != nil {
			return Call{}, false, fmt.Errorf("enter lobby: %w", err)
		}
		return call, true, nil
	}
	if err := s.admitByLink(ctx, link, call.ID, userID); err != nil {
		return Call{}, false, err
	}
	return call, false, nil
}

// admitByLink counts a use of the link and invites somebody into the call.
func (s *Service) admitByLink(ctx context.Context, link dbgen.CallLink, callID, userID uuid.UUID) error {
	used, err := s.store.UseCallLink(ctx, link.ID)
	if err != nil {
		return fmt.Errorf("use call link: %w", err)
	}
	if used == 0 {
		return ErrLinkUnusable
	}
	return s.store.InviteToCall(ctx, dbgen.InviteToCallParams{CallID: callID, UserID: userID, InvitedBy: link.CreatedBy})
}

// CallLobby lists who is waiting to be let into a call.
func (s *Service) CallLobby(ctx context.Context, callID uuid.UUID) ([]dbgen.CallLobbyRow, error) {
	return s.store.CallLobby(ctx, callID)
}

/*
Admit lets somebody waiting in a call's lobby in, or turns them away. Only
somebody in the call who belongs to its conversation decides: a person who
came in by a link does not let others in after them.
*/
func (s *Service) Admit(ctx context.Context, callID, deciderID, waitingID uuid.UUID, admit bool) error {
	call, guest, err := s.CallFor(ctx, callID, deciderID)
	if err != nil {
		return err
	}
	if guest {
		return ErrNoCall
	}
	if _, in := s.CallRoomOf(ctx, callID, deciderID); !in {
		return ErrNoCall
	}
	linkID, err := s.store.LeaveCallLobby(ctx, dbgen.LeaveCallLobbyParams{CallID: callID, UserID: waitingID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNoCall
	}
	if err != nil {
		return fmt.Errorf("leave lobby: %w", err)
	}
	if !admit {
		return nil
	}
	link, err := s.store.CallLinkByID(ctx, linkID)
	if err != nil {
		return fmt.Errorf("read call link: %w", err)
	}
	return s.admitByLink(ctx, link, call.ID, waitingID)
}

// LeaveLobby takes somebody out of a lobby they gave up waiting in.
func (s *Service) LeaveLobby(ctx context.Context, callID, userID uuid.UUID) {
	_, _ = s.store.LeaveCallLobby(ctx, dbgen.LeaveCallLobbyParams{CallID: callID, UserID: userID})
}

func linkHash(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}
