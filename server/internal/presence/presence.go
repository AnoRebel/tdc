// Package presence tracks who is connected and what they have chosen to show.
package presence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

// State is what others see of a person.
type State string

const (
	StateOnline  State = "online"
	StateOffline State = "offline"
)

// StaleAfter is how long a connection may go unrefreshed before it is treated
// as gone.
//
// Longer than the keepalive interval, so an ordinary pause between pings does
// not read as a disconnection, but short enough that someone whose connection
// died does not appear present for long.
const StaleAfter = 2 * time.Minute

// TypingLifetime is how long an indication lasts without being refreshed.
//
// Short, because the alternative is an indicator left on by someone who closed
// their laptop mid-sentence.
const TypingLifetime = 8 * time.Second

// Presence is what one person's presence looks like to someone else.
type Presence struct {
	UserID uuid.UUID
	State  State
	// Status is the operator-configured status the person has chosen.
	Status     string
	StatusText string
	LastSeenAt time.Time
	// SuppressesNotifications reports whether this status withholds them.
	SuppressesNotifications bool
}

// Store is the database surface this package needs.
type Store interface {
	dbgen.Querier
}

// Service tracks presence, status and typing.
type Service struct {
	store Store
}

// NewService builds a presence service.
func NewService(store Store) *Service {
	return &Service{store: store}
}

// Connected records that a person has opened a live connection.
func (s *Service) Connected(ctx context.Context, userID uuid.UUID) error {
	if _, err := s.store.ConnectionOpened(ctx, userID); err != nil {
		return fmt.Errorf("record connection: %w", err)
	}
	return nil
}

// Disconnected records that one of a person's connections has closed.
//
// Presence ends when the last one closes, not the first, so closing a laptop
// while a phone stays connected does not make someone appear offline.
func (s *Service) Disconnected(ctx context.Context, userID uuid.UUID) error {
	if err := s.store.ConnectionClosed(ctx, userID); err != nil {
		return fmt.Errorf("record disconnection: %w", err)
	}
	return nil
}

// Touch refreshes a connection's liveness, so it is not swept as stale.
func (s *Service) Touch(ctx context.Context, userID uuid.UUID) error {
	if err := s.store.TouchPresence(ctx, userID); err != nil {
		return fmt.Errorf("refresh presence: %w", err)
	}
	return nil
}

// Of returns what one person's presence looks like.
//
// Someone who has chosen to appear offline is reported offline regardless of
// whether they are connected, and their status is withheld: showing it would
// give away that they are there.
func (s *Service) Of(ctx context.Context, userID uuid.UUID) (Presence, error) {
	record, err := s.store.GetPresence(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Never connected: offline, with the default status.
		return Presence{UserID: userID, State: StateOffline, Status: "available"}, nil
	}
	if err != nil {
		return Presence{}, fmt.Errorf("read presence: %w", err)
	}

	return s.present(ctx, record), nil
}

// ForMany returns presence for a set of people in one query, which is what a
// member list or a room header needs.
func (s *Service) ForMany(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]Presence, error) {
	if len(userIDs) == 0 {
		return map[uuid.UUID]Presence{}, nil
	}

	records, err := s.store.ListPresenceFor(ctx, userIDs)
	if err != nil {
		return nil, fmt.Errorf("read presence: %w", err)
	}

	states := make(map[uuid.UUID]Presence, len(userIDs))
	for _, record := range records {
		states[record.UserID] = s.present(ctx, record)
	}

	// Anyone with no record has never connected.
	for _, id := range userIDs {
		if _, ok := states[id]; !ok {
			states[id] = Presence{UserID: id, State: StateOffline, Status: "available"}
		}
	}

	return states, nil
}

// present renders a stored record as what others may see.
func (s *Service) present(ctx context.Context, record dbgen.UserPresence) Presence {
	state := StateOffline

	switch {
	case record.Invisible:
		// Chosen to appear offline. Reported offline while remaining fully
		// functional, and the status is withheld with it.
		state = StateOffline
	case record.Connections > 0 && time.Since(record.LastSeenAt) < StaleAfter:
		state = StateOnline
	}

	// A status that has lapsed reads as the default, even before the sweep
	// has run, so a reader never sees an expired status.
	status := record.Status
	statusText := record.StatusText
	if record.StatusExpiresAt != nil && time.Now().After(*record.StatusExpiresAt) {
		status = "available"
		statusText = ""
	}
	if record.Invisible {
		status = "available"
		statusText = ""
	}

	suppresses := false
	if definition, err := s.store.GetServerStatus(ctx, status); err == nil {
		suppresses = definition.SuppressesNotifications
	}

	return Presence{
		UserID:                  record.UserID,
		State:                   state,
		Status:                  status,
		StatusText:              statusText,
		LastSeenAt:              record.LastSeenAt,
		SuppressesNotifications: suppresses,
	}
}

// ErrUnknownStatus reports a status the operator has not configured.
var ErrUnknownStatus = errors.New("unknown status")

// SetStatus records the status a person has chosen.
//
// Only a status the operator has configured may be chosen, so a client cannot
// invent one that others would not understand.
func (s *Service) SetStatus(
	ctx context.Context,
	userID uuid.UUID,
	status, statusText string,
	expiresAt *time.Time,
) error {
	if _, err := s.store.GetServerStatus(ctx, status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: %q", ErrUnknownStatus, status)
		}
		return fmt.Errorf("read status: %w", err)
	}

	if _, err := s.store.SetStatus(ctx, dbgen.SetStatusParams{
		UserID:          userID,
		Status:          status,
		StatusText:      statusText,
		StatusExpiresAt: expiresAt,
	}); err != nil {
		return fmt.Errorf("set status: %w", err)
	}

	return nil
}

// SetInvisible controls whether a person appears offline while connected.
func (s *Service) SetInvisible(ctx context.Context, userID uuid.UUID, invisible bool) error {
	if err := s.store.SetInvisible(ctx, dbgen.SetInvisibleParams{
		UserID: userID, Invisible: invisible,
	}); err != nil {
		return fmt.Errorf("set invisibility: %w", err)
	}
	return nil
}

// Statuses returns what the operator has made available.
func (s *Service) Statuses(ctx context.Context) ([]dbgen.ServerStatus, error) {
	statuses, err := s.store.ListServerStatuses(ctx)
	if err != nil {
		return nil, fmt.Errorf("list statuses: %w", err)
	}
	return statuses, nil
}

// StartTyping records that a person is composing in a room.
//
// The indication expires on its own rather than depending on the client to
// withdraw it, so someone who closes their laptop mid-sentence does not leave
// an indicator on permanently.
func (s *Service) StartTyping(ctx context.Context, roomID, userID uuid.UUID) error {
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("read user: %w", err)
	}
	if !user.SendsTypingIndication {
		// Opted out: nothing is recorded, so nothing is shown.
		return nil
	}

	if err := s.store.StartTyping(ctx, dbgen.StartTypingParams{
		RoomID:    roomID,
		UserID:    userID,
		ExpiresAt: time.Now().Add(TypingLifetime),
	}); err != nil {
		return fmt.Errorf("record typing: %w", err)
	}
	return nil
}

// StopTyping withdraws an indication early, when a person sends or clears
// what they were writing.
func (s *Service) StopTyping(ctx context.Context, roomID, userID uuid.UUID) error {
	if err := s.store.StopTyping(ctx, dbgen.StopTypingParams{
		RoomID: roomID, UserID: userID,
	}); err != nil {
		return fmt.Errorf("withdraw typing: %w", err)
	}
	return nil
}

// Typing returns who is currently composing in a room, excluding the asker:
// nobody needs to be told they are typing.
func (s *Service) Typing(ctx context.Context, roomID, excluding uuid.UUID) ([]dbgen.ListTypingRow, error) {
	typing, err := s.store.ListTyping(ctx, roomID)
	if err != nil {
		return nil, fmt.Errorf("read typing: %w", err)
	}

	others := make([]dbgen.ListTypingRow, 0, len(typing))
	for _, row := range typing {
		if row.UserID != excluding {
			others = append(others, row)
		}
	}
	return others, nil
}

// SetTypingPreference controls whether a person sends typing indication.
func (s *Service) SetTypingPreference(ctx context.Context, userID uuid.UUID, sends bool) error {
	if err := s.store.SetTypingPreference(ctx, dbgen.SetTypingPreferenceParams{
		ID:                    userID,
		SendsTypingIndication: sends,
	}); err != nil {
		return fmt.Errorf("set typing preference: %w", err)
	}
	return nil
}

// Sweep clears state that has lapsed.
//
// Run periodically. Presence from connections that ended without a clean close
// is what makes an ungraceful disconnect expire rather than persist; expired
// statuses and typing indications are cleared here too.
func (s *Service) Sweep(ctx context.Context) error {
	if err := s.store.ResetStalePresence(ctx, time.Now().Add(-StaleAfter)); err != nil {
		return fmt.Errorf("clear stale presence: %w", err)
	}
	if err := s.store.ClearExpiredStatuses(ctx); err != nil {
		return fmt.Errorf("clear expired statuses: %w", err)
	}
	if err := s.store.ClearExpiredTyping(ctx); err != nil {
		return fmt.Errorf("clear expired typing: %w", err)
	}
	return nil
}
