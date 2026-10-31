package httpapi

import (
	"context"
	"time"

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
	for {
		s.deliverDue(ctx)
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
		for _, peer := range peers {
			room, err := s.rooms.ByID(ctx, peer.RoomID)
			if err != nil {
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
