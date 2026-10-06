package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/AnoRebel/tdc/server/internal/httpapi"
)

// directCallBetween opens a direct conversation and has the first person start
// a call in it, returning the conversation.
func directCallBetween(t *testing.T, handler http.Handler, caller, other string) string {
	t.Helper()
	room := decode(t, mustCall(t, handler, http.MethodPost, "/api/direct",
		map[string]any{"userIds": []string{accountID(t, handler, other)}}, caller, http.StatusOK))
	id, _ := room["id"].(string)
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+id+"/call", map[string]any{"audio": true}, caller, http.StatusOK)
	return id
}

/*
Ringing reaches every device of the person rung, and answering on one stops the
others.
*/
func TestARingReachesEveryDeviceAndStopsWhenAnswered(t *testing.T) {
	handler, _, _, amina, baraka := withCalls(t)
	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	phone := liveConnection(t, ctx, server, baraka)
	laptop := liveConnection(t, ctx, server, baraka)

	room := directCallBetween(t, handler, amina, baraka)
	rung := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/call/ring", map[string]any{}, amina, http.StatusOK))
	if got, _ := rung["rung"].([]any); len(got) != 1 {
		t.Fatalf("ringing in a direct conversation rang %v", rung)
	}
	for name, device := range map[string]*websocket.Conn{"phone": phone, "laptop": laptop} {
		if _, ok := nextCallFrame(t, ctx, device, "call.ring"); !ok {
			t.Fatalf("the %s never rang", name)
		}
	}

	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/call", map[string]any{"audio": true}, baraka, http.StatusOK)
	if _, ok := nextCallFrame(t, ctx, laptop, "call.ring_ended"); !ok {
		t.Error("answering did not stop the other device ringing")
	}
}

/*
Who may call somebody is theirs to say, and a refusal says nothing about why:
the response counts who was not rung without naming a reason.
*/
func TestCallPrivacyDecidesWhoMayRing(t *testing.T) {
	handler, admin, _, amina, baraka := withCalls(t)

	privacy := decode(t, mustCall(t, handler, http.MethodGet, "/api/me/privacy", nil, baraka, http.StatusOK))
	if privacy["calls"] != "anyone" || privacy["callsFromOtherServers"] != true {
		t.Errorf("the default call privacy reads %v", privacy)
	}

	mustCall(t, handler, http.MethodPut, "/api/me/privacy", map[string]any{"calls": "nobody"}, baraka, http.StatusNoContent)
	room := directCallBetween(t, handler, amina, baraka)
	refused := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/call/ring", map[string]any{}, amina, http.StatusOK))
	if got, _ := refused["rung"].([]any); len(got) != 0 || refused["notRung"] != float64(1) {
		t.Errorf("somebody taking no calls was rung: %v", refused)
	}
	if strings.Contains(strings.ToLower(mustJSON(t, refused)), "nobody") {
		t.Error("the refusal says which setting refused it")
	}

	// "People I share a conversation with": a stranger who only opened a direct
	// conversation is not one; somebody in a channel with them is.
	mustCall(t, handler, http.MethodPut, "/api/me/privacy", map[string]any{"calls": "shared"}, baraka, http.StatusNoContent)
	stranger := registerAndSignIn(t, handler, "chausiku")
	strangerRoom := directCallBetween(t, handler, stranger, baraka)
	notRung := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+strangerRoom+"/call/ring", map[string]any{}, stranger, http.StatusOK))
	if got, _ := notRung["rung"].([]any); len(got) != 0 {
		t.Errorf("a stranger rang somebody who takes calls only from people they share a conversation with: %v", notRung)
	}
	mustCall(t, handler, http.MethodDelete, "/api/rooms/"+room+"/call", nil, amina, http.StatusNoContent)
	_ = admin
	again := directCallBetween(t, handler, amina, baraka)
	rung := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+again+"/call/ring", map[string]any{}, amina, http.StatusOK))
	if got, _ := rung["rung"].([]any); len(got) != 1 {
		t.Errorf("a colleague sharing a channel could not ring: %v", rung)
	}
}

// A ring nobody answers becomes a missed call, pointing at the call.
func TestAnUnansweredRingIsAMissedCall(t *testing.T) {
	previous := httpapi.SetRingTimeout(200 * time.Millisecond)
	defer httpapi.SetRingTimeout(previous)

	handler, _, _, amina, baraka := withCalls(t)
	room := directCallBetween(t, handler, amina, baraka)
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/call/ring", map[string]any{}, amina, http.StatusOK)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, hit := range notificationsFor(t, handler, baraka) {
			if hit.Reason == "missed_call" {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Error("an unanswered ring left no missed-call notification")
}

// Declining stops the ring and tells the people in the call.
func TestDecliningTellsTheCall(t *testing.T) {
	handler, _, _, amina, baraka := withCalls(t)
	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	caller := liveConnection(t, ctx, server, amina)

	room := directCallBetween(t, handler, amina, baraka)
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/call/ring", map[string]any{}, amina, http.StatusOK)
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/call/decline",
		map[string]any{"message": "  In a meeting, call you after  "}, baraka, http.StatusNoContent)
	frame, ok := nextCallFrame(t, ctx, caller, "call.declined")
	if !ok || !strings.Contains(mustJSON(t, frame), "baraka") {
		t.Errorf("the caller was not told who declined: %v", frame)
	}
	if !strings.Contains(mustJSON(t, frame), `"message":"In a meeting, call you after"`) {
		t.Errorf("the caller did not get the words sent with the decline: %v", frame)
	}

	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/call/decline",
		map[string]any{"message": strings.Repeat("a", 281)}, baraka, http.StatusBadRequest)
}
