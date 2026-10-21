package httpapi_test

import (
	"context"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"
)

func answer(t *testing.T, handler http.Handler, token, id, status string) map[string]any {
	t.Helper()
	return decode(t, mustCall(t, handler, http.MethodPut, "/api/events/"+id+"/rsvp",
		map[string]any{"status": status}, token, http.StatusOK))
}

func reasonsOf(t *testing.T, handler http.Handler, token string) []string {
	t.Helper()
	reasons := []string{}
	for _, notification := range notificationsOf(t, handler, token) {
		reasons = append(reasons, notification["reason"].(string))
	}
	return reasons
}

/*
Two people answering for the last place at once cannot both have it; the one
who loses waits, and moves up — and is told — when somebody going declines.
*/
func TestTheLastPlaceGoesToOnePersonAndTheWaitingListMovesUp(t *testing.T) {
	handler := newTestAPI(t)
	_ = completeSetup(t, handler)
	host := registerAndSignIn(t, handler, "host")
	amina := registerAndSignIn(t, handler, "amina")
	baraka := registerAndSignIn(t, handler, "baraka")
	chausiku := registerAndSignIn(t, handler, "chausiku")

	event := makeEvent(t, handler, host, map[string]any{"capacity": 2})
	id := event["id"].(string)
	if got := answer(t, handler, amina, id, "going")["status"]; got != "going" {
		t.Fatalf("the first answer is %v", got)
	}

	var wait sync.WaitGroup
	results := make([]string, 2)
	for i, token := range []string{baraka, chausiku} {
		wait.Add(1)
		go func() {
			defer wait.Done()
			recorder := call(t, handler, http.MethodPut, "/api/events/"+id+"/rsvp", map[string]any{"status": "going"}, token)
			if recorder.Code == http.StatusOK {
				results[i], _ = decode(t, recorder)["status"].(string)
			}
		}()
	}
	wait.Wait()
	slices.Sort(results)
	if !slices.Equal(results, []string{"going", "waitlisted"}) {
		t.Fatalf("two people answering for one place came out %v", results)
	}
	waiting := baraka
	if decode(t, mustCall(t, handler, http.MethodGet, "/api/events/"+id, nil, baraka, http.StatusOK))["myAnswer"] == "going" {
		waiting = chausiku
	}

	answer(t, handler, amina, id, "declined")
	detail := decode(t, mustCall(t, handler, http.MethodGet, "/api/events/"+id, nil, waiting, http.StatusOK))
	if detail["myAnswer"] != "going" {
		t.Errorf("after a place freed, the person waiting is %v", detail["myAnswer"])
	}
	if !slices.Contains(reasonsOf(t, handler, waiting), "event_promoted") {
		t.Error("the person moved up was not told")
	}
	// The host does not take a place.
	answer(t, handler, host, id, "going")
	if counts := detail["counts"].(map[string]any); counts["going"] != float64(2) {
		t.Errorf("counts read %v", counts)
	}
}

/*
Somebody who hides which events they go to is counted and not named — except
to the event's organisers. Sabotage-verified: naming everybody fails the first
assertion.
*/
func TestHiddenAttendanceIsCountedNotNamed(t *testing.T) {
	handler := newTestAPI(t)
	_ = completeSetup(t, handler)
	host := registerAndSignIn(t, handler, "host")
	amina := registerAndSignIn(t, handler, "amina")
	baraka := registerAndSignIn(t, handler, "baraka")

	mustCall(t, handler, http.MethodPut, "/api/me/privacy", map[string]any{"showsEventAttendance": false}, amina, http.StatusNoContent)
	if decode(t, mustCall(t, handler, http.MethodGet, "/api/me/privacy", nil, amina, http.StatusOK))["showsEventAttendance"] != false {
		t.Fatal("the setting was not kept")
	}
	id := makeEvent(t, handler, host, nil)["id"].(string)
	answer(t, handler, amina, id, "going")
	answer(t, handler, baraka, id, "going")

	names := func(token string) ([]string, map[string]any) {
		body := decode(t, mustCall(t, handler, http.MethodGet, "/api/events/"+id+"/attendees", nil, token, http.StatusOK))
		listed := []string{}
		for _, raw := range body["attendees"].([]any) {
			listed = append(listed, raw.(map[string]any)["username"].(string))
		}
		return listed, body["unnamed"].(map[string]any)
	}
	shown, unnamed := names(baraka)
	if slices.Contains(shown, "amina") || unnamed["going"] != float64(1) {
		t.Errorf("baraka sees %v and %v unnamed", shown, unnamed)
	}
	if shown, _ := names(host); !slices.Contains(shown, "amina") {
		t.Errorf("the host does not see amina: %v", shown)
	}
	if shown, _ := names(amina); !slices.Contains(shown, "amina") {
		t.Error("amina does not see herself")
	}
	types := timelineTypes(t, handler, host, id)
	if count := len(slices.DeleteFunc(slices.Clone(types), func(kind string) bool { return kind != "event.rsvp" })); count != 1 {
		t.Errorf("the event's log holds %d answers; only baraka's should be recorded: %v", count, types)
	}
}

// An invited channel's members can see an invite-only event; nobody else can.
func TestInvitingAChannelOpensAnInviteOnlyEventToItsMembers(t *testing.T) {
	handler := newTestAPI(t)
	_ = completeSetup(t, handler)
	host := registerAndSignIn(t, handler, "host")
	amina := registerAndSignIn(t, handler, "amina")
	baraka := registerAndSignIn(t, handler, "baraka")
	crew := makeRoom(t, handler, host, "crew", "public")
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+crew+"/join", nil, amina, http.StatusOK)

	id := makeEvent(t, handler, host, map[string]any{"visibility": "invite"})["id"].(string)
	mustCall(t, handler, http.MethodGet, "/api/events/"+id, nil, amina, http.StatusNotFound)
	// Somebody who cannot see it cannot invite to it either.
	mustCall(t, handler, http.MethodPost, "/api/events/"+id+"/invites", map[string]any{"channels": []string{crew}}, amina, http.StatusNotFound)

	mustCall(t, handler, http.MethodPost, "/api/events/"+id+"/invites", map[string]any{"channels": []string{crew}}, host, http.StatusOK)
	mustCall(t, handler, http.MethodGet, "/api/events/"+id, nil, amina, http.StatusOK)
	mustCall(t, handler, http.MethodGet, "/api/events/"+id, nil, baraka, http.StatusNotFound)
	if !slices.Contains(reasonsOf(t, handler, amina), "event_invite") {
		t.Error("the invited channel's member was not told")
	}
}

/*
One reminder per occurrence and lead, however many passes; none for a muted
event, and none for somebody who is not going.
*/
func TestRemindersGoOnceToThePeopleGoing(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	host := registerAndSignIn(t, handler, "host")
	amina := registerAndSignIn(t, handler, "amina")
	baraka := registerAndSignIn(t, handler, "baraka")
	chausiku := registerAndSignIn(t, handler, "chausiku")
	setSetting(t, handler, admin, "events.reminder_minutes", "60")

	// Scheduled long ago, as far as the reminder rule is concerned: a lead
	// that falls before an event was scheduled is skipped.
	start := time.Now().Add(30 * time.Minute).UTC().Truncate(time.Second)
	id := makeEvent(t, handler, host, map[string]any{
		"startsAt": start.Format(time.RFC3339), "endsAt": start.Add(time.Hour).Format(time.RFC3339),
	})["id"].(string)
	answer(t, handler, amina, id, "going")
	answer(t, handler, baraka, id, "going")
	mustCall(t, handler, http.MethodPut, "/api/me/preferences/notifications",
		map[string]any{"level": "all", "muted": true, "room": id}, baraka, http.StatusNoContent)

	server := serverBehind(t, handler)
	if sent := server.RemindOfEvents(context.Background(), time.Now()); sent != 0 {
		t.Fatalf("a reminder was sent for an event scheduled inside its lead: %d", sent)
	}
	backdate(t, id, 2*time.Hour)

	first := server.RemindOfEvents(context.Background(), time.Now())
	second := server.RemindOfEvents(context.Background(), time.Now())
	server.WaitForBackground()
	if first != 1 || second != 0 {
		t.Fatalf("two passes sent %d and %d reminders, want 1 and 0", first, second)
	}
	if !slices.Contains(reasonsOf(t, handler, amina), "event_reminder") {
		t.Error("amina, going, was not reminded")
	}
	if slices.Contains(reasonsOf(t, handler, baraka), "event_reminder") {
		t.Error("baraka muted the event and was reminded")
	}
	if slices.Contains(reasonsOf(t, handler, chausiku), "event_reminder") {
		t.Error("chausiku is not going and was reminded")
	}
	if !slices.Contains(timelineTypes(t, handler, host, id), "event.reminder") {
		t.Error("the reminder is not in the event's log")
	}
}

// backdate makes an event look as if it was scheduled some time ago.
func backdate(t *testing.T, id string, by time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := poolForTest.Exec(ctx, `UPDATE calendar_events SET created_at = created_at - $2::interval WHERE id = $1`,
		id, by.String()); err != nil {
		t.Fatalf("backdating: %v", err)
	}
}
