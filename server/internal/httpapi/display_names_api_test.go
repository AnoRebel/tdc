package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AnoRebel/tdc/server/internal/httpapi"
)

/*
People are called by their display names wherever one is named to another:
somebody ringing, somebody's message, somebody behind a notification. The
username stays beside it, as the identifier it is.
*/
func TestPeopleAreCalledByTheirDisplayNames(t *testing.T) {
	previous := httpapi.SetRingTimeout(200 * time.Millisecond)
	defer httpapi.SetRingTimeout(previous)

	handler, _, _, amina, baraka := withCalls(t)
	mustCall(t, handler, http.MethodPut, "/api/me/display-name",
		map[string]string{"displayName": "Amina Juma"}, amina, http.StatusOK)

	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	phone := liveConnection(t, ctx, server, baraka)

	room := directCallBetween(t, handler, amina, baraka)
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/call/ring", map[string]any{}, amina, http.StatusOK)

	ring, ok := nextCallFrame(t, ctx, phone, "call.ring")
	if !ok {
		t.Fatal("the ring never arrived")
	}
	from, _ := ring["payload"].(map[string]any)["from"].(map[string]any)
	if from["displayName"] != "Amina Juma" || from["username"] != "amina" {
		t.Errorf("the ring is from %v, want Amina Juma with her username beside it", from)
	}

	// The ring goes unanswered, and the missed call says who.
	var missed map[string]any
	deadline := time.Now().Add(5 * time.Second)
	for missed == nil && time.Now().Before(deadline) {
		list := decodeList(t, mustCall(t, handler, http.MethodGet, "/api/me/notifications?grouped=false", nil, baraka, http.StatusOK))
		for _, raw := range list {
			if entry, _ := raw.(map[string]any); entry["reason"] == "missed_call" {
				missed = entry
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if missed == nil {
		t.Fatal("no missed call was recorded")
	}
	if missed["actorName"] != "Amina Juma" {
		t.Errorf("the missed call is from %v, want Amina Juma", missed["actorName"])
	}

	// A message names its sender the same way, in the timeline.
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/messages",
		map[string]any{"body": "Are you around?"}, amina, http.StatusCreated)
	timeline := decodeList(t, mustCall(t, handler, http.MethodGet, "/api/rooms/"+room+"/timeline", nil, baraka, http.StatusOK))
	found := false
	for _, raw := range timeline {
		if message, _ := raw.(map[string]any); message["type"] == "message.sent" {
			found = true
			if message["senderName"] != "Amina Juma" {
				t.Errorf("the message's sender is called %v, want Amina Juma", message["senderName"])
			}
		}
	}
	if !found {
		t.Error("the message is not in the timeline")
	}
}
