package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// offer offers a file directly and returns the transfer's identifier.
func offer(t *testing.T, handler http.Handler, token, roomID, recipientID string, want int) string {
	t.Helper()
	recorder := mustCall(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/transfers", map[string]any{
		"recipient": recipientID, "filename": "holiday.mp4", "size": 2 << 30, "contentType": "video/mp4",
	}, token, want)
	if want != http.StatusCreated {
		return ""
	}
	id, _ := decode(t, recorder)["id"].(string)
	return id
}

/*
An offer is between two people: only they see it, only the recipient accepts
or declines it, an offer answered twice is answered once, and one nobody
answered in time cannot be accepted late.
*/
func TestADirectTransferIsBetweenItsTwoPeople(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	amani := registerAndSignIn(t, handler, "amani")
	stranger := registerAndSignIn(t, handler, "stranger")
	direct := openDirectWith(t, handler, founder, "amani")
	amaniID := userIDOf(t, handler, founder, "amani")

	id := offer(t, handler, founder, direct, amaniID, http.StatusCreated)

	mustCall(t, handler, http.MethodGet, "/api/transfers/"+id, nil, amani, http.StatusOK)
	mustCall(t, handler, http.MethodGet, "/api/transfers/"+id, nil, stranger, http.StatusNotFound)
	mustCall(t, handler, http.MethodPost, "/api/transfers/"+id+"/accept", nil, stranger, http.StatusNotFound)
	// The sender cannot accept on the recipient's behalf.
	mustCall(t, handler, http.MethodPost, "/api/transfers/"+id+"/accept", nil, founder, http.StatusForbidden)

	accepted := decode(t, mustCall(t, handler, http.MethodPost, "/api/transfers/"+id+"/accept", nil, amani, http.StatusOK))
	if accepted["state"] != "accepted" {
		t.Fatalf("the transfer is %v after accepting", accepted["state"])
	}
	mustCall(t, handler, http.MethodPost, "/api/transfers/"+id+"/decline", nil, amani, http.StatusConflict)
	mustCall(t, handler, http.MethodPost, "/api/transfers/"+id+"/finish", nil, amani, http.StatusOK)

	// An offer whose time ran out.
	late := offer(t, handler, founder, direct, amaniID, http.StatusCreated)
	if _, err := scheduledDatabase(t).Exec(context.Background(),
		`UPDATE direct_transfers SET expires_at = now() - interval '1 second' WHERE id = $1`, late); err != nil {
		t.Fatal(err)
	}
	mustCall(t, handler, http.MethodPost, "/api/transfers/"+late+"/accept", nil, amani, http.StatusConflict)
}

// Nobody can be offered a file from outside their conversation, or across a
// block — and the refusal is the same either way, so it says nothing about who
// blocked whom.
func TestAnOfferNeedsTheConversationAndNoBlock(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	amani := registerAndSignIn(t, handler, "amani")
	registerAndSignIn(t, handler, "outsider")
	direct := openDirectWith(t, handler, founder, "amani")

	offer(t, handler, founder, direct, userIDOf(t, handler, founder, "outsider"), http.StatusNotFound)

	mustCall(t, handler, http.MethodPost, "/api/me/blocks",
		map[string]any{"user": userIDOf(t, handler, amani, "founder")}, amani, http.StatusNoContent)
	offer(t, handler, founder, direct, userIDOf(t, handler, founder, "amani"), http.StatusNotFound)
}

/*
The negotiation is relayed only within an accepted offer and only between its
two people: not before it is accepted, and never for somebody else — even
somebody else in the same channel.
*/
func TestOnlyTheTwoPeopleNegotiateAnAcceptedTransfer(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	amani := registerAndSignIn(t, handler, "amani")
	bystander := registerAndSignIn(t, handler, "stranger")
	direct := makeRoom(t, handler, founder, "everyone", "public")
	joinRoom(t, handler, amani, direct)
	joinRoom(t, handler, bystander, direct)
	amaniID := userIDOf(t, handler, founder, "amani")
	founderID := uuid.MustParse(userIDOf(t, handler, amani, "founder"))
	strangerID := uuid.MustParse(userIDOf(t, handler, founder, "stranger"))

	id := uuid.MustParse(offer(t, handler, founder, direct, amaniID, http.StatusCreated))
	signal := json.RawMessage(`{"description":{"type":"offer","sdp":"v=0"}}`)
	server := serverOf(handler)
	ctx := context.Background()

	if err := server.SignalTransfer(ctx, founderID, id, signal); err == nil {
		t.Error("a signal was relayed before the recipient accepted")
	}

	mustCall(t, handler, http.MethodPost, "/api/transfers/"+id.String()+"/accept", nil, amani, http.StatusOK)

	if err := server.SignalTransfer(ctx, strangerID, id, signal); err == nil {
		t.Error("somebody outside the transfer signalled it")
	}
	if err := server.SignalTransfer(ctx, founderID, id, signal); err != nil {
		t.Errorf("the sender could not signal an accepted transfer: %v", err)
	}
	if err := server.SignalTransfer(ctx, uuid.MustParse(amaniID), id, signal); err != nil {
		t.Errorf("the recipient could not signal an accepted transfer: %v", err)
	}

	mustCall(t, handler, http.MethodPost, "/api/transfers/"+id.String()+"/cancel", nil, founder, http.StatusOK)
	if err := server.SignalTransfer(ctx, founderID, id, signal); err == nil {
		t.Error("a signal was relayed for a transfer that had been stopped")
	}
}

// Direct transfers can be turned off, and then nothing about them answers.
func TestDirectTransfersCanBeTurnedOff(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	registerAndSignIn(t, handler, "amani")
	direct := openDirectWith(t, handler, founder, "amani")

	setFeature(t, handler, founder, "direct-transfers", false)
	offer(t, handler, founder, direct, userIDOf(t, handler, founder, "amani"), http.StatusNotFound)
	mustCall(t, handler, http.MethodGet, "/api/transfers/ice", nil, founder, http.StatusNotFound)
}
