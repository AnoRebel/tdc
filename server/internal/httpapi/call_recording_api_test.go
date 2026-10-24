package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

/*
Recording needs both the operator's calls.recording and the person's
call.record. The administrator holds every permission and is still refused
while the setting is off; a member is refused, naming the permission, until a
role gives it.
*/
func TestRecordingNeedsTheSettingAndThePermission(t *testing.T) {
	handler, admin, room, amina, _ := withCalls(t)
	for _, person := range []string{admin, amina} {
		mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/call", map[string]any{"audio": true}, person, http.StatusOK)
	}
	recording := "/api/rooms/" + room + "/call/recording"

	for _, person := range []string{admin, amina} {
		if got := call(t, handler, http.MethodPut, recording, nil, person); got.Code != http.StatusForbidden ||
			!strings.Contains(got.Body.String(), "not enabled") {
			t.Errorf("recording with the setting off: %d %s", got.Code, got.Body.String())
		}
	}

	setSetting(t, handler, admin, "calls.recording", "true")
	if got := call(t, handler, http.MethodPut, recording, nil, amina); got.Code != http.StatusForbidden ||
		!strings.Contains(got.Body.String(), "call.record") {
		t.Errorf("recording without call.record: %d %s", got.Code, got.Body.String())
	}

	grantRole(t, handler, admin, amina, "call.record")
	state := decode(t, mustCall(t, handler, http.MethodPut, recording, nil, amina, http.StatusOK))
	if got, _ := state["recording"].([]any); len(got) != 1 || got[0] != accountID(t, handler, amina) {
		t.Errorf("after starting, the call says %v is recording", state["recording"])
	}
}

/*
A recording is in the conversation's log, the audit log and the call's state —
and the state has gone out to everybody in the call — by the time the recorder
is told it may begin. Leaving stops it, and so does a dropped connection, each
noted the same way.
*/
func TestEverybodyIsToldBeforeARecordingBegins(t *testing.T) {
	handler, admin, room, amina, baraka := withCalls(t)
	setSetting(t, handler, admin, "calls.recording", "true")
	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	other := liveConnection(t, ctx, server, baraka)
	for _, person := range []string{admin, amina, baraka} {
		mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/call", map[string]any{"audio": true}, person, http.StatusOK)
	}
	recording := "/api/rooms/" + room + "/call/recording"
	adminID := accountID(t, handler, admin)

	mustCall(t, handler, http.MethodPut, recording, nil, admin, http.StatusOK)

	// Already on its way to the others when the recorder heard back.
	told := false
	for !told {
		frame, ok := nextCallFrame(t, ctx, other, "call.state")
		if !ok {
			break
		}
		told = strings.Contains(mustJSON(t, frame), `"recording":["`+adminID+`"]`)
	}
	if !told {
		t.Error("somebody in the call was not told of the recording")
	}
	if !slices.Contains(timelineTypes(t, handler, amina, room), "call.recording_started") {
		t.Error("the recording is not in the conversation's log")
	}
	if entries := decodeList(t, mustCall(t, handler, http.MethodGet, "/api/admin/audit?action=call.recording.started", nil, admin, http.StatusOK)); len(entries) != 1 {
		t.Errorf("the audit log holds %d recording starts, want 1", len(entries))
	}
	// Somebody about to join sees it before they do.
	if state := decode(t, mustCall(t, handler, http.MethodGet, "/api/rooms/"+room+"/call", nil, amina, http.StatusOK)); state["recording"] == nil {
		t.Error("the call does not say it is being recorded")
	}

	// Leaving stops it.
	mustCall(t, handler, http.MethodDelete, "/api/rooms/"+room+"/call", nil, admin, http.StatusNoContent)
	if !slices.Contains(timelineTypes(t, handler, amina, room), "call.recording_stopped") {
		t.Error("leaving did not stop the recording in the conversation's log")
	}
	if state := decode(t, mustCall(t, handler, http.MethodGet, "/api/rooms/"+room+"/call", nil, amina, http.StatusOK)); state["recording"] != nil {
		t.Errorf("after the recorder left, the call says %v is recording", state["recording"])
	}

	// So does a dropped connection: the administrator holds none, so the sweep
	// takes them out of the call, and their recording with them.
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/call", map[string]any{"audio": true}, admin, http.StatusOK)
	mustCall(t, handler, http.MethodPut, recording, nil, admin, http.StatusOK)
	serverOf(handler).SweepCalls(context.Background())
	stops := decodeList(t, mustCall(t, handler, http.MethodGet, "/api/admin/audit?action=call.recording.stopped", nil, admin, http.StatusOK))
	if len(stops) != 2 {
		t.Errorf("the audit log holds %d recording stops, want 2", len(stops))
	}
}
