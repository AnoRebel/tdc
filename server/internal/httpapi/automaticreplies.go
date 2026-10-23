package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/autoreply"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/notify"
)

/*
Automatic replies: what the server says on somebody's behalf while they are
away, to a person who writes to them directly or names them in a channel.

The rules about when it speaks are the point of the feature, more than the
speaking. An automatic reply that fires too often is worse than none, and one
that gives away what somebody chose to hide is worse still. So:

  - It answers people, never programs: nothing that carries a "via" — a bot, a
    webhook, another automatic reply — is answered, and neither is the person
    themselves.
  - Once per sender per conversation per day, and at most HourlyCeiling
    conversations an hour for any one person.
  - Never to somebody either party has blocked, nor to somebody the person's
    own privacy setting would not let start a conversation with them.
  - A reply tied to a status does not fire while the person appears offline,
    because that status is withheld from everybody then. Nothing about the
    reply depends on whether they are connected.
  - Local only: in conversations this server holds, between people with
    accounts on it.
*/

// automaticReplyRequest is one automatic reply as a client sets it.
type automaticReplyRequest struct {
	Enabled         *bool      `json:"enabled"`
	Message         string     `json:"message"`
	MentionMessage  string     `json:"mentionMessage"`
	Statuses        []string   `json:"statuses"`
	StartsAt        *time.Time `json:"startsAt"`
	EndsAt          *time.Time `json:"endsAt"`
	TimeZone        string     `json:"timeZone"`
	ReplyToDirect   *bool      `json:"replyToDirect"`
	ReplyToGroups   *bool      `json:"replyToGroups"`
	ReplyToMentions *bool      `json:"replyToMentions"`
}

// automaticReplyResponse is one automatic reply as its owner sees it.
type automaticReplyResponse struct {
	ID              string     `json:"id"`
	Enabled         bool       `json:"enabled"`
	Message         string     `json:"message"`
	MentionMessage  string     `json:"mentionMessage"`
	Statuses        []string   `json:"statuses"`
	StartsAt        *time.Time `json:"startsAt,omitempty"`
	EndsAt          *time.Time `json:"endsAt,omitempty"`
	TimeZone        string     `json:"timeZone"`
	ReplyToDirect   bool       `json:"replyToDirect"`
	ReplyToGroups   bool       `json:"replyToGroups"`
	ReplyToMentions bool       `json:"replyToMentions"`
	// Active is whether it would answer right now, given the status as it is.
	Active    bool      `json:"active"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func orDefault(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func (request automaticReplyRequest) rule() autoreply.Rule {
	return autoreply.Rule{
		Enabled:         orDefault(request.Enabled, true),
		Message:         request.Message,
		MentionMessage:  request.MentionMessage,
		Statuses:        request.Statuses,
		StartsAt:        request.StartsAt,
		EndsAt:          request.EndsAt,
		TimeZone:        request.TimeZone,
		ReplyToDirect:   orDefault(request.ReplyToDirect, true),
		ReplyToGroups:   orDefault(request.ReplyToGroups, false),
		ReplyToMentions: orDefault(request.ReplyToMentions, true),
	}
}

func automaticReplyResponseOf(rule dbgen.AutomaticReply, own ownStatusForRules) automaticReplyResponse {
	return automaticReplyResponse{
		ID: rule.ID.String(), Enabled: rule.Enabled, Message: rule.Message,
		MentionMessage: rule.MentionMessage, Statuses: rule.Statuses,
		StartsAt: rule.StartsAt, EndsAt: rule.EndsAt, TimeZone: rule.TimeZone,
		ReplyToDirect: rule.ReplyToDirect, ReplyToGroups: rule.ReplyToGroups,
		ReplyToMentions: rule.ReplyToMentions,
		Active:          autoreply.Applies(rule, own.status, own.invisible, time.Now()),
		UpdatedAt:       rule.UpdatedAt,
	}
}

type ownStatusForRules struct {
	status    string
	invisible bool
}

func (s *Server) ownStatusForRules(ctx context.Context, userID uuid.UUID) ownStatusForRules {
	own, err := s.presence.Own(ctx, userID)
	if err != nil {
		return ownStatusForRules{status: "available"}
	}
	return ownStatusForRules{status: own.Status, invisible: own.Invisible}
}

// automaticReplyActive reports whether any of somebody's replies answers now.
func (s *Server) automaticReplyActive(ctx context.Context, userID uuid.UUID, own ownStatusForRules) bool {
	rules, err := s.autoreplies.List(ctx, userID)
	if err != nil {
		return false
	}
	for _, rule := range rules {
		if autoreply.Applies(rule, own.status, own.invisible, time.Now()) {
			return true
		}
	}
	return false
}

func automaticReplyError(err error) error {
	switch {
	case errors.Is(err, autoreply.ErrInvalid), errors.Is(err, autoreply.ErrTooMany):
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	case errors.Is(err, autoreply.ErrNotFound):
		return echo.NewHTTPError(http.StatusNotFound, "no such automatic reply")
	default:
		return err
	}
}

// checkStatuses refuses a status the operator has not configured, so a reply
// cannot be tied to something nobody can ever hold.
func (s *Server) checkStatuses(ctx context.Context, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	configured, err := s.presence.Statuses(ctx)
	if err != nil {
		return err
	}
	known := make(map[string]bool, len(configured))
	for _, status := range configured {
		known[status.Key] = true
	}
	for _, key := range keys {
		if !known[key] {
			return echo.NewHTTPError(http.StatusBadRequest, "there is no status called "+key)
		}
	}
	return nil
}

func (s *Server) handleListAutomaticReplies(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	rules, err := s.autoreplies.List(ctx, user.ID)
	if err != nil {
		return err
	}
	own := s.ownStatusForRules(ctx, user.ID)
	response := make([]automaticReplyResponse, 0, len(rules))
	for _, rule := range rules {
		response = append(response, automaticReplyResponseOf(rule, own))
	}
	return c.JSON(http.StatusOK, response)
}

func (s *Server) handleCreateAutomaticReply(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	var request automaticReplyRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	if err := s.checkStatuses(ctx, request.Statuses); err != nil {
		return err
	}
	created, err := s.autoreplies.Create(ctx, user.ID, request.rule())
	if err != nil {
		return automaticReplyError(err)
	}
	return c.JSON(http.StatusCreated, automaticReplyResponseOf(created, s.ownStatusForRules(ctx, user.ID)))
}

func (s *Server) handleUpdateAutomaticReply(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such automatic reply")
	}
	var request automaticReplyRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	if err := s.checkStatuses(ctx, request.Statuses); err != nil {
		return err
	}
	updated, err := s.autoreplies.Update(ctx, user.ID, id, request.rule())
	if err != nil {
		return automaticReplyError(err)
	}
	return c.JSON(http.StatusOK, automaticReplyResponseOf(updated, s.ownStatusForRules(ctx, user.ID)))
}

func (s *Server) handleDeleteAutomaticReply(c *echo.Context) error {
	user, _ := currentUser(c)

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such automatic reply")
	}
	if err := s.autoreplies.Delete(c.Request().Context(), user.ID, id); err != nil {
		return automaticReplyError(err)
	}
	return c.NoContent(http.StatusNoContent)
}

// messageContent is the part of a message's content automatic replies read.
type messageContent struct {
	Body string          `json:"body"`
	Via  json.RawMessage `json:"via"`
}

/*
answerAutomatically posts the automatic replies a message has earned.

Run after a message is stored, detached from the request like notifications:
an automatic reply that could not be posted must never cost the message it
answers.
*/
func (s *Server) answerAutomatically(ctx context.Context, event dbgen.Event) {
	if s.autoreplies == nil || s.presence == nil {
		return
	}
	// A person's message, and only that: webhooks have no account, and
	// bots and automatic replies carry a "via".
	if event.SenderID == nil || event.Type != string(events.TypeMessageSent) || event.RedactedAt != nil {
		return
	}
	var content messageContent
	if err := json.Unmarshal(event.Content, &content); err != nil || len(content.Via) > 0 {
		return
	}

	room, err := s.queries.GetRoomByID(ctx, event.RoomID)
	if err != nil || !s.rooms.HostedHere(room) {
		// Local only. A room another server holds is answered there or not
		// at all; posting into its replica here would fork its history.
		return
	}
	members, err := s.queries.ListRoomMembers(ctx, room.ID)
	if err != nil {
		return
	}
	sender, err := s.queries.GetUserByID(ctx, *event.SenderID)
	if err != nil {
		return
	}

	kind := autoreply.Mention
	threadRoot := event.ThreadRootID
	targets := make([]dbgen.ListRoomMembersRow, 0, 1)
	if room.Kind == "direct" {
		kind = autoreply.Direct
		if len(members) > 2 {
			kind = autoreply.Group
		}
		for _, member := range members {
			if member.ID != sender.ID {
				targets = append(targets, member)
			}
		}
	} else {
		/*
			A mention is answered in the thread of the message that named
			them — the least noisy place that still reaches the sender, who is
			told of the reply. The channel's own timeline is left alone.
		*/
		if threadRoot == nil {
			threadRoot = &event.ID
		}
		named := map[string]bool{}
		for _, name := range notify.MentionsIn(content.Body) {
			named[name] = true
		}
		for _, member := range members {
			if member.ID != sender.ID && named[strings.ToLower(member.Username)] {
				targets = append(targets, member)
			}
		}
	}

	for _, target := range targets {
		s.answerFor(ctx, room, sender, target.ID, kind, threadRoot)
	}
}

func (s *Server) answerFor(
	ctx context.Context,
	room dbgen.Room,
	sender dbgen.User,
	personID uuid.UUID,
	kind autoreply.Kind,
	threadRoot *string,
) {
	rules, err := s.autoreplies.List(ctx, personID)
	if err != nil || len(rules) == 0 {
		return
	}
	own := s.ownStatusForRules(ctx, personID)
	rule, ok := autoreply.Choose(rules, kind, own.status, own.invisible, time.Now())
	if !ok {
		return
	}

	/*
		Somebody who could not start a conversation with this person is not
		answered on their behalf either: a block in either direction, or the
		person's own setting for who may contact them. The silence is the
		same silence anybody gets from somebody who is not replying, so it
		discloses nothing about which applied.

		MayDirectMessage covers the person having blocked the sender; the
		other direction is checked here, since somebody who blocked this
		person does not want words from them, automatic or not.
	*/
	if s.moderation != nil {
		if !s.moderation.MayDirectMessage(ctx, sender.ID, personID) ||
			s.moderation.HasBlocked(ctx, sender.ID, personID) {
			return
		}
	}

	if !s.autoreplies.Claim(ctx, personID, room.ID, sender.ID) {
		return
	}

	person, err := s.queries.GetUserByID(ctx, personID)
	if err != nil {
		s.autoreplies.Release(ctx, personID, room.ID, sender.ID)
		return
	}
	text := autoreply.Text(rule, kind)
	reply, err := s.rooms.PostAutomaticReply(ctx, room, person, text, threadRoot)
	if err != nil {
		s.autoreplies.Release(ctx, personID, room.ID, sender.ID)
		s.log.Warn("could not post an automatic reply", "userId", personID, "roomId", room.ID, "error", err)
		return
	}

	s.indexForSearch(ctx, reply, text)
	if reply.ThreadRootID != nil {
		// The one it answers hears of it as a reply, which is what it is.
		if s.notify != nil {
			s.notify.About(ctx, sender.ID, person.ID, s.rooms.UserAddress(person.Username),
				reply.RoomID, reply.ID, notify.ReasonReply, "")
		}
	} else {
		s.notifyRecipients(ctx, reply, text, room.ID)
	}
	s.publishEvent(ctx, reply)
}
