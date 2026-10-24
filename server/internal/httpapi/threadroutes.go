package httpapi

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/identity"
)

// threadResponse is a thread's root and its replies, with where the reader
// stands in it.
type threadResponse struct {
	Root messageResponse `json:"root"`
	// Replies is every event in the thread, oldest first: the replies, and
	// the edits, deletions and updates of them, folded by the client as the
	// channel's timeline is.
	Replies []messageResponse `json:"replies"`
	// ReadSequence is how far the reader has read this thread, zero if never.
	ReadSequence int64 `json:"readSequence"`
	// Following is whether replies here notify the reader.
	Following bool `json:"following"`
}

func (s *Server) handleThread(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	root, replies, err := s.rooms.Thread(ctx, roomID, user.ID, eventIDParam(c))
	if err != nil {
		return roomError(err)
	}

	// The root and its replies, drawn as the timeline draws messages.
	rendered, err := s.messagesFor(ctx, user.ID, append([]dbgen.Event{root}, replies...))
	if err != nil {
		return err
	}

	reading, err := s.rooms.ThreadReadingFor(ctx, user.ID, root.ID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, threadResponse{
		Root:         rendered[0],
		Replies:      rendered[1:],
		ReadSequence: reading.ReadSequence,
		Following:    reading.Following,
	})
}

/*
handleReplyInThread posts a reply under a message.

A reply is a message, so it takes the same request and goes through the same
path as posting to the channel: its files, its buttons, the content filter and
everything that follows a message.
*/
func (s *Server) handleReplyInThread(c *echo.Context) error {
	user, _ := currentUser(c)

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	var request sendMessageRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	return s.postMessage(c, roomID, user, request, eventIDParam(c))
}

// handleMarkThreadRead records how far the reader has read one thread.
func (s *Server) handleMarkThreadRead(c *echo.Context) error {
	user, _ := currentUser(c)

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	var request markReadRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	if err := s.rooms.MarkThreadRead(c.Request().Context(), roomID, user.ID,
		eventIDParam(c), request.Sequence); err != nil {
		return roomError(err)
	}
	return c.NoContent(http.StatusNoContent)
}

// handleFollowThread and handleUnfollowThread are the reader's own choice,
// which replying afterwards does not override.
func (s *Server) handleFollowThread(c *echo.Context) error {
	return s.setFollowing(c, true)
}

func (s *Server) handleUnfollowThread(c *echo.Context) error {
	return s.setFollowing(c, false)
}

func (s *Server) setFollowing(c *echo.Context, following bool) error {
	user, _ := currentUser(c)

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	if err := s.rooms.SetThreadFollowing(c.Request().Context(), roomID, user.ID,
		eventIDParam(c), following); err != nil {
		return roomError(err)
	}
	return c.NoContent(http.StatusNoContent)
}

type openDirectRequest struct {
	// UserIDs of the other participants. The caller is included
	// automatically, so a client cannot open a conversation it is not in.
	UserIDs []string `json:"userIds"`
	// Usernames name participants where a client has a name and not an id,
	// as on a directory profile.
	Usernames []string `json:"usernames"`
}

func (s *Server) handleOpenDirect(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	var request openDirectRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	participants := []uuid.UUID{user.ID}
	for _, raw := range request.UserIDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "not a valid user identifier: "+raw)
		}
		// Confirm the person exists before opening a conversation with them,
		// so a typo does not create a conversation with nobody.
		if _, err := s.identity.UserByID(ctx, id); err != nil {
			return echo.NewHTTPError(http.StatusNotFound, "no such person")
		}
		participants = append(participants, id)
	}
	for _, name := range request.Usernames {
		other, err := s.identity.UserByUsername(ctx, name)
		if err != nil {
			return echo.NewHTTPError(http.StatusNotFound, "no such person")
		}
		if other.ID != user.ID {
			participants = append(participants, other.ID)
		}
	}

	if len(participants) < 2 {
		return echo.NewHTTPError(http.StatusBadRequest,
			"a direct conversation needs at least one other person")
	}

	/*
		Whether each person accepts a conversation from this one.

		The refusal says only that it cannot be opened. It must not say which
		rule applied: "they have blocked you" and "they accept messages only
		from colleagues" are different disclosures, and the first is one the
		blocked person must never receive.

		A moderator is exempt, so that somebody cannot put themselves beyond
		contact by the people responsible for the server.
	*/
	if s.moderation != nil && !s.mayContactAll(ctx, user, participants) {
		return echo.NewHTTPError(http.StatusForbidden,
			"that conversation cannot be opened")
	}

	room, err := s.rooms.OpenDirect(ctx, participants)
	if err != nil {
		return roomError(err)
	}

	return c.JSON(http.StatusOK, s.roomResponse(room, true))
}

func (s *Server) handleListDirect(c *echo.Context) error {
	user, _ := currentUser(c)

	direct, err := s.rooms.DirectRooms(c.Request().Context(), user.ID)
	if err != nil {
		return err
	}

	response := make([]roomResponse, 0, len(direct))
	for _, room := range direct {
		response = append(response, s.roomResponse(room, true))
	}
	return c.JSON(http.StatusOK, response)
}

type markReadRequest struct {
	Sequence int64 `json:"sequence"`
}

func (s *Server) handleMarkRead(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	var request markReadRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	if _, err := s.rooms.RequireMembership(ctx, roomID, user.ID); err != nil {
		return roomError(err)
	}
	if err := s.rooms.MarkRead(ctx, roomID, user.ID, request.Sequence); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}

func (s *Server) handleUnreadCounts(c *echo.Context) error {
	user, _ := currentUser(c)

	counts, err := s.rooms.UnreadCounts(c.Request().Context(), user.ID)
	if err != nil {
		return err
	}

	response := make(map[string]int64, len(counts))
	for roomID, count := range counts {
		response[roomID.String()] = count
	}
	return c.JSON(http.StatusOK, response)
}

/*
presenceResponse is what one person's presence looks like to another.

Each detail is optional because each is somebody's to withhold, and a withheld
detail is left out rather than sent empty: "offline" or a zero time is an
answer to the question they declined to answer.
*/
type presenceResponse struct {
	UserID     string     `json:"userId"`
	State      string     `json:"state,omitempty"`
	Status     string     `json:"status,omitempty"`
	StatusText string     `json:"statusText,omitempty"`
	LastSeenAt *time.Time `json:"lastSeenAt,omitempty"`
}

func (s *Server) handleRoomPresence(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	// Membership is checked first: who is present in a room is not something
	// an outsider may learn.
	members, err := s.rooms.Members(ctx, roomID, user.ID)
	if err != nil {
		return roomError(err)
	}

	ids := make([]uuid.UUID, 0, len(members))
	for _, member := range members {
		ids = append(ids, member.ID)
	}

	states, err := s.presence.ForMany(ctx, ids)
	if err != nil {
		return err
	}

	/*
		What each person withholds, by the rule the member list follows.

		Read from the same query rather than decided again here, so the two
		answers to "is this person online" cannot come to disagree. Never in the
		moderation context: this is what a conversation polls, not a report
		being reviewed.
	*/
	disclosure, err := s.rooms.MembersWithDisclosure(ctx, roomID, user.ID, false, int16(viewerRankOf(user)))
	if err != nil {
		return roomError(err)
	}
	withholds := make(map[uuid.UUID]dbgen.ListRoomMembersWithDisclosureRow, len(disclosure))
	for _, member := range disclosure {
		withholds[member.ID] = member
	}

	response := make([]presenceResponse, 0, len(members))
	for _, member := range members {
		state := states[member.ID]
		entry := presenceResponse{UserID: member.ID.String()}

		// Somebody's own presence is theirs to see whatever they withhold.
		chosen, known := withholds[member.ID]
		own := member.ID == user.ID
		if !known && !own {
			// Not in the disclosure list, so nothing is known to be allowed.
			response = append(response, entry)
			continue
		}

		if own || !chosen.WithholdsPresence {
			entry.State = string(state.State)
		}
		if own || !chosen.WithholdsStatus {
			entry.Status = state.Status
			entry.StatusText = state.StatusText
		}
		if own || !chosen.WithholdsLastSeen {
			lastSeen := state.LastSeenAt
			entry.LastSeenAt = &lastSeen
		}
		response = append(response, entry)
	}

	return c.JSON(http.StatusOK, response)
}

type setStatusRequest struct {
	Status string `json:"status"`
	Text   string `json:"text"`
	// ExpiresInSeconds, when set, makes the status lapse on its own.
	ExpiresInSeconds int64 `json:"expiresInSeconds"`
	Invisible        *bool `json:"invisible"`
}

func (s *Server) handleSetStatus(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	var request setStatusRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	if request.Status != "" {
		var expiresAt *time.Time
		if request.ExpiresInSeconds > 0 {
			at := time.Now().Add(time.Duration(request.ExpiresInSeconds) * time.Second)
			expiresAt = &at
		}
		if err := s.presence.SetStatus(ctx, user.ID, request.Status, request.Text, expiresAt); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
	}

	if request.Invisible != nil {
		if err := s.presence.SetInvisible(ctx, user.ID, *request.Invisible); err != nil {
			return err
		}
	}

	return s.respondWithOwnStatus(c, user.ID)
}

/*
ownStatusResponse is somebody's status as they set it.

Not presenceResponse, which is what others see and so withholds the status of
somebody appearing offline. The person who chose both is shown both, with when
the status lapses, so a client can say "until 15:00" and offer to change it.
*/
type ownStatusResponse struct {
	Status     string `json:"status"`
	StatusText string `json:"statusText"`
	// ExpiresAt is when the status reverts on its own, absent when it does not.
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	// Invisible is whether this person appears offline while connected.
	Invisible bool `json:"invisible"`
	// AutomaticReply is whether one of their automatic replies answers now.
	AutomaticReply bool `json:"automaticReply"`
}

// handleGetStatus returns the signed-in person's own status.
func (s *Server) handleGetStatus(c *echo.Context) error {
	user, _ := currentUser(c)
	return s.respondWithOwnStatus(c, user.ID)
}

func (s *Server) respondWithOwnStatus(c *echo.Context, userID uuid.UUID) error {
	own, err := s.presence.Own(c.Request().Context(), userID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, ownStatusResponse{
		Status:     own.Status,
		StatusText: own.StatusText,
		ExpiresAt:  own.ExpiresAt,
		Invisible:  own.Invisible,
		AutomaticReply: s.automaticReplyActive(c.Request().Context(), userID,
			ownStatusForRules{status: own.Status, invisible: own.Invisible}),
	})
}

func (s *Server) handleListStatuses(c *echo.Context) error {
	statuses, err := s.presence.Statuses(c.Request().Context())
	if err != nil {
		return err
	}

	response := make([]map[string]any, 0, len(statuses))
	for _, status := range statuses {
		response = append(response, map[string]any{
			"key":                     status.Key,
			"label":                   status.Label,
			"suppressesNotifications": status.SuppressesNotifications,
		})
	}
	return c.JSON(http.StatusOK, response)
}

// startTypingRequest is optional: an empty body is typing in the channel.
type startTypingRequest struct {
	// ThreadRoot names the thread being replied in.
	ThreadRoot string `json:"threadRoot,omitempty"`
}

func (s *Server) handleStartTyping(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	var request startTypingRequest
	if c.Request().ContentLength != 0 {
		if err := c.Bind(&request); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
		}
	}

	if _, err := s.rooms.RequireMembership(ctx, roomID, user.ID); err != nil {
		return roomError(err)
	}
	// Only a message in this room, so the thread a writer is shown in is one
	// the readers of the room can open.
	if request.ThreadRoot != "" {
		if _, err := s.rooms.EventInRoom(ctx, roomID, request.ThreadRoot); err != nil {
			return roomError(err)
		}
	}
	if err := s.presence.StartTypingIn(ctx, roomID, user.ID, request.ThreadRoot); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}

func (s *Server) handleListTyping(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	if _, err := s.rooms.RequireMembership(ctx, roomID, user.ID); err != nil {
		return roomError(err)
	}

	typing, err := s.presence.Typing(ctx, roomID, user.ID)
	if err != nil {
		return err
	}

	response := make([]map[string]string, 0, len(typing))
	for _, row := range typing {
		entry := map[string]string{
			"userId":      row.UserID.String(),
			"displayName": identity.NameOf(row.Username, row.DisplayName),
		}
		// Absent for the channel itself, so each view shows its own writers.
		if row.ThreadRootID != nil {
			entry["threadRoot"] = *row.ThreadRootID
		}
		response = append(response, entry)
	}
	return c.JSON(http.StatusOK, response)
}

// threadSummary is how many replies one message has.
type threadSummary struct {
	ReplyCount  int64 `json:"replyCount"`
	LastReplyAt int64 `json:"lastReplyAt"`
	// Unread is replies by others the reader has not read, in a thread they
	// follow; zero in one they do not.
	Unread int64 `json:"unread"`
}

/*
handleThreadSummaries returns reply counts for a room's timeline in one call.

A count per message is what a timeline needs to render, and asking for each one
separately meant a room with fifty messages made fifty requests to draw itself.
The batch query already existed; only the way in was missing.

Scoped to the room and its membership, so this cannot be used to count replies
in a conversation the caller cannot read.
*/
func (s *Server) handleThreadSummaries(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	// The same check the single-thread endpoint makes. Counts disclose that a
	// conversation is active, so they are not more public than its messages.
	if _, err := s.rooms.Visible(ctx, roomID, user.ID); err != nil {
		return roomError(err)
	}

	summaries, err := s.rooms.RoomThreadSummaries(ctx, roomID, user.ID)
	if err != nil {
		return err
	}

	response := make(map[string]threadSummary, len(summaries))
	for rootID, summary := range summaries {
		response[rootID] = threadSummary{
			ReplyCount:  summary.ReplyCount,
			LastReplyAt: summary.LastReplyAt,
			Unread:      summary.Unread,
		}
	}

	return c.JSON(http.StatusOK, response)
}
