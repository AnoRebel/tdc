package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func oneEvent(uid, title string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\n" +
		"BEGIN:VEVENT\r\nUID:" + uid + "\r\nSUMMARY:" + title + "\r\n" +
		"DTSTART;TZID=Africa/Dar_es_Salaam:20271107T180000\r\nDTEND;TZID=Africa/Dar_es_Salaam:20271107T200000\r\n" +
		"RRULE:FREQ=WEEKLY;COUNT=3\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
}

/*
Importing the same file twice updates rather than duplicates, and an event
somebody else made under the same identity is skipped, never overwritten.
*/
func TestImportingTwiceCreatesNothingTwice(t *testing.T) {
	handler := newTestAPI(t)
	_ = completeSetup(t, handler)
	amina := registerAndSignIn(t, handler, "amina")
	baraka := registerAndSignIn(t, handler, "baraka")
	file := oneEvent("meetup-1@example.com", "Imported meetup")

	preview := decode(t, mustCall(t, handler, http.MethodPost, "/api/events/import/preview", map[string]any{"ics": file}, amina, http.StatusOK))
	plans := preview["events"].([]any)
	if len(plans) != 1 || plans[0].(map[string]any)["action"] != "create" || plans[0].(map[string]any)["timeZone"] != "Africa/Dar_es_Salaam" {
		t.Fatalf("the preview reads %v", plans)
	}
	first := decode(t, mustCall(t, handler, http.MethodPost, "/api/events/import", map[string]any{"ics": file}, amina, http.StatusOK))
	second := decode(t, mustCall(t, handler, http.MethodPost, "/api/events/import",
		map[string]any{"ics": oneEvent("meetup-1@example.com", "Renamed meetup")}, amina, http.StatusOK))
	if first["created"] != float64(1) || second["created"] != float64(0) || second["updated"] != float64(1) {
		t.Errorf("two imports: %v then %v", first, second)
	}
	theirs := decode(t, mustCall(t, handler, http.MethodPost, "/api/events/import", map[string]any{"ics": file}, baraka, http.StatusOK))
	if theirs["skipped"] != float64(1) || theirs["created"] != float64(0) {
		t.Errorf("somebody else importing the same identity: %v", theirs)
	}
}

/*
Following a calendar takes event.calendars.manage, refuses an address on a
private network, and — where the operator allows private addresses — creates
the calendar's events and cancels the ones it later drops.
*/
func TestFollowingACalendar(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	amina := registerAndSignIn(t, handler, "amina")

	var served atomic.Value
	served.Store(oneEvent("remote-1@example.com", "Remote meetup"))
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/calendar")
		_, _ = w.Write([]byte(served.Load().(string)))
	}))
	defer remote.Close()
	body := map[string]any{"url": remote.URL, "label": "Community calendar"}

	if code := call(t, handler, http.MethodPost, "/api/calendar/subscriptions", body, amina).Code; code != http.StatusForbidden {
		t.Errorf("following without event.calendars.manage: status %d", code)
	}
	refused := call(t, handler, http.MethodPost, "/api/calendar/subscriptions", body, admin)
	if refused.Code != http.StatusBadRequest || !strings.Contains(refused.Body.String(), "private") {
		t.Errorf("following a private address: %d %s", refused.Code, refused.Body.String())
	}

	setSetting(t, handler, admin, "integrations.allow_private_addresses", "true")
	made := decode(t, mustCall(t, handler, http.MethodPost, "/api/calendar/subscriptions", body, admin, http.StatusCreated))
	if made["lastError"] != nil {
		t.Fatalf("the first fetch failed: %v", made["lastError"])
	}
	listed := decodeList(t, mustCall(t, handler, http.MethodGet, "/api/events?from=2027-11-01T00:00:00Z&to=2027-12-31T00:00:00Z", nil, amina, http.StatusOK))
	if len(listed) != 3 || listed[0].(map[string]any)["imported"] != true {
		t.Fatalf("the followed calendar's weekly event listed %d times: %v", len(listed), listed)
	}
	id := listed[0].(map[string]any)["id"].(string)
	if code := call(t, handler, http.MethodPut, "/api/events/"+id, eventBody(nil), admin).Code; code != http.StatusBadRequest {
		t.Errorf("an imported event was edited here: %d", code)
	}

	served.Store(oneEvent("remote-2@example.com", "Another meetup"))
	refreshed := decode(t, mustCall(t, handler, http.MethodPost, "/api/calendar/subscriptions/"+made["id"].(string)+"/refresh", nil, admin, http.StatusOK))
	if refreshed["created"] != float64(1) || refreshed["cancelled"] != float64(1) {
		t.Errorf("refreshing a changed calendar: %v", refreshed)
	}
	gone := decode(t, mustCall(t, handler, http.MethodGet, "/api/events/"+id, nil, amina, http.StatusOK))
	if gone["cancelledAt"] == nil {
		t.Error("an event dropped from the followed calendar is not cancelled")
	}
	if fetched := serverBehind(t, handler).RefreshDueSubscriptions(context.Background()); fetched != 0 {
		t.Errorf("a subscription just fetched was fetched again: %d", fetched)
	}
}
