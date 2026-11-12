package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/federation"
	"github.com/AnoRebel/tdc/server/internal/settings"
)

/*
federationDeliveryInterval is how often the server looks for peers owed
events. Short, because a message reaching another server is a conversation
happening, and the look itself is one indexed query that finds nothing almost
always.
*/
const federationDeliveryInterval = 2 * time.Second

// federationDeliveryBatch is how many peers one pass claims at a time.
const federationDeliveryBatch = 50

/*
replicaHomeCheckInterval is how often each replica asks its home for anything
it missed — which also finds out whether the home still answers.
*/
const replicaHomeCheckInterval = time.Minute

/*
deliverFederationPeriodically sends every peer what it is owed, until the
context ends.

The rooms this server is home to are told to their peers as their events
arrive, a batch at a time, each peer from its own cursor: a peer that fails is
retried later and later, and abandoned past the ceiling until an operator
resumes it, by the bookkeeping on its row. Nothing runs while federation is
off or the server has no address to sign as.
*/
func (s *Server) deliverFederationPeriodically(ctx context.Context) {
	ticker := time.NewTicker(federationDeliveryInterval)
	defer ticker.Stop()
	var checked time.Time
	for {
		s.deliverDue(ctx)
		if time.Since(checked) >= replicaHomeCheckInterval {
			s.checkReplicaHomes(ctx)
			checked = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// deliverDue delivers to every peer owed something, until none is.
func (s *Server) deliverDue(ctx context.Context) {
	if s.settings == nil || s.config.CanonicalAddress == "" ||
		s.settings.Get(ctx, settings.FederationEnabled) != "true" {
		return
	}
	sender := federation.NewSender(s.config.CanonicalAddress, s.rooms.Keyring(), s.federationTrust(ctx),
		s.settings.Get(ctx, settings.FederationAllowPrivateAddresses) == "true")
	s.offerAndWithdraw(ctx, sender)

	for ctx.Err() == nil {
		peers, err := s.rooms.ClaimPeersDue(ctx, federationDeliveryBatch)
		if err != nil {
			if ctx.Err() == nil {
				s.log.Error("could not find peers owed events", "error", err.Error())
			}
			return
		}
		if len(peers) == 0 {
			return
		}
		rooms := make(map[uuid.UUID]dbgen.Room, len(peers))
		for _, peer := range peers {
			if room, err := s.rooms.ByID(ctx, peer.RoomID); err == nil {
				rooms[peer.RoomID] = room
			}
		}
		// A project before its issues' discussions, whose rooms its replica
		// makes: the claim chose in that order, but RETURNING keeps none.
		later := func(peer dbgen.RoomPeer) int {
			if rooms[peer.RoomID].Kind == "project" {
				return 0
			}
			return 1
		}
		slices.SortStableFunc(peers, func(a, b dbgen.RoomPeer) int { return later(a) - later(b) })
		for _, peer := range peers {
			room, found := rooms[peer.RoomID]
			if !found {
				continue
			}
			if result := s.deliverToPeer(ctx, sender, room, peer); result["error"] != nil {
				s.log.Warn("a federated delivery failed", "peer", peer.Peer, "room", room.ID, "error", result["error"])
			}
		}
	}
}

// DeliverDueForTest runs one delivery pass, as the background loop does.
func (s *Server) DeliverDueForTest(ctx context.Context) { s.deliverDue(ctx) }

/*
checkReplicaHomes asks each replica's home for the events it has not got.

A delivery that never arrived is made up this way, and the asking is the check
on the home itself: one that does not answer is recorded, from the first
failure, so readers of the replica are told it may be behind and that changes
cannot be carried there. A home that answers with events this server refuses
did answer; that is the replica's own trouble, said by its replica error.
*/
func (s *Server) checkReplicaHomes(ctx context.Context) {
	if s.settings == nil || s.config.CanonicalAddress == "" ||
		s.settings.Get(ctx, settings.FederationEnabled) != "true" {
		return
	}
	replicas, err := s.projects.Replicas(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.log.Error("could not list replicas", "error", err.Error())
		}
		return
	}
	for _, id := range replicas {
		room, err := s.rooms.ByID(ctx, id)
		if err != nil {
			continue
		}
		_, _, err = s.catchUp(ctx, room)
		var refused *catchUpRefusedError
		reachable := err == nil || errors.As(err, &refused) && refused.status == http.StatusUnprocessableEntity
		if !reachable {
			s.log.Warn("a replica's home did not answer", "project", id, "error", err.Error())
		}
		if err := s.projects.SetHomeReachable(ctx, id, reachable); err != nil && ctx.Err() == nil {
			s.log.Error("could not record whether a replica's home answered", "project", id, "error", err.Error())
		}
		if err := s.projects.ResumeIfStopped(ctx, room); err != nil && ctx.Err() == nil {
			s.log.Warn("a stopped replica is still stopped", "project", id, "error", err.Error())
		}
	}
}

/*
RebuildReplica brings a replica held here up to date by hand, for an operator:
whatever its home has that it does not, then its tables replayed from its whole
stream, then whatever its issues' discussions missed. It says how many events it now holds. The home not answering is said,
not fatal: the replica is still replayed from what it has.
*/
func (s *Server) RebuildReplica(ctx context.Context, key string) (string, error) {
	project, err := s.projects.ReplicaByKey(ctx, key)
	if err != nil {
		return "", err
	}
	room, err := s.rooms.ByID(ctx, project.ID)
	if err != nil {
		return "", fmt.Errorf("%s's room: %w", key, err)
	}
	var notes []string
	if _, stored, err := s.catchUp(ctx, room); err != nil {
		notes = append(notes, fmt.Sprintf("its home did not answer (%v), so it is replayed from what is held", err))
	} else {
		notes = append(notes, fmt.Sprintf("%d events fetched from its home", stored))
	}
	if err := s.projects.Replicate(ctx, room); err != nil {
		return strings.Join(notes, "; "), err
	}
	// Then each issue's discussion, which the minute's check leaves to the
	// home's own retries.
	discussions, err := s.projects.Discussions(ctx, room.ID)
	if err != nil {
		return "", err
	}
	missed := 0
	for _, id := range discussions {
		discussion, err := s.rooms.ByID(ctx, id)
		if err != nil {
			continue
		}
		if _, stored, err := s.catchUp(ctx, discussion); err == nil {
			missed += stored
		}
	}
	notes = append(notes, fmt.Sprintf("%d messages fetched for %d discussions", missed, len(discussions)))
	stream, err := s.projects.Stream(ctx, room.ID)
	if err != nil {
		return "", err
	}
	notes = append(notes, fmt.Sprintf("replayed from %d events", len(stream)))
	return strings.Join(notes, "; "), nil
}

// CheckReplicaHomesForTest runs one check of every replica's home.
func (s *Server) CheckReplicaHomesForTest(ctx context.Context) { s.checkReplicaHomes(ctx) }
