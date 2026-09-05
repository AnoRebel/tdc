/*
Package invitations issues and redeems the links and codes that admit somebody
to a channel or a server.

An invitation is a bearer credential: whoever holds it can use it. That shapes
everything here. The secret is stored hashed, so reading the database does not
yield working invitations. Revocation is immediate, because somebody who has
already been sent a link cannot be un-sent it. And every refusal is the same
refusal, because a message that distinguished "expired" from "no such
invitation" would tell somebody guessing that they had guessed correctly.
*/
package invitations

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
ErrNotUsable is every refusal.

One error for expired, exhausted, revoked, bound to somebody else, and never
existed. Distinguishing them would turn the redemption endpoint into an oracle:
"expired" tells somebody guessing codes that they found a real one, which is
most of the work of guessing it.

The person who was legitimately sent an invitation and finds it expired is told
to ask for a new one, which is the same advice in every case.
*/
var ErrNotUsable = errors.New("that invitation cannot be used")

// ErrAlreadyUsed is returned when the same person redeems twice.
//
// Distinct from ErrNotUsable because it is not a refusal: they are already in,
// which is what they wanted, and telling them so is not a disclosure — they
// held a working invitation to learn it.
var ErrAlreadyUsed = errors.New("you have already used this invitation")

// Invitation is one issued invitation, without its secret.
type Invitation struct {
	ID     uuid.UUID
	RoomID *uuid.UUID
	// Token and Code are set only when an invitation is first issued. They are
	// not stored, so they can never be shown again — which is what makes the
	// hash-only storage meaningful rather than theatre.
	Token string
	Code  string

	CreatedBy string
	BoundTo   string
	MaxUses   *int32
	Uses      int32
	ExpiresAt *time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
}

// Usable reports whether an invitation would be accepted right now.
//
// For display, not for the decision: the decision is made in one statement
// against the database so that two people cannot both take the last use.
func (i Invitation) Usable() bool {
	switch {
	case i.RevokedAt != nil:
		return false
	case i.ExpiresAt != nil && !i.ExpiresAt.After(time.Now()):
		return false
	case i.MaxUses != nil && i.Uses >= *i.MaxUses:
		return false
	default:
		return true
	}
}

// Service issues and redeems invitations.
type Service struct {
	queries *dbgen.Queries
	log     *slog.Logger
}

func NewService(pool *db.Pool, log *slog.Logger) *Service {
	return &Service{queries: dbgen.New(pool), log: log}
}

// Terms are the conditions an invitation is issued under.
type Terms struct {
	// RoomID is the channel this admits somebody to. Nil for the server.
	RoomID *uuid.UUID
	// BoundTo restricts it to one email address, so forwarding it does not
	// work.
	BoundTo string
	// MaxUses is how many times it may be accepted. Nil is unlimited.
	MaxUses *int32
	// ExpiresIn is how long it lasts. Zero never expires.
	ExpiresIn time.Duration
	// WithCode issues a short code alongside the link, for reading aloud.
	WithCode bool
}

/*
maxLifetime bounds how long an invitation may last.

A year, which is far longer than any real invitation and short enough that a
link forgotten in a chat log eventually stops working. An operator wanting a
permanent join link gets one by not setting an expiry at all, which is a
deliberate choice rather than a number that drifted.
*/
const maxLifetime = 365 * 24 * time.Hour

// Issue creates an invitation and returns its secrets, once.
func (s *Service) Issue(
	ctx context.Context,
	by uuid.UUID,
	byAddress string,
	terms Terms,
) (Invitation, error) {
	if terms.ExpiresIn > maxLifetime {
		return Invitation{}, fmt.Errorf("an invitation may last at most a year")
	}

	token, err := newSecret(32)
	if err != nil {
		return Invitation{}, err
	}

	params := dbgen.CreateInvitationParams{
		ID:               uuid.New(),
		TokenHash:        hash(token),
		RoomID:           terms.RoomID,
		CreatedBy:        &by,
		CreatedByAddress: byAddress,
		BoundTo:          optional(strings.ToLower(strings.TrimSpace(terms.BoundTo))),
		MaxUses:          terms.MaxUses,
	}

	if terms.ExpiresIn > 0 {
		expires := time.Now().Add(terms.ExpiresIn)
		params.ExpiresAt = &expires
	}

	code := ""
	if terms.WithCode {
		code, err = newCode()
		if err != nil {
			return Invitation{}, err
		}
		params.CodeHash = hash(code)
	}

	row, err := s.queries.CreateInvitation(ctx, params)
	if err != nil {
		return Invitation{}, fmt.Errorf("invitations: issuing: %w", err)
	}

	issued := invitationOf(row)
	// Returned here and never stored. Somebody who loses the link issues
	// another rather than recovering this one, which is what "stored hashed"
	// has to mean to be worth anything.
	issued.Token = token
	issued.Code = code

	return issued, nil
}

/*
Redeem accepts an invitation, returning what it admits somebody to.

The check and the increment happen in one statement, so two people accepting
the last use of an invitation cannot both succeed — a check followed by an
update would let an invitation limited to one use admit two people.
*/
func (s *Service) Redeem(
	ctx context.Context,
	secret string,
	byCode bool,
	userID uuid.UUID,
	email string,
) (Invitation, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return Invitation{}, ErrNotUsable
	}

	var row dbgen.Invitation
	var err error

	if byCode {
		row, err = s.queries.FindInvitationByCodeHash(ctx, hash(normaliseCode(secret)))
	} else {
		row, err = s.queries.FindInvitationByTokenHash(ctx, hash(secret))
	}

	if errors.Is(err, pgx.ErrNoRows) {
		// The same refusal as every other. An invitation that never existed
		// must be indistinguishable from one that expired, or this endpoint
		// tells somebody guessing when they have guessed right.
		return Invitation{}, ErrNotUsable
	}
	if err != nil {
		return Invitation{}, fmt.Errorf("invitations: reading: %w", err)
	}

	invitation := invitationOf(row)

	// Bound to somebody else. Checked before the use is claimed, so a
	// forwarded invitation does not consume one of its uses being refused.
	if row.BoundTo != nil && *row.BoundTo != "" {
		if !strings.EqualFold(strings.TrimSpace(email), *row.BoundTo) {
			return Invitation{}, ErrNotUsable
		}
	}

	// Already accepted by this person: they are in, which is what they wanted.
	used, err := s.queries.HasUsedInvitation(ctx, dbgen.HasUsedInvitationParams{
		InvitationID: row.ID,
		UserID:       userID,
	})
	if err == nil && used {
		return invitation, ErrAlreadyUsed
	}

	claimed, err := s.queries.RecordInvitationUse(ctx, row.ID)
	if err != nil {
		return Invitation{}, fmt.Errorf("invitations: claiming a use: %w", err)
	}
	if claimed == 0 {
		// Revoked, expired, or exhausted — the statement's own conditions
		// decided, and which one it was is not disclosed.
		return Invitation{}, ErrNotUsable
	}

	if err := s.queries.RecordInvitationUser(ctx, dbgen.RecordInvitationUserParams{
		InvitationID: row.ID,
		UserID:       userID,
	}); err != nil {
		// The use is claimed and the person is being admitted. Failing to
		// record who they were costs an audit entry, not their membership.
		s.log.Error("cannot record who used an invitation",
			"invitationId", row.ID, "userId", userID, "error", err)
	}

	return invitation, nil
}

// Revoke stops an invitation working, immediately.
//
// Immediate is the point: somebody who has already been sent a link cannot be
// un-sent it, so the only way to withdraw it is for the server to stop
// honouring it.
func (s *Service) Revoke(ctx context.Context, id, by uuid.UUID) (Invitation, error) {
	row, err := s.queries.RevokeInvitation(ctx, dbgen.RevokeInvitationParams{
		ID:        id,
		RevokedBy: &by,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Invitation{}, ErrNotUsable
	}
	if err != nil {
		return Invitation{}, fmt.Errorf("invitations: revoking: %w", err)
	}

	return invitationOf(row), nil
}

// ForRoom lists a channel's invitations, including withdrawn ones.
func (s *Service) ForRoom(ctx context.Context, roomID uuid.UUID, limit int32) ([]Invitation, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	rows, err := s.queries.ListInvitationsForRoom(ctx, dbgen.ListInvitationsForRoomParams{
		RoomID: &roomID,
		Limit:  limit,
	})
	if err != nil {
		return nil, fmt.Errorf("invitations: listing: %w", err)
	}

	return invitationsOf(rows), nil
}

// ForServer lists invitations to the server itself.
func (s *Service) ForServer(ctx context.Context, limit int32) ([]Invitation, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	rows, err := s.queries.ListServerInvitations(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("invitations: listing: %w", err)
	}

	return invitationsOf(rows), nil
}

func invitationsOf(rows []dbgen.Invitation) []Invitation {
	out := make([]Invitation, 0, len(rows))
	for _, row := range rows {
		out = append(out, invitationOf(row))
	}
	return out
}

func invitationOf(row dbgen.Invitation) Invitation {
	invitation := Invitation{
		ID:        row.ID,
		RoomID:    row.RoomID,
		CreatedBy: row.CreatedByAddress,
		MaxUses:   row.MaxUses,
		Uses:      row.Uses,
		ExpiresAt: row.ExpiresAt,
		RevokedAt: row.RevokedAt,
		CreatedAt: row.CreatedAt,
	}
	if row.BoundTo != nil {
		invitation.BoundTo = *row.BoundTo
	}
	return invitation
}

/*
newSecret makes a link token.

256 bits from the system's own source. Long enough that guessing is not a
strategy, which is what lets the link path be rate limited far more loosely
than the code path.
*/
func newSecret(bytes int) (string, error) {
	raw := make([]byte, bytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("invitations: generating a token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

/*
newCode makes a short code somebody can read aloud.

Base32 without padding, which excludes the characters that are misread when
spoken or typed: no 0 against O, no 1 against I. Ten characters is about fifty
bits — short enough to dictate over a phone, and far too short to be left
unguarded, which is why codes are rate limited much more strictly than links.
*/
func newCode() (string, error) {
	raw := make([]byte, 7)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("invitations: generating a code: %w", err)
	}

	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
	return strings.ToUpper(encoded[:10]), nil
}

// normaliseCode makes a typed code match an issued one.
//
// People type codes with spaces and hyphens and in whatever case their keyboard
// produces, and none of that is meant to be part of the secret.
func normaliseCode(code string) string {
	replacer := strings.NewReplacer(" ", "", "-", "", "_", "")
	return strings.ToUpper(replacer.Replace(strings.TrimSpace(code)))
}

/*
hash reduces a secret to what is stored.

SHA-256 rather than a password hash. A password is chosen by a person and
therefore guessable, which is what the slow hashes defend against; these are
256 and 50 bits from the system's own source, where the defence is the entropy
itself. A slow hash here would only make redemption slow.
*/
func hash(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

func optional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

/*
WouldAdmit reports whether an invitation would be accepted, without accepting
it.

Used during registration, where the account does not exist yet: there is nobody
to record as having used it, so the check and the claim are separate steps. The
claim happens once the account exists.

That leaves a window in which two people could both pass this check against an
invitation with one use left, and only one of them claim it. The other is
admitted to the server without consuming a use, which is the lesser of the two
failures available — refusing somebody who held a valid invitation because
another person was a moment faster is worse than an off-by-one on a count.

A single answer for every refusal, so the registration form does not become an
oracle for guessing codes.
*/
func (s *Service) WouldAdmit(ctx context.Context, secret, email string) bool {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return false
	}

	// Either spelling: somebody registering pastes whichever they were given,
	// and the form has no reason to ask which kind it is.
	row, err := s.queries.FindInvitationByTokenHash(ctx, hash(secret))
	if errors.Is(err, pgx.ErrNoRows) {
		row, err = s.queries.FindInvitationByCodeHash(ctx, hash(normaliseCode(secret)))
	}
	if err != nil {
		return false
	}

	invitation := invitationOf(row)
	if !invitation.Usable() {
		return false
	}

	if row.BoundTo != nil && *row.BoundTo != "" {
		return strings.EqualFold(strings.TrimSpace(email), *row.BoundTo)
	}

	return true
}

/*
ClaimForRegistration records that a new account used an invitation.

Separate from WouldAdmit because the account has to exist before it can be
recorded as having used one. A failure here costs the use count and the audit
entry, not the account: somebody who registered with a valid invitation is
registered.
*/
func (s *Service) ClaimForRegistration(ctx context.Context, secret string, userID uuid.UUID) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return
	}

	row, err := s.queries.FindInvitationByTokenHash(ctx, hash(secret))
	if errors.Is(err, pgx.ErrNoRows) {
		row, err = s.queries.FindInvitationByCodeHash(ctx, hash(normaliseCode(secret)))
	}
	if err != nil {
		return
	}

	if _, err := s.queries.RecordInvitationUse(ctx, row.ID); err != nil {
		s.log.Error("cannot claim an invitation used to register",
			"invitationId", row.ID, "error", err)
		return
	}

	if err := s.queries.RecordInvitationUser(ctx, dbgen.RecordInvitationUserParams{
		InvitationID: row.ID,
		UserID:       userID,
	}); err != nil {
		s.log.Error("cannot record who registered with an invitation",
			"invitationId", row.ID, "userId", userID, "error", err)
	}
}
