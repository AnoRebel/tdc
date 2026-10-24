package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/actions"
	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/moderation"
	"github.com/AnoRebel/tdc/server/internal/notify"
	"github.com/AnoRebel/tdc/server/internal/rooms"
	"github.com/AnoRebel/tdc/server/internal/settings"
)

// roomResponse is the public shape of a room.
type roomResponse struct {
	ID         string `json:"id"`
	Slug       string `json:"slug"`
	Name       string `json:"name"`
	Topic      string `json:"topic"`
	Kind       string `json:"kind"`
	Visibility string `json:"visibility"`
	// Address is the federated form, room@server.
	Address  string `json:"address"`
	Archived bool   `json:"archived"`
	// Member says whether the requester is in this room, so a client can
	// distinguish a room it can read from one it must join first.
	Member bool `json:"member"`

	/*
		HomeServer is the server that owns this room, when that is not this
		one.

		Absent for an ordinary room, so a client that knows nothing about
		federation sees exactly what it saw before. Present for a replica,
		which a client needs in order to know that joining and posting go
		through a different path — and to say so, since a room hosted
		elsewhere behaves differently when that server is down.
	*/
	HomeServer string `json:"homeServer,omitempty"`

	/*
		RetentionSeconds is how long messages here are kept, or absent when
		they are kept indefinitely.

		Disclosed to everybody who can see the channel, because a conversation
		that quietly deletes itself is one people say things in they would not
		have said knowing. Somebody deciding what to write is entitled to know
		how long it lasts.
	*/
	RetentionSeconds int32 `json:"retentionSeconds,omitempty"`
	// RetentionFromChannel says this channel sets its own period rather than
	// inheriting the server's, which are different things to be told.
	RetentionFromChannel bool `json:"retentionFromChannel,omitempty"`
}

/*
roomResponse describes a channel to a client.

Reads the server's retention default rather than taking it as an argument. The
settings service caches, so this costs nothing on the common path, and
threading it through six call sites would put a parameter nobody reads at every
one of them.
*/
func (s *Server) roomResponse(room dbgen.Room, member bool) roomResponse {
	policy := moderation.PolicyFor(room.MessageTtlSeconds, s.retentionDefault())
	slug := ""
	if room.Slug != nil {
		slug = *room.Slug
	}

	/*
		Only when it is somebody else's: a client that knows nothing about
		federation should see exactly the shape it saw before.

		Compared with the name rooms are recorded under, not the canonical
		address's host. The two agree when a canonical address is set; without
		one the host is empty while rooms are recorded as "localhost", and every
		room on the server was reported as another server's — which the client
		takes at its word and sends through federation, which is off.
	*/
	homeServer := ""
	if !s.rooms.HostedHere(room) {
		homeServer = room.HomeServer
	}

	return roomResponse{
		ID:                   room.ID.String(),
		HomeServer:           homeServer,
		RetentionSeconds:     policy.Seconds,
		RetentionFromChannel: policy.FromChannel,
		Slug:                 slug,
		Name:                 room.Name,
		Topic:                room.Topic,
		Kind:                 room.Kind,
		Visibility:           room.Visibility,
		Address:              s.rooms.RoomAddress(room),
		Archived:             room.ArchivedAt != nil,
		Member:               member,
	}
}

// messageResponse is the public shape of an event in a timeline.
type messageResponse struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Sender   string          `json:"sender"`
	Sequence int64           `json:"sequence"`
	Content  json.RawMessage `json:"content"`
	// Attachments is absent when a message carries no files, which is most of
	// them.
	Attachments []attachmentResponse `json:"attachments,omitempty"`
	SentAt      time.Time            `json:"sentAt"`
	Redacted    bool                 `json:"redacted"`

	/*
		Lifetime describes when this message will be removed, when its sender
		gave it one.

		Disclosed while the message is still readable, so nobody is surprised
		by its removal — somebody who would rather not have this conversation
		on a timer can see that it is on one before they answer.
	*/
	Lifetime *lifetimeResponse `json:"lifetime,omitempty"`

	// ThreadRoot is the message whose thread this belongs to: a reply, or an
	// edit, deletion or update of one. Absent in the channel itself.
	ThreadRoot string `json:"threadRoot,omitempty"`
}

// lifetimeResponse is a message's expiry as a reader needs to understand it.
type lifetimeResponse struct {
	// Kind is "after_sending", "once_seen" or "after_seeing".
	Kind string `json:"kind"`
	// Seconds is the duration for the kinds that use one.
	Seconds int32 `json:"seconds,omitempty"`
	/*
		ExpiresAt is when it goes, absent while that is not yet known.

		A seen-based lifetime has no expiry until people have read the message,
		and a client showing a countdown from nothing would be inventing one.
		Absent means "when everybody has read it", which the kind already says.
	*/
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

func messageResponseOf(event dbgen.Event) messageResponse {
	response := messageResponse{
		ID:       event.ID,
		Type:     event.Type,
		Sender:   event.SenderAddress,
		Sequence: event.Sequence,
		Content:  json.RawMessage(event.Content),
		SentAt:   time.UnixMilli(event.CreatedAt),
		Redacted: event.RedactedAt != nil,
	}
	if event.ThreadRootID != nil {
		response.ThreadRoot = *event.ThreadRootID
	}
	return response
}

// withAttachments fills in a message's attachments.
//
// Looked up per message rather than joined into the timeline query: most
// messages carry none, and the common case should not pay for the rare one.
func (s *Server) withAttachments(ctx context.Context, message messageResponse) messageResponse {
	files, err := s.attachments.ForEvent(ctx, message.ID)
	if err != nil || len(files) == 0 {
		// A message whose attachments cannot be read is still a message. The
		// text is what matters most, and hiding it because a file is missing
		// would be the worse failure.
		return message
	}

	message.Attachments = make([]attachmentResponse, 0, len(files))
	for _, file := range files {
		message.Attachments = append(message.Attachments, attachmentResponseOf(file))
	}
	return message
}

type createRoomRequest struct {
	Slug       string `json:"slug"`
	Name       string `json:"name"`
	Topic      string `json:"topic"`
	Visibility string `json:"visibility"`
	/*
		MessageTTLSeconds keeps this channel's messages for a fixed period,
		overriding the server's own.

		Set at creation rather than changed afterwards, deliberately for now:
		shortening a period destroys content, and doing that to a channel
		people are already using needs the same recorded action the server-wide
		setting has. A channel created with a period discloses it from the
		start, so nobody says anything in it under a different expectation.
	*/
	MessageTTLSeconds *int32 `json:"messageTtlSeconds,omitempty"`
	/*
		Kind is "channel" or "disposable". Empty means a channel.

		A disposable chat is a room whose messages the server removes after a
		lifetime chosen when it is made. It is a kind rather than a flag
		because the distinction changes what the room is for, and a client
		listing rooms should be able to say so without inspecting a number.
	*/
	Kind string `json:"kind,omitempty"`
}

func (s *Server) handleListRooms(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	discoverable, err := s.rooms.Discoverable(ctx, user.ID)
	if err != nil {
		return err
	}

	// Which of these the requester is actually in, so the client can show the
	// difference between a room they read and one they may join.
	joined, err := s.rooms.RoomsUserIsIn(ctx, user.ID)
	if err != nil {
		return err
	}
	member := make(map[uuid.UUID]bool, len(joined))
	for _, room := range joined {
		member[room.ID] = true
	}

	response := make([]roomResponse, 0, len(discoverable))
	for _, room := range discoverable {
		response = append(response, s.roomResponse(room, member[room.ID]))
	}

	return c.JSON(http.StatusOK, response)
}

func (s *Server) handleCreateRoom(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	var request createRoomRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	// A server at its channel limit refuses before creating anything.
	if err := s.withinChannelLimit(ctx); err != nil {
		return err
	}

	/*
		A disposable chat must say how long its messages live.

		Without a lifetime it is an ordinary channel wearing a label, and every
		participant would have been shown a promise the room does not keep. The
		lifetime is disclosed before anybody posts precisely so that it means
		something.
	*/
	if request.Kind == "disposable" {
		if request.MessageTTLSeconds == nil || *request.MessageTTLSeconds <= 0 {
			return echo.NewHTTPError(http.StatusBadRequest,
				"a disposable chat needs to say how long its messages last")
		}
	}
	if request.Kind != "" && request.Kind != "channel" && request.Kind != "disposable" {
		return echo.NewHTTPError(http.StatusBadRequest, "that is not a kind of room")
	}

	room, err := s.rooms.Create(ctx, rooms.NewRoom{
		Kind:              request.Kind,
		Slug:              request.Slug,
		Name:              request.Name,
		Topic:             request.Topic,
		Visibility:        request.Visibility,
		CreatedBy:         user.ID,
		MessageTTLSeconds: request.MessageTTLSeconds,
	})
	if err != nil {
		return roomError(err)
	}

	return c.JSON(http.StatusCreated, s.roomResponse(room, true))
}

func (s *Server) handleGetRoom(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	room, err := s.rooms.Visible(ctx, roomID, user.ID)
	if err != nil {
		return roomError(err)
	}

	_, membershipErr := s.rooms.RequireMembership(ctx, roomID, user.ID)
	return c.JSON(http.StatusOK, s.roomResponse(room, membershipErr == nil))
}

func (s *Server) handleJoinRoom(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	// A ban keeps somebody out of the channel it names, and only that one:
	// every other membership they hold is untouched.
	if s.moderation != nil && s.moderation.IsBanned(ctx, roomID, user.ID) {
		return echo.NewHTTPError(http.StatusForbidden,
			"you are banned from this channel")
	}

	if err := s.rooms.Join(ctx, roomID, user.ID); err != nil {
		return roomError(err)
	}

	room, err := s.rooms.Visible(ctx, roomID, user.ID)
	if err != nil {
		return roomError(err)
	}

	s.notifyOfJoin(ctx, roomID, user)

	return c.JSON(http.StatusOK, s.roomResponse(room, true))
}

func (s *Server) handleLeaveRoom(c *echo.Context) error {
	user, _ := currentUser(c)

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	if err := s.rooms.Leave(c.Request().Context(), roomID, user.ID); err != nil {
		return roomError(err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (s *Server) handleRoomTimeline(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	timeline, err := s.rooms.Timeline(ctx, roomID, user.ID, 100)
	if err != nil {
		return roomError(err)
	}

	response, err := s.messagesFor(ctx, user.ID, timeline)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, response)
}

/*
messagesFor renders events as a reader sees them: with their files, and with
their lifetimes, so a reader can see what is on a timer before they answer it.
The channel's timeline and a thread are drawn by this one function, so a reply
can never come to show less than a message does.
*/
func (s *Server) messagesFor(ctx context.Context, readerID uuid.UUID, page []dbgen.Event) ([]messageResponse, error) {
	response := make([]messageResponse, 0, len(page))
	eventIDs := make([]string, 0, len(page))
	for _, event := range page {
		response = append(response, s.withAttachments(ctx, messageResponseOf(event)))
		if event.RedactedAt == nil {
			eventIDs = append(eventIDs, event.ID)
		}
	}

	// One query for the page rather than one per message.
	if s.moderation != nil && len(eventIDs) > 0 {
		lifetimes, err := s.moderation.LifetimesFor(ctx, eventIDs)
		if err != nil {
			return nil, err
		}
		for i, message := range response {
			if lifetime, ok := lifetimes[message.ID]; ok {
				resolved := lifetimeResponseOf(lifetime)
				response[i].Lifetime = &resolved
			}
		}

		/*
			Reading a message starts the clock on anything that expires after
			seeing.

			Detached from the request: recording that somebody looked must not
			be able to fail their reading of the conversation, and the sweep
			that acts on it runs minutes later regardless.
		*/
		s.inBackground(ctx, "record message views", func(ctx context.Context) {
			if err := s.moderation.RecordViews(ctx, readerID, eventIDs); err != nil {
				s.log.Error("cannot record message views", "error", err)
			}
		})
	}

	return response, nil
}

type sendMessageRequest struct {
	// AttachmentIDs are uploads to bind to this message. They were uploaded
	// beforehand, so a refusal here costs only the binding, not the transfer.
	AttachmentIDs []string `json:"attachmentIds,omitempty"`
	Body          string   `json:"body"`
	// ClientToken makes a retry safe: resending with the same token returns
	// the original message rather than posting twice.
	ClientToken string `json:"clientToken"`

	/*
		Buttons this message offers.

		Refused at this point rather than when somebody presses one: a message
		that reached a channel and then failed on being touched has already
		been read and already been trusted.
	*/
	Actions []actions.Action `json:"actions,omitempty"`
}

func (s *Server) handleSendMessage(c *echo.Context) error {
	user, _ := currentUser(c)

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	// A ban or a posting restriction has already stopped this, in
	// requirePosting, as it has every other way of posting.

	var request sendMessageRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	return s.postMessage(c, roomID, user, request, "")
}

/*
postMessage posts a message to a room, or a reply into one of its threads when
threadRoot names one.

One path for both, so a reply is checked exactly as a message is — its files,
its buttons, the content filter — and is followed by everything a message is.
A thread that skipped one of these would be the way around it.
*/
func (s *Server) postMessage(
	c *echo.Context,
	roomID uuid.UUID,
	user dbgen.User,
	request sendMessageRequest,
	threadRoot string,
) error {
	ctx := c.Request().Context()

	// Checked before the message is accepted, so a refusal does not leave a
	// message posted with its files missing.
	attachmentIDs, err := parseAttachmentIDs(request.AttachmentIDs)
	if err != nil {
		return err
	}
	if len(attachmentIDs) > 0 {
		if err := s.attachments.CheckBatch(ctx, attachmentIDs); err != nil {
			return attachmentError(err)
		}
	}

	/*
		The operator's content filter, applied before the message is accepted.

		Blocking returns here, so a refused message is never stored. Masking
		changes what is stored, which is why this runs before the message is
		written rather than after: a message stored and then edited would have
		existed unmasked, and anything watching the timeline would have seen it.
	*/
	filtered, err := s.applyContentFilter(ctx, request.Body)
	if err != nil {
		return err
	}

	var event dbgen.Event
	if threadRoot == "" {
		event, err = s.rooms.SendMessage(ctx, roomID, user.ID, filtered.Body, request.ClientToken, request.Actions...)
	} else {
		event, err = s.rooms.ReplyInThread(ctx, roomID, user.ID,
			threadRoot, filtered.Body, request.ClientToken, request.Actions...)
	}
	if err != nil {
		return roomError(err)
	}

	// Delivered first, then queued for review: a moderation queue that could
	// not accept an entry must not turn a successful send into an error.
	if filtered.Flagged {
		s.flagForReview(c, roomID, event.ID,
			s.rooms.UserAddress(user.Username), filtered.Matched, filtered.Body)
	}

	// Bound after the event exists, since the binding names it. Scoped to this
	// uploader and room, so one person cannot attach another's upload.
	if len(attachmentIDs) > 0 {
		if _, err := s.attachments.Bind(ctx, event.ID, roomID, user.ID, attachmentIDs); err != nil {
			return attachmentError(err)
		}
	}

	// Sending clears any typing indication, so it does not linger after the
	// message it announced has arrived.
	if err := s.presence.StopTyping(ctx, roomID, user.ID); err != nil {
		s.log.Error("cannot withdraw typing indication", "error", err)
	}

	s.afterMessage(ctx, event, request.Body)

	return c.JSON(http.StatusCreated, s.withAttachments(ctx, messageResponseOf(event)))
}

/*
afterMessage is what follows a message being stored, however it was sent — by
somebody now, or by the scheduler on their behalf.

body is what the sender wrote, before any masking, which is what search and
mentions are worked out from.
*/
func (s *Server) afterMessage(ctx context.Context, event dbgen.Event, body string) {
	// A reply follows its thread for whoever wrote it and whoever started
	// it, before anybody is notified: the thread's author must be among the
	// followers this very reply reaches.
	if event.ThreadRootID != nil {
		if err := s.rooms.TookPart(ctx, event); err != nil {
			s.log.Error("cannot record taking part in a thread", "eventId", event.ID, "error", err)
		}
	}

	// Neither of these blocks the response. The message is already stored,
	// which is the part that matters; making somebody wait for a search index
	// and a set of badges before their message appears is a cost paid on every
	// send for something nobody is watching.
	s.inBackground(ctx, "index", func(ctx context.Context) {
		s.indexForSearch(ctx, event, body)
	})
	s.inBackground(ctx, "notify", func(ctx context.Context) {
		s.notifyRecipients(ctx, event, body, event.RoomID)
	})
	// Answered on behalf of whoever it reached and is away; see
	// answerAutomatically for when that is.
	s.inBackground(ctx, "automatic replies", func(ctx context.Context) {
		s.answerAutomatically(ctx, event)
	})

	// Delivered after it is stored, so a client that misses the live frame
	// still finds the message on reconnection. This one stays on the request
	// path: it is an in-memory fan-out, and doing it later would mean the
	// sender's own client sees its message arrive after the response.
	s.publishEvent(ctx, event)
}

type editMessageRequest struct {
	Body string `json:"body"`
}

func (s *Server) handleEditMessage(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	var request editMessageRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	// Edits are filtered too. Without this the filter is trivially walked
	// around: send something harmless, then edit it into anything.
	filtered, err := s.applyContentFilter(ctx, request.Body)
	if err != nil {
		return err
	}

	// Editing somebody else's message needs the moderation permission, in this
	// channel; the service enforces authorship when it is absent.
	moderator := currentPermissions(c).AllowsChannel(authz.PermMessageEditAny, roomID).Allowed

	event, err := s.rooms.EditMessageAs(ctx, roomID, user.ID, eventIDParam(c), filtered.Body, moderator)
	if err != nil {
		return roomError(err)
	}

	if filtered.Flagged {
		s.flagForReview(c, roomID, event.ID,
			s.rooms.UserAddress(user.Username), filtered.Matched, filtered.Body)
	}

	/*
		Recorded when it was a moderator's doing, as a deletion is, and for the
		same reason: changing what somebody is shown to have said is an exercise
		of authority, and correcting your own typo is not. Neither text goes in
		the entry — the log is read by people the conversation was not for.
		Read from the event the service wrote, which knows whose message it was.
	*/
	var edit struct {
		Replaces    string `json:"replaces"`
		ByModerator bool   `json:"byModerator"`
	}
	_ = json.Unmarshal(event.Content, &edit)
	if edit.ByModerator {
		s.recordModeration(c, user, audit.ActionMessageEdited, "event", edit.Replaces,
			map[string]any{"room": roomID.String()})
	}

	s.publishEvent(ctx, event)
	return c.JSON(http.StatusCreated, messageResponseOf(event))
}

func (s *Server) handleDeleteMessage(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	// Deleting someone else's message needs the moderation permission; the
	// service enforces authorship when it is absent.
	moderator := currentPermissions(c).AllowsChannel(authz.PermMessageDeleteAny, roomID).Allowed

	event, err := s.rooms.DeleteMessage(ctx, roomID, user.ID, eventIDParam(c), moderator)

	/*
		A redaction on a replica removes the content and writes no event,
		because this server does not number events in a room it does not own.

		It is a success, and the act most worth recording: somebody here
		decided, unilaterally and without the origin's agreement, that this
		server would stop carrying something. The origin keeps its copy, so
		the only account of what happened is the one kept here.
	*/
	if errors.Is(err, rooms.ErrRedactedLocally) {
		if moderator {
			s.recordModeration(c, user, audit.ActionMessageRemoved, "event", eventIDParam(c), nil)
		}
		if err := s.attachments.DeleteForEvent(ctx, eventIDParam(c)); err != nil {
			s.log.Error("cannot remove a locally redacted message's attachments",
				"event", eventIDParam(c), "error", err)
		}
		return c.NoContent(http.StatusNoContent)
	}

	if err != nil {
		return roomError(err)
	}

	// The files go with the message. Leaving them reachable would mean a
	// deletion removed the words and kept the photograph, which is not what
	// anyone deleting a message expects.
	//
	// Logged rather than returned: the message is already deleted, and failing
	// the request now would suggest to the caller that it was not.
	if err := s.attachments.DeleteForEvent(ctx, eventIDParam(c)); err != nil {
		s.log.Error("cannot remove a deleted message's attachments",
			"eventId", eventIDParam(c), "error", err)
	}

	// A deleted message that stayed searchable would be a deletion that did
	// not delete: the placeholder says the content is gone, and search has to
	// agree.
	if s.search != nil {
		if err := s.search.Unindex(ctx, eventIDParam(c)); err != nil {
			s.log.Error("cannot remove a deleted message from the search index",
				"eventId", eventIDParam(c), "error", err)
		}
	}

	/*
		Recorded when it was a moderator's doing, not when somebody removed
		their own message.

		Deleting what you wrote is ordinary use of the application; recording
		every one would bury the deletions that were an exercise of authority,
		which are the ones anybody reading the log is looking for. The author
		is recorded rather than the text — the whole point of a deletion is
		that the text is gone, and an audit entry carrying it would put it back.
	*/
	/*
		byModerator comes from the deletion event, which the service has
		already decided.

		Not re-derived here: the event returned is the deletion, whose sender
		is whoever performed it — so comparing its sender to the actor is
		always true and would record nothing. The service knows whether the
		target was theirs, and says so in the event it wrote.
	*/
	var deletion struct {
		Redacts     string `json:"redacts"`
		ByModerator bool   `json:"byModerator"`
	}
	_ = json.Unmarshal(event.Content, &deletion)

	if deletion.ByModerator {
		s.recordModeration(c, user, audit.ActionMessageRemoved, "event", deletion.Redacts, nil)
	}

	s.publishEvent(ctx, event)
	return c.JSON(http.StatusCreated, messageResponseOf(event))
}

/*
memberResponse is one person in a member list.

The name and address are always present: they are how a message is attributed
and a conversation is opened, and a list without them is not a member list.

Everything else is omitted when the person has withheld it — omitted, not sent
as false or empty, because a detail sent is a detail disclosed however the
client behaves. omitempty on a pointer is what makes the field absent from the
JSON rather than present and null.
*/
type memberResponse struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"displayName"`
	Address     string    `json:"address"`
	JoinedAt    time.Time `json:"joinedAt"`

	Online     *bool      `json:"online,omitempty"`
	Status     *string    `json:"status,omitempty"`
	StatusText *string    `json:"statusText,omitempty"`
	LastSeenAt *time.Time `json:"lastSeenAt,omitempty"`

	/*
		Withheld names the details this person does not disclose, and is sent
		only to a moderator who is being shown them anyway.

		It exists so a moderator can see that they are looking at something the
		person chose to hide, rather than reading it as ordinary information.
		Sending it to everybody would defeat the setting: "this field is
		withheld" answers the question the person declined to answer.
	*/
	Withheld []string `json:"withheld,omitempty"`
}

func (s *Server) handleRoomMembers(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	/*
		The moderation context is asked for, not assumed.

		Without the query parameter a moderator sees exactly what anybody else
		sees. Opening a member list is not moderating, and a moderator who never
		asked to look past somebody's settings should not have done so — nor
		collected an audit entry saying they did.
	*/
	asModerator := c.QueryParam("moderating") == "true" && s.mayModerate(ctx, user)

	members, err := s.rooms.MembersWithDisclosure(ctx, roomID, user.ID, asModerator)
	if err != nil {
		return roomError(err)
	}

	response := make([]memberResponse, 0, len(members))
	disclosed := make([]string, 0)
	for _, member := range members {
		entry := memberResponse{
			ID:          member.ID.String(),
			Username:    member.Username,
			DisplayName: member.DisplayName,
			Address:     member.Username + "@" + s.serverName(),
			JoinedAt:    member.JoinedAt,
		}

		// Each detail is attached only when it is not withheld. The query has
		// already refused to read a withheld one, so what these guards prevent
		// is sending its zero value as though it were an answer.
		if !member.WithholdsPresence {
			online := member.Online
			entry.Online = &online
		}
		if !member.WithholdsStatus {
			status, statusText := member.Status, member.StatusText
			entry.Status, entry.StatusText = &status, &statusText
		}
		// The query returns the Unix epoch for "no answer" — withheld, or a
		// person who has never connected. Anything at or before it is not a
		// time somebody was last seen.
		if !member.WithholdsLastSeen && member.LastSeenAt.After(time.Unix(0, 0)) {
			lastSeen := member.LastSeenAt
			entry.LastSeenAt = &lastSeen
		}

		if asModerator {
			entry.Withheld = withheldNames(member)
			if len(entry.Withheld) > 0 {
				disclosed = append(disclosed, member.Username)
			}
		}

		response = append(response, entry)
	}

	/*
		Recorded once for the request rather than once per person, and only when
		something withheld was actually shown. A moderator who opened a list
		where everybody discloses everything learned nothing an ordinary member
		could not have, and an entry for that would bury the ones that matter.
	*/
	if len(disclosed) > 0 {
		s.recordModeration(c, user, audit.ActionWithheldDetailsViewed,
			"room", roomID.String(), map[string]any{"people": disclosed})
	}

	return c.JSON(http.StatusOK, response)
}

// withheldNames lists the details this person does not disclose.
func withheldNames(member dbgen.ListRoomMembersWithDisclosureRow) []string {
	withheld := make([]string, 0, 3)
	if member.WithholdsPresence {
		withheld = append(withheld, "presence")
	}
	if member.WithholdsStatus {
		withheld = append(withheld, "status")
	}
	if member.WithholdsLastSeen {
		withheld = append(withheld, "lastSeen")
	}
	return withheld
}

/*
mayModerate reports whether somebody holds the moderation permission.

Separate from the route middleware because this is not a moderation route: the
member list is for everybody, and moderating is a thing one caller may
additionally be doing. A server with no authz configured moderates nobody.
*/
func (s *Server) mayModerate(ctx context.Context, actor dbgen.User) bool {
	if s.authz == nil {
		return false
	}
	permissions, err := s.authz.PermissionsFor(ctx, actor.ID)
	if err != nil {
		return false
	}
	return permissions.AllowsServer(authz.PermModerationReview).Allowed
}

// roomError maps a rooms failure to a response.
//
// A room the requester may not see is reported as missing, identically to one
// that does not exist, so its existence cannot be probed.
func roomError(err error) error {
	switch {
	case errors.Is(err, rooms.ErrRoomNotFound):
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	case errors.Is(err, rooms.ErrNotAMember):
		return echo.NewHTTPError(http.StatusForbidden, "you are not a member of this room")
	case errors.Is(err, rooms.ErrArchived):
		return echo.NewHTTPError(http.StatusConflict, "this room is archived")
	case errors.Is(err, rooms.ErrNoSuchAction):
		// Missing rather than a bad request: a press naming something the
		// message does not offer is a press of a button that is not there.
		return echo.NewHTTPError(http.StatusNotFound, err.Error())
	case errors.Is(err, rooms.ErrAlreadyPressed):
		// Conflict, because the request is well formed and the state refuses
		// it — which is also what a client needs to tell "try again" from
		// "this is settled".
		return echo.NewHTTPError(http.StatusConflict, err.Error())
	case errors.Is(err, rooms.ErrActionExpired):
		return echo.NewHTTPError(http.StatusGone, err.Error())
	case errors.Is(err, rooms.ErrNotTheAuthor):
		return echo.NewHTTPError(http.StatusForbidden, "only the author may change this message")
	case errors.Is(err, rooms.ErrSlugTaken):
		return echo.NewHTTPError(http.StatusConflict, "that room address is already taken")
	case errors.Is(err, rooms.ErrInvalidSlug),
		errors.Is(err, rooms.ErrEmptyMessage),
		errors.Is(err, rooms.ErrMessageTooLong),
		/*
			A message whose buttons are not acceptable.

			Reported with the reason rather than as a bare refusal: the caller
			is a program, and "that is not something a client will do:
			\"execute\"" is the difference between a fix and an afternoon.
		*/
		errors.Is(err, actions.ErrTooMany),
		errors.Is(err, actions.ErrLabelRequired),
		errors.Is(err, actions.ErrLabelTooLong),
		errors.Is(err, actions.ErrNameRequired),
		errors.Is(err, actions.ErrDuplicateName),
		errors.Is(err, actions.ErrUnknownCapability),
		errors.Is(err, actions.ErrURLRequired),
		errors.Is(err, actions.ErrURLNotAllowed),
		// Menus and fields, whose refusals all wrap this one.
		errors.Is(err, actions.ErrInvalidElement):
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	default:
		return err
	}
}

// indexForSearch records a message so it can be found later.
//
// Failures are logged rather than returned. Somebody whose message did not
// send because a search index was unavailable has lost something real;
// somebody whose message is briefly unsearchable has not.
func (s *Server) indexForSearch(ctx context.Context, event dbgen.Event, body string) {
	if s.search == nil {
		return
	}

	if err := s.search.Index(ctx, event, body); err != nil {
		s.log.Error("cannot index a message for search", "eventId", event.ID, "error", err)
	}
}

// notifyRecipients tells the room's members about a message.
//
// After the message is stored and outside its transaction, so a notification
// that cannot be written costs a notification rather than the message. Somebody
// whose message failed to send because a badge could not be updated has lost
// something real.
func (s *Server) notifyRecipients(
	ctx context.Context,
	event dbgen.Event,
	body string,
	roomID uuid.UUID,
) {
	if s.notify == nil {
		return
	}

	// A room belonging to something else may change who is in it before the
	// list below is read; see beforeNotifying.
	for _, hook := range beforeNotifying {
		hook(s, ctx, event, body, roomID)
	}

	members, err := s.queries.ListRoomMembers(ctx, roomID)
	if err != nil {
		s.log.Error("cannot read a room's members to notify them",
			"roomId", roomID, "error", err)
		return
	}

	room, err := s.queries.GetRoomByID(ctx, roomID)
	if err != nil {
		s.log.Error("cannot read a room to notify its members",
			"roomId", roomID, "error", err)
		return
	}

	/*
		What a notification says, for the devices that are not open.

		Names rather than identifiers, because a phone shows this to whoever is
		holding it. Read here because this is where the room is already in
		hand; how much of it is disclosed is decided further down, against the
		recipient's own setting.
	*/
	about := notify.Delivery{
		Room: room.Name,
		// A private channel is one whose contents are not already public on
		// this server. Direct conversations are added to this by the notifier,
		// which is told about them separately.
		Private: room.Visibility != "public",
	}
	if event.SenderID != nil {
		if sender, err := s.queries.GetUserByID(ctx, *event.SenderID); err == nil {
			about.Sender = sender.DisplayName
		}
	}

	// A direct conversation notifies whoever is in it regardless of their
	// level: somebody set to "mentions" should still hear from a person
	// speaking to them privately.
	direct := room.Kind == "direct"
	if event.ThreadRootID == nil {
		s.notify.ForMessageIn(ctx, event, body, members, direct, about)
		return
	}

	followers, err := s.rooms.ThreadFollowers(ctx, *event.ThreadRootID)
	if err != nil {
		s.log.Error("cannot read a thread's followers", "eventId", event.ID, "error", err)
	}
	s.notify.ForReplyIn(ctx, event, body, members, direct, about, followers)
}

/*
notifyOfJoin tells a room's members that somebody has joined.

Only the people who asked to hear everything about the conversation, which the
notification service decides: on a busy channel where people come and go, this
would otherwise be a stream of notifications about nothing anybody said.

The join event is read back rather than returned from Join, so that adding this
did not change a signature four other callers and their tests depend on.
*/
func (s *Server) notifyOfJoin(ctx context.Context, roomID uuid.UUID, user dbgen.User) {
	if s.notify == nil {
		return
	}

	// The newest event in the room is the join that was just appended. A
	// message arriving in the same instant would make this point at the wrong
	// event, which costs a notification that names the right person and links
	// to a neighbouring message — not worth a transaction to prevent.
	timeline, err := s.rooms.Timeline(ctx, roomID, user.ID, 1)
	if err != nil || len(timeline) == 0 {
		return
	}

	members, err := s.queries.ListRoomMembers(ctx, roomID)
	if err != nil {
		return
	}

	s.notify.AboutRoom(ctx, members, user.ID, s.rooms.UserAddress(user.Username),
		roomID, timeline[len(timeline)-1].ID, notify.ReasonJoin)
}

/*
retentionDefault is the server-wide retention period in seconds, or zero for
none.

Zero when the setting cannot be read, which keeps content rather than removing
it. Failing towards deletion because a settings query timed out would be
unrecoverable in a way failing towards keeping never is.
*/
func (s *Server) retentionDefault() int32 {
	if s.settings == nil {
		return 0
	}
	return int32(s.settings.Int(context.Background(), settings.RetentionSeconds))
}

/*
handlePressAction records somebody pressing a button on a message.

The press becomes an event in the room, which is how it reaches the program
that posted the message: over the live connection it already holds. Menus,
fields, versions and restrictions are handled where the rest of interactive
messages are, in integrations_interactive.go.
*/
func (s *Server) handlePressAction(c *echo.Context) error {
	return s.pressInteractive(c)
}
