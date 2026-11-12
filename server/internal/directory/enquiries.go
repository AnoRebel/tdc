package directory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
Enquiries: a client's first contact with somebody through their profile.

An enquiry opens a private conversation holding one message — the enquiry —
with only the recipient in it. The sender has said what they came to say; the
recipient decides whether to take it further, and until they do the sender
cannot add to it. That is what stops an enquiry being a way around somebody's
direct-message setting: it is one message, to somebody who said they take
them, rate limited, and answerable only by them.
*/

var (
	/*
		ErrNotAccepting is every reason an enquiry cannot be sent to somebody:
		no such person, no profile the sender may see, a profile that takes no
		enquiries, or a sender the recipient has blocked. One error and one
		wording, so a refusal does not say which — a blocked person must not
		learn they were blocked, and a hidden profile must not confirm it
		exists.
	*/
	ErrNotAccepting = errors.New("that person is not accepting enquiries")
	// ErrAlreadyAsked reports an enquiry to somebody who has not yet answered
	// the last one from the same sender.
	ErrAlreadyAsked = errors.New("you have already sent them an enquiry they have not answered")
	// ErrNoEnquiry reports an enquiry the caller has no part in, or none.
	ErrNoEnquiry = errors.New("no such enquiry")
	// ErrAnswered reports a decision about an enquiry already decided.
	ErrAnswered = errors.New("that enquiry has already been answered")
)

const (
	maxEnquirySubject = 120
	maxEnquiryBody    = 4000
)

// EnquiryStatus is where an enquiry stands.
type EnquiryStatus string

const (
	EnquiryPending   EnquiryStatus = "pending"
	EnquiryAccepted  EnquiryStatus = "accepted"
	EnquiryDeclined  EnquiryStatus = "declined"
	EnquiryWithdrawn EnquiryStatus = "withdrawn"
)

// Recipient is somebody an enquiry can be sent to.
type Recipient struct {
	ID          uuid.UUID
	Username    string
	DisplayName string
}

// Enquiry is one enquiry, from the point of view of one of its two people.
type Enquiry struct {
	ID          uuid.UUID
	SenderID    uuid.UUID
	RecipientID uuid.UUID
	Subject     string
	Body        string
	RoomID      *uuid.UUID
	Status      EnquiryStatus
	CreatedAt   time.Time
	RespondedAt *time.Time
	// Other is the other person: the sender for somebody who received it, the
	// recipient for somebody who sent it.
	OtherUsername    string
	OtherDisplayName string
	// EngagementID is the engagement it became, once taken up.
	EngagementID *uuid.UUID
}

// EnquiryInput is what somebody sends.
type EnquiryInput struct {
	Subject string
	Body    string
}

// Normalise trims an enquiry and refuses one that cannot be sent.
func (input *EnquiryInput) Normalise() error {
	input.Subject = strings.Join(strings.Fields(input.Subject), " ")
	input.Body = strings.TrimSpace(input.Body)
	switch {
	case input.Subject == "":
		return invalid("subject", "say what it is about")
	case utf8.RuneCountInString(input.Subject) > maxEnquirySubject:
		return invalid("subject", fmt.Sprintf("at most %d characters", maxEnquirySubject))
	case input.Body == "":
		return invalid("body", "describe the work")
	case utf8.RuneCountInString(input.Body) > maxEnquiryBody:
		return invalid("body", fmt.Sprintf("at most %d characters", maxEnquiryBody))
	}
	return nil
}

// RecipientFor is whom an enquiry to this username would reach, if they take
// one from this sender; ErrNotAccepting otherwise, whatever the reason.
func (s *Service) RecipientFor(ctx context.Context, username string, sender uuid.UUID, rank Rank) (Recipient, error) {
	row, err := s.queries.EnquiryRecipient(ctx, dbgen.EnquiryRecipientParams{
		ViewerRank: int16(rank), Sender: sender, Username: username,
	})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (!row.Accepts || row.ID == sender)) {
		return Recipient{}, ErrNotAccepting
	}
	if err != nil {
		return Recipient{}, fmt.Errorf("find an enquiry's recipient: %w", err)
	}
	return Recipient{ID: row.ID, Username: row.Username, DisplayName: row.DisplayName}, nil
}

/*
OpenEnquiryRoom makes the private conversation an enquiry lives in, with both
people in it so the sender can post the enquiry. CloseEnquiry takes the sender
out again once it is recorded.
*/
func (s *Service) OpenEnquiryRoom(ctx context.Context, sender, recipient uuid.UUID, subject, homeServer string) (dbgen.Room, error) {
	key := "enquiry:" + uuid.New().String()
	room, err := s.queries.CreateEnquiryRoom(ctx, dbgen.CreateEnquiryRoomParams{
		ID: uuid.New(), Name: subject, HomeServer: homeServer,
		CreatedBy: &sender, ParticipantKey: &key,
	})
	if err != nil {
		return dbgen.Room{}, fmt.Errorf("open an enquiry's conversation: %w", err)
	}
	for _, member := range []uuid.UUID{recipient, sender} {
		if err := s.queries.AddRoomMember(ctx, dbgen.AddRoomMemberParams{RoomID: room.ID, UserID: member}); err != nil {
			s.AbandonEnquiryRoom(ctx, room.ID)
			return dbgen.Room{}, fmt.Errorf("add to an enquiry's conversation: %w", err)
		}
	}
	return room, nil
}

// AbandonEnquiryRoom removes a conversation opened for an enquiry that then
// could not be sent, so a failure leaves nothing half made.
func (s *Service) AbandonEnquiryRoom(ctx context.Context, roomID uuid.UUID) {
	if err := s.queries.DeleteEnquiryRoom(ctx, roomID); err != nil {
		s.log.Error("cannot remove an abandoned enquiry's conversation", "roomId", roomID, "error", err)
	}
}

/*
RecordEnquiry records a sent enquiry and takes the sender out of its
conversation, which from now on holds only the recipient until they answer.
*/
func (s *Service) RecordEnquiry(ctx context.Context, sender, recipient uuid.UUID, roomID uuid.UUID, input EnquiryInput) (Enquiry, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Enquiry{}, fmt.Errorf("begin recording an enquiry: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)

	row, err := queries.InsertEnquiry(ctx, dbgen.InsertEnquiryParams{
		ID: uuid.New(), SenderID: sender, RecipientID: recipient,
		Subject: input.Subject, Body: input.Body, RoomID: &roomID,
	})
	if isUniqueViolation(err) {
		return Enquiry{}, ErrAlreadyAsked
	}
	if err != nil {
		return Enquiry{}, fmt.Errorf("record an enquiry: %w", err)
	}
	if err := queries.RemoveRoomMember(ctx, dbgen.RemoveRoomMemberParams{RoomID: roomID, UserID: sender}); err != nil {
		return Enquiry{}, fmt.Errorf("close an enquiry's conversation to its sender: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Enquiry{}, fmt.Errorf("commit an enquiry: %w", err)
	}
	return enquiryOf(row, "", ""), nil
}

// PendingBetween reports whether the sender already has an unanswered
// enquiry with the recipient, checked before anything is made.
func (s *Service) PendingBetween(ctx context.Context, sender, recipient uuid.UUID) (bool, error) {
	rows, err := s.queries.ListSentEnquiries(ctx, sender)
	if err != nil {
		return false, fmt.Errorf("list enquiries: %w", err)
	}
	for _, row := range rows {
		if row.RecipientID == recipient && row.Status == string(EnquiryPending) {
			return true, nil
		}
	}
	return false, nil
}

// Received lists the enquiries somebody was sent, newest first.
func (s *Service) Received(ctx context.Context, userID uuid.UUID) ([]Enquiry, error) {
	rows, err := s.queries.ListReceivedEnquiries(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list received enquiries: %w", err)
	}
	enquiries := make([]Enquiry, 0, len(rows))
	for _, row := range rows {
		enquiries = append(enquiries, enquiryOf(dbgen.Enquiry{
			ID: row.ID, SenderID: row.SenderID, RecipientID: row.RecipientID, Subject: row.Subject,
			Body: row.Body, RoomID: row.RoomID, Status: row.Status, CreatedAt: row.CreatedAt,
			RespondedAt: row.RespondedAt,
		}, row.OtherUsername, row.OtherDisplayName))
		if row.EngagementID != uuid.Nil() {
			id := row.EngagementID
			enquiries[len(enquiries)-1].EngagementID = &id
		}
	}
	return enquiries, nil
}

// Sent lists the enquiries somebody sent, newest first.
func (s *Service) Sent(ctx context.Context, userID uuid.UUID) ([]Enquiry, error) {
	rows, err := s.queries.ListSentEnquiries(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list sent enquiries: %w", err)
	}
	enquiries := make([]Enquiry, 0, len(rows))
	for _, row := range rows {
		enquiries = append(enquiries, enquiryOf(dbgen.Enquiry{
			ID: row.ID, SenderID: row.SenderID, RecipientID: row.RecipientID, Subject: row.Subject,
			Body: row.Body, RoomID: row.RoomID, Status: row.Status, CreatedAt: row.CreatedAt,
			RespondedAt: row.RespondedAt,
		}, row.OtherUsername, row.OtherDisplayName))
		if row.EngagementID != uuid.Nil() {
			id := row.EngagementID
			enquiries[len(enquiries)-1].EngagementID = &id
		}
	}
	return enquiries, nil
}

func enquiryOf(row dbgen.Enquiry, otherUsername, otherDisplayName string) Enquiry {
	return Enquiry{
		ID: row.ID, SenderID: row.SenderID, RecipientID: row.RecipientID,
		Subject: row.Subject, Body: row.Body, RoomID: row.RoomID,
		Status: EnquiryStatus(row.Status), CreatedAt: row.CreatedAt, RespondedAt: row.RespondedAt,
		OtherUsername: otherUsername, OtherDisplayName: otherDisplayName,
	}
}
