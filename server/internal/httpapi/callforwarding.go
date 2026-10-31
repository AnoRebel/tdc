package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
	"uuid"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/realtime"
	"github.com/AnoRebel/tdc/server/internal/rooms"
	"github.com/AnoRebel/tdc/server/internal/settings"
)

/*
Calls past the mesh limit, through the forwarding unit.

A call starts as a mesh and moves to the unit when a join takes it past
calls.mesh_limit on a server with calls.sfu on — at that join, while it is in
progress, rather than deciding at the start, since a call starts with one person
and nobody knows how large it will grow. Every participant is told by the
call's state (its mode becomes "sfu"); each client then closes its connections
to the others and makes one to the unit. Once forwarded a call stays forwarded
until it ends: moving back as people leave would interrupt everybody twice for
the sake of the server's bandwidth.

On several nodes a forwarded call's media must meet in one process, so the call
records the node that holds it — the node that moved it. Negotiation from a
participant connected to another node is passed to that node over the nodes'
private channel, addressed to the call; the unit's answers reach the
participant as any private frame does. Media goes straight from each participant
to the holding node's UDP port, which is why every node that may forward needs
its port reachable. A node that stops answering for a call — it restarted, or
died — has it taken over by the next node a participant's negotiation reaches;
the participants reconnect to it.
*/

// Frames between nodes for a forwarded call, addressed to the call. Never sent
// to a client: no client is a call.
const (
	frameForwardedSignal = "call.forwarded.signal"
	frameForwardedState  = "call.forwarded.state"
)

type forwardedSignal struct {
	From   uuid.UUID       `json:"from"`
	Signal json.RawMessage `json:"signal"`
}

// forwardingOn says whether calls past the mesh limit move to the unit here.
func (s *Server) forwardingOn(ctx context.Context) bool {
	if s.settings.Get(ctx, settings.CallsSFU) != "true" {
		return false
	}
	if err := s.forwarding.Ready(); err != nil {
		s.log.Warn("calls are not forwarded: the forwarding unit could not listen", "error", err)
		return false
	}
	return true
}

/*
callLimit is how many people a call may hold now, and what to tell the next
person when it is full. Without the unit, the mesh limit is the limit; with it,
the participant limit is.
*/
func (s *Server) callLimit(ctx context.Context) (int, string) {
	mesh := int(s.settings.Int(ctx, settings.CallsMeshLimit))
	most := int(s.settings.Int(ctx, settings.CallsMaxParticipants))
	if s.forwardingOn(ctx) || most <= mesh {
		return most, fmt.Sprintf("the call is full: %d people is the most a call may have on this server", most)
	}
	return mesh, fmt.Sprintf("the call is full: %d people is as many as a call can hold "+
		"while everybody sends to everybody, and this server does not forward larger calls", mesh)
}

// joinError says why a join failed, with the limit's own reason for a full call.
func joinError(err error, full string) error {
	if errors.Is(err, rooms.ErrCallFull) {
		return echo.NewHTTPError(http.StatusConflict, full)
	}
	return callError(err)
}

// forwardIfLarge moves a call that a join took past the mesh limit to this
// node's unit, and returns the call as it now is.
func (s *Server) forwardIfLarge(ctx context.Context, call rooms.Call) rooms.Call {
	if call.Mode != "mesh" || len(call.Participants) <= int(s.settings.Int(ctx, settings.CallsMeshLimit)) || !s.forwardingOn(ctx) {
		return call
	}
	moved, err := s.rooms.ForwardCall(ctx, call.ID, s.node)
	if err != nil {
		s.log.Warn("could not move a call to the forwarding unit", "error", err)
		return call
	}
	if moved {
		s.forwarding.Own(call.ID)
	}
	if record, err := s.rooms.CallRecord(ctx, call.ID); err == nil {
		call.Call = record
	}
	return call
}

/*
signalForwarding passes a participant's negotiation to the unit forwarding their
call, on this node or another. The participant is authorised here, on the node
they are connected to, as a mesh signal is.
*/
func (s *Server) signalForwarding(ctx context.Context, from, callID uuid.UUID, signal json.RawMessage) error {
	roomID, in := s.rooms.CallRoomOf(ctx, callID, from)
	if !in {
		return rooms.ErrNoCall
	}
	if !s.mayTakePart(ctx, from, roomID, callID) {
		return errors.New("not allowed to take part in this call")
	}
	record, err := s.rooms.CallRecord(ctx, callID)
	if err != nil {
		return err
	}
	if record.Mode != "sfu" || record.ForwardingNode == nil {
		return errors.New("this call is not forwarded")
	}
	holder := *record.ForwardingNode
	if holder == s.node || s.forwarding.Holds(callID) {
		s.forwarding.Own(callID)
		return s.forwarding.Handle(callID, from, signal)
	}

	payload, err := json.Marshal(forwardedSignal{From: from, Signal: signal})
	if err != nil {
		return err
	}
	if s.broker != nil {
		reached, err := s.broker.Whisper(ctx, callID, realtime.Frame{Type: frameForwardedSignal, Payload: payload}, 1, privateReachLimit)
		if err == nil && reached > 0 {
			return nil
		}
	}
	// Nobody answers for it: that node is gone. Forward it here instead.
	claimed, err := s.rooms.ClaimForwarding(ctx, callID, holder, s.node)
	if err != nil {
		return err
	}
	if !claimed {
		return errors.New("the call's forwarding is moving; try again")
	}
	s.log.Info("took over forwarding a call from a node that no longer answers", "call", callID, "from", holder)
	s.forwarding.Own(callID)
	return s.forwarding.Handle(callID, from, signal)
}

// receiveForwarded takes a frame another node passed to a call forwarded here.
func (s *Server) receiveForwarded(callID uuid.UUID, frame realtime.Frame) int {
	switch frame.Type {
	case frameForwardedSignal:
		var signal forwardedSignal
		if err := json.Unmarshal(frame.Payload, &signal); err != nil {
			return 0
		}
		if err := s.forwarding.Handle(callID, signal.From, signal.Signal); err != nil {
			s.log.Debug("forwarding unit refused a signal from another node", "error", err)
		}
	case frameForwardedState:
		var present []uuid.UUID
		if err := json.Unmarshal(frame.Payload, &present); err != nil {
			return 0
		}
		s.forwarding.Reconcile(callID, present)
	default:
		return 0
	}
	return 1
}

/*
tellForwarding tells the unit forwarding a call who is in it now, so somebody
who left, or was taken out, stops being sent anything at once.
*/
func (s *Server) tellForwarding(ctx context.Context, call dbgen.Call, participants []uuid.UUID) {
	if call.Mode != "sfu" {
		return
	}
	if s.forwarding.Holds(call.ID) {
		s.forwarding.Reconcile(call.ID, participants)
		return
	}
	if s.broker == nil {
		return
	}
	payload, err := json.Marshal(participants)
	if err != nil {
		return
	}
	if _, err := s.broker.Whisper(ctx, call.ID, realtime.Frame{Type: frameForwardedState, Payload: payload}, 1, privateReachLimit); err != nil {
		s.log.Warn("could not tell the forwarding node who is in a call", "error", err)
	}
}

// sendFromForwarding passes the unit's half of a negotiation to a participant,
// as a signal from nobody — the zero identifier, which is the unit.
func (s *Server) sendFromForwarding(callID, to uuid.UUID, signal json.RawMessage) {
	if s.hub == nil {
		return
	}
	payload, err := json.Marshal(map[string]any{"call": callID.String(), "from": uuid.Nil().String(), "signal": signal})
	if err != nil {
		return
	}
	frame := realtime.Frame{Type: realtime.FrameCallSignal, Payload: payload}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.tellPerson(ctx, callID, to, frame)
}
