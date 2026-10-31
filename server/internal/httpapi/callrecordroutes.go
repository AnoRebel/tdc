package httpapi

import (
	"context"
	"errors"
	"net/http"
	"uuid"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/rooms"
	"github.com/AnoRebel/tdc/server/internal/settings"
)

/*
Recording a call.

The recording itself is made on the recorder's device, from what it receives,
and saved to the conversation through the ordinary upload path; no call media
reaches the server for it. What the server does is decide whether somebody may
record — the operator's calls.recording and the person's call.record, both
needed — and make sure everybody knows: the call's state says who is recording,
and the start is in the conversation's log and the audit log, all before the
recorder's client is told it may begin.
*/
func init() {
	registerRoutes(func(s *Server) {
		if s.rooms == nil {
			return
		}
		recording := s.echo.Group("/api/rooms/:id/call/recording", requireAuthentication, s.requireFeature(settings.FeatureCalls))
		recording.PUT("", s.handleStartRecording)
		recording.DELETE("", s.handleStopRecording)
	})

	documentRoutes(map[string]entry{
		"PUT /api/rooms/:id/call/recording": {
			Summary:        "Begin recording the call, everybody in it told first",
			Authentication: "session or token", Permission: "call.record",
		},
		"DELETE /api/rooms/:id/call/recording": {Summary: "Stop recording the call", Authentication: "session or token"},
	})
}

/*
handleStartRecording marks the caller as recording the call they are in.

In this order, each before the next: the call's state says they are recording,
the start is in the conversation's log and the audit log, and everybody in the
call is sent the new state. Only then does the answer go back, and only on that
answer does the client start recording — so a recording cannot begin that the
others have not been told of. If the audit log cannot take it, the recording is
stopped again and refused.
*/
func (s *Server) handleStartRecording(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}
	if s.settings.Get(ctx, settings.CallsRecording) != "true" {
		return echo.NewHTTPError(http.StatusForbidden, "recording calls is not enabled on this server")
	}
	if decision := currentPermissions(c).AllowsChannel(authz.PermCallRecord, roomID); !decision.Allowed {
		return echo.NewHTTPError(http.StatusForbidden, decision.Error())
	}
	call, err := s.callForPresenting(c, roomID)
	if err != nil {
		return err
	}
	started, err := s.rooms.StartRecording(ctx, call.ID, user.ID)
	if err != nil {
		if errors.Is(err, rooms.ErrNotAMember) {
			return echo.NewHTTPError(http.StatusForbidden, "only a member of the conversation can record its call")
		}
		return callError(err)
	}
	if started != nil {
		if err := s.recordRecordingAudit(ctx, c, audit.ActionCallRecordingStarted, user.ID, user.Username, call.ID, ""); err != nil {
			s.stopRecordingOf(context.WithoutCancel(ctx), call.ID, user.ID, "")
			return echo.NewHTTPError(http.StatusServiceUnavailable, "the recording could not be recorded in the audit log, so it was not started")
		}
		s.publishEvent(ctx, *started)
	}
	return s.respondWithCallState(c, roomID, call)
}

func (s *Server) handleStopRecording(c *echo.Context) error {
	ctx := context.WithoutCancel(c.Request().Context())
	user, _ := currentUser(c)
	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}
	call, err := s.rooms.OpenCallIn(ctx, roomID)
	if err != nil {
		return callError(err)
	}
	s.stopRecordingOf(ctx, call.ID, user.ID, "")
	return s.respondWithCallState(c, roomID, call)
}

/*
stopRecordingOf stops somebody's recording of a call, if they are recording,
and notes it in the conversation and the audit log. The reason is empty when
they stopped it themselves.
*/
func (s *Server) stopRecordingOf(ctx context.Context, callID, userID uuid.UUID, reason string) {
	stopped, err := s.rooms.StopRecording(ctx, callID, userID)
	if err != nil {
		s.log.Warn("could not stop a recording", "error", err)
		return
	}
	if stopped == nil {
		return
	}
	s.publishEvent(ctx, *stopped)
	username := ""
	if person, err := s.identity.UserByID(ctx, userID); err == nil {
		username = person.Username
	}
	if err := s.recordRecordingAudit(ctx, nil, audit.ActionCallRecordingStopped, userID, username, callID, reason); err != nil {
		s.log.Warn("could not record a stopped recording in the audit log", "error", err)
	}
}

/*
stopAbsentRecordings stops the recordings of people who are no longer in their
call — they left, their connection dropped, or the call ended — so a recording
never outlives its recorder's being there. Called wherever somebody leaves and
from the sweep that notices dropped connections.
*/
func (s *Server) stopAbsentRecordings(ctx context.Context) {
	stopped, err := s.rooms.StopAbsentRecordings(ctx)
	if err != nil {
		s.log.Warn("could not stop recordings of people who left", "error", err)
	}
	for _, each := range stopped {
		s.publishEvent(ctx, each.Event)
		username := ""
		if person, err := s.identity.UserByID(ctx, each.UserID); err == nil {
			username = person.Username
		}
		if err := s.recordRecordingAudit(ctx, nil, audit.ActionCallRecordingStopped, each.UserID, username, each.CallID, "left the call"); err != nil {
			s.log.Warn("could not record a stopped recording in the audit log", "error", err)
		}
	}
}

func (s *Server) recordRecordingAudit(
	ctx context.Context, c *echo.Context, action audit.Action,
	actor uuid.UUID, label string, callID uuid.UUID, reason string,
) error {
	if s.audit == nil {
		return nil
	}
	entry := audit.Entry{
		ActorID: &actor, ActorLabel: label, Action: action,
		TargetKind: "call", TargetID: callID.String(),
	}
	if c != nil {
		entry.SourceIP = requestIP(c)
		entry.UserAgent = c.Request().UserAgent()
	}
	if reason != "" {
		entry.Detail = map[string]any{"reason": reason}
	}
	return s.audit.Record(ctx, entry)
}
