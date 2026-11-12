package httpapi_test

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
)

// eventBody is a one-off event two days from now, with whatever the test
// changes.
func eventBody(changes map[string]any) map[string]any {
	start := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Minute)
	body := map[string]any{
		"kind": "meetup", "title": "Go meetup", "description": "Bring a **laptop**.",
		"startsAt": start.Format(time.RFC3339), "endsAt": start.Add(2 * time.Hour).Format(time.RFC3339),
		"timeZone": "Africa/Dar_es_Salaam", "visibility": "public",
	}
	for key, value := range changes {
		body[key] = value
	}
	return body
}

func makeEvent(t *testing.T, handler http.Handler, token string, changes map[string]any) map[string]any {
	t.Helper()
	return decode(t, mustCall(t, handler, http.MethodPost, "/api/events", eventBody(changes), token, http.StatusCreated))
}

// listedEventIDs is the ids of the occurrences a person is shown over a range.
func listedEventIDs(t *testing.T, handler http.Handler, token, query string) []string {
	t.Helper()
	ids := []string{}
	for _, raw := range decodeList(t, mustCall(t, handler, http.MethodGet, "/api/events"+query, nil, token, http.StatusOK)) {
		ids = append(ids, raw.(map[string]any)["id"].(string))
	}
	return ids
}

// Off means absent: nothing answers, nothing is offered, and the events are
// still there when it is turned back on.
func TestEventsAreAbsentWhenSwitchedOff(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	if !slices.Contains(advertised(t, handler), "events") {
		t.Fatal("events are not advertised on a fresh server, where they are on by default")
	}
	event := makeEvent(t, handler, admin, nil)
	id := event["id"].(string)

	setFeature(t, handler, admin, "events", false)
	for _, attempt := range []struct{ method, path string }{
		{http.MethodGet, "/api/events"},
		{http.MethodPost, "/api/events"},
		{http.MethodGet, "/api/events/" + id},
		{http.MethodPost, "/api/events/" + id + "/cancel"},
	} {
		if code := call(t, handler, attempt.method, attempt.path, map[string]any{}, admin).Code; code != http.StatusNotFound {
			t.Errorf("%s %s with events off: status %d, want 404", attempt.method, attempt.path, code)
		}
	}
	if slices.Contains(advertised(t, handler), "events") {
		t.Error("events are advertised while they are off")
	}

	setFeature(t, handler, admin, "events", true)
	mustCall(t, handler, http.MethodGet, "/api/events/"+id, nil, admin, http.StatusOK)
}

/*
An event is a room too, and never a conversation anybody sees in their list or
can be invited into: its membership is the calendar's to keep.
*/
func TestAnEventsConversationIsNotAChannel(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	amina := registerAndSignIn(t, handler, "amina")
	event := makeEvent(t, handler, amina, nil)
	id := event["id"].(string)

	for _, raw := range decodeList(t, mustCall(t, handler, http.MethodGet, "/api/rooms", nil, amina, http.StatusOK)) {
		if raw.(map[string]any)["id"] == id {
			t.Fatal("an event's conversation is listed among the creator's conversations")
		}
	}
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+id+"/invitations", map[string]any{}, amina, http.StatusNotFound)
	baraka := registerAndSignIn(t, handler, "baraka")
	if code := call(t, handler, http.MethodPost, "/api/rooms/"+id+"/join", nil, baraka).Code; code == http.StatusOK {
		t.Error("somebody joined an event's conversation without answering the event")
	}
	_ = admin
}

/*
Only the people running an event change it; somebody holding event.manage.any
may too, and that is recorded.
*/
func TestOnlyItsPeopleOrAManagerChangeAnEvent(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	amina := registerAndSignIn(t, handler, "amina")
	baraka := registerAndSignIn(t, handler, "baraka")
	chausiku := registerAndSignIn(t, handler, "chausiku")

	event := makeEvent(t, handler, amina, nil)
	id := event["id"].(string)

	renamed := eventBody(map[string]any{"title": "Renamed by a stranger"})
	if code := call(t, handler, http.MethodPut, "/api/events/"+id, renamed, baraka).Code; code != http.StatusForbidden {
		t.Errorf("a member changed somebody else's event: status %d", code)
	}
	if code := call(t, handler, http.MethodPost, "/api/events/"+id+"/cancel", map[string]any{}, baraka).Code; code != http.StatusForbidden {
		t.Errorf("a member cancelled somebody else's event: status %d", code)
	}
	if code := call(t, handler, http.MethodDelete, "/api/events/"+id, nil, baraka).Code; code != http.StatusForbidden {
		t.Errorf("a member deleted somebody else's event: status %d", code)
	}

	// A host may change it.
	hosted := eventBody(map[string]any{"hosts": []string{accountID(t, handler, baraka)}})
	mustCall(t, handler, http.MethodPut, "/api/events/"+id, hosted, amina, http.StatusOK)
	mustCall(t, handler, http.MethodPut, "/api/events/"+id, eventBody(map[string]any{"title": "Go meetup, round two"}), baraka, http.StatusOK)

	grantRole(t, handler, admin, chausiku, "event.manage.any")
	mustCall(t, handler, http.MethodPut, "/api/events/"+id, eventBody(map[string]any{"title": "Managed"}), chausiku, http.StatusOK)
	auditDetail(t, handler, admin, "event.managed", id)

	mustCall(t, handler, http.MethodPost, "/api/events/"+id+"/cancel", map[string]any{"reason": "venue closed"}, chausiku, http.StatusOK)
	cancelled := decode(t, mustCall(t, handler, http.MethodGet, "/api/events/"+id, nil, amina, http.StatusOK))
	if cancelled["cancelReason"] != "venue closed" || cancelled["cancelledAt"] == nil {
		t.Errorf("a cancelled event reads %v", cancelled)
	}
	auditDetail(t, handler, admin, "event.cancelled", id)
}

// A member without event.create, withdrawn from the Member role, schedules nothing.
func TestSchedulingTakesEventCreate(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	amina := registerAndSignIn(t, handler, "amina")
	member := roleNamed(t, handler, admin, "Member")
	mustCall(t, handler, http.MethodPut, "/api/admin/roles/"+member+"/permissions",
		map[string]any{"permission": "event.create", "granted": false}, admin, http.StatusNoContent)
	recorder := call(t, handler, http.MethodPost, "/api/events", eventBody(nil), amina)
	if recorder.Code != http.StatusForbidden {
		t.Errorf("scheduling without event.create: status %d", recorder.Code)
	}
}

/*
Who sees what: an invite-only event is missing to somebody not invited, a
channel's event to somebody outside the channel, and a public event cannot sit
in a private channel and give its name away.
*/
func TestEventVisibility(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	amina := registerAndSignIn(t, handler, "amina")
	baraka := registerAndSignIn(t, handler, "baraka")

	backend := makeRoom(t, handler, amina, "backend", "private")
	open := makeRoom(t, handler, amina, "frontend", "public")

	private := makeEvent(t, handler, amina, map[string]any{"visibility": "invite", "title": "Secret"})
	inChannel := makeEvent(t, handler, amina, map[string]any{"channelId": backend, "visibility": "channel", "title": "Backend sync"})
	public := makeEvent(t, handler, amina, map[string]any{"channelId": open, "title": "Open meetup"})

	for _, id := range []string{private["id"].(string), inChannel["id"].(string)} {
		recorder := call(t, handler, http.MethodGet, "/api/events/"+id, nil, baraka)
		if recorder.Code != http.StatusNotFound || !strings.Contains(recorder.Body.String(), "no such event") {
			t.Errorf("an event baraka may not see answered %d %s", recorder.Code, recorder.Body.String())
		}
	}
	listed := listedEventIDs(t, handler, baraka, "")
	if slices.Contains(listed, private["id"].(string)) || slices.Contains(listed, inChannel["id"].(string)) {
		t.Errorf("baraka's list includes events closed to him: %v", listed)
	}
	if !slices.Contains(listed, public["id"].(string)) {
		t.Errorf("baraka's list leaves out a public event: %v", listed)
	}

	recorder := call(t, handler, http.MethodPost, "/api/events",
		eventBody(map[string]any{"channelId": backend, "visibility": "public"}), amina)
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("a public event in a private channel: status %d", recorder.Code)
	}
	// Nor in a channel the creator is not in.
	if code := call(t, handler, http.MethodPost, "/api/events",
		eventBody(map[string]any{"channelId": backend, "visibility": "channel"}), baraka).Code; code != http.StatusBadRequest {
		t.Errorf("an event in a channel its creator cannot read: status %d", code)
	}
	_ = admin
}

/*
A weekly series is listed once a week, at its local time in its own zone, over
any range, skipping what was skipped.
*/
func TestARepeatingEventIsListedPerOccurrence(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)

	zone, _ := time.LoadLocation("Europe/London")
	start := time.Date(2026, time.October, 6, 19, 0, 0, 0, zone)
	skipped := start.AddDate(0, 0, 14)
	event := makeEvent(t, handler, admin, map[string]any{
		"startsAt": start.Format(time.RFC3339), "endsAt": start.Add(time.Hour).Format(time.RFC3339),
		"timeZone": "Europe/London", "recurrence": "FREQ=WEEKLY;COUNT=6",
		"exceptions": []string{skipped.UTC().Format(time.RFC3339)},
	})
	if event["recurrence"] != "FREQ=WEEKLY;COUNT=6" {
		t.Errorf("the repeat was stored as %v", event["recurrence"])
	}

	query := "?from=" + url.QueryEscape(start.Add(-time.Hour).Format(time.RFC3339)) +
		"&to=" + url.QueryEscape(start.AddDate(0, 2, 0).Format(time.RFC3339))
	occurrences := decodeList(t, mustCall(t, handler, http.MethodGet, "/api/events"+query, nil, admin, http.StatusOK))
	if len(occurrences) != 5 {
		t.Fatalf("six weekly occurrences with one skipped listed %d", len(occurrences))
	}
	for _, raw := range occurrences {
		at, _ := time.Parse(time.RFC3339, raw.(map[string]any)["occurrenceStartsAt"].(string))
		if local := at.In(zone); local.Hour() != 19 || local.Weekday() != time.Tuesday {
			t.Errorf("an occurrence starts at %v London time; the series is Tuesdays at 19:00", local)
		}
		if at.Equal(skipped) {
			t.Error("the skipped occurrence is listed")
		}
	}

	bad := call(t, handler, http.MethodPost, "/api/events", eventBody(map[string]any{"recurrence": "FREQ=YEARLY"}), admin)
	if bad.Code != http.StatusBadRequest {
		t.Errorf("a yearly repeat was accepted: %d", bad.Code)
	}
}
