package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

/*
A person's feed holds the events they are going to and their channels'
events, decided for them at every fetch; leaving a channel takes its events
out, and revoking the address refuses it. Sabotage-verified: serving a feed
without deciding visibility for its owner puts the invite-only event in it.
*/
func TestAPersonalFeedFollowsItsOwner(t *testing.T) {
	handler := newTestAPI(t)
	_ = completeSetup(t, handler)
	host := registerAndSignIn(t, handler, "host")
	amina := registerAndSignIn(t, handler, "amina")
	crew := makeRoom(t, handler, host, "crew", "public")
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+crew+"/join", nil, amina, http.StatusOK)

	going := makeEvent(t, handler, host, map[string]any{"title": "Going, with a comma; and a semicolon"})["id"].(string)
	inCrew := makeEvent(t, handler, host, map[string]any{"title": "Crew standup", "channelId": crew, "visibility": "channel"})["id"].(string)
	closed := makeEvent(t, handler, host, map[string]any{"title": "Not for amina", "channelId": crew, "visibility": "invite"})["id"].(string)
	answer(t, handler, amina, going, "going")

	made := decode(t, mustCall(t, handler, http.MethodPost, "/api/me/calendar-feed", nil, amina, http.StatusCreated))
	path := made["path"].(string)
	if !strings.HasPrefix(path, "/api/calendar/feeds/tdcf_") {
		t.Fatalf("the feed's address is %q", path)
	}
	if status := decode(t, mustCall(t, handler, http.MethodGet, "/api/me/calendar-feed", nil, amina, http.StatusOK)); status["path"] != nil {
		t.Error("the secret was shown again")
	}

	feed := call(t, handler, http.MethodGet, path, nil, "")
	body := feed.Body.String()
	if feed.Code != http.StatusOK || !strings.HasPrefix(feed.Header().Get("Content-Type"), "text/calendar") {
		t.Fatalf("fetching the feed: %d %s", feed.Code, feed.Header().Get("Content-Type"))
	}
	for _, want := range []string{"BEGIN:VCALENDAR", "UID:" + going, "UID:" + inCrew, `SUMMARY:Going\, with a comma\; and a semicolon`} {
		if !strings.Contains(body, want) {
			t.Errorf("the feed lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, closed) {
		t.Error("an invite-only event in her channel, to which amina was not invited, is in her feed")
	}
	if !strings.Contains(body, "\r\n") {
		t.Error("the feed's lines do not end in CRLF")
	}

	mustCall(t, handler, http.MethodPost, "/api/rooms/"+crew+"/leave", nil, amina, http.StatusNoContent)
	if body := call(t, handler, http.MethodGet, path, nil, "").Body.String(); strings.Contains(body, inCrew) {
		t.Error("a channel amina left still has its event in her feed")
	}

	mustCall(t, handler, http.MethodDelete, "/api/me/calendar-feed", nil, amina, http.StatusNoContent)
	mustCall(t, handler, http.MethodGet, path, nil, "", http.StatusNotFound)
	mustCall(t, handler, http.MethodGet, "/api/calendar/feeds/tdcf_guess/events.ics", nil, "", http.StatusNotFound)
}

// The public feed is off until the operator publishes it, and then holds
// public events only.
func TestThePublicFeedIsTheOperatorsChoice(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	public := makeEvent(t, handler, admin, map[string]any{"title": "Open"})["id"].(string)
	private := makeEvent(t, handler, admin, map[string]any{"title": "Closed", "visibility": "invite"})["id"].(string)

	mustCall(t, handler, http.MethodGet, "/api/calendar/public.ics", nil, "", http.StatusNotFound)
	setSetting(t, handler, admin, "events.public_feed", "true")
	body := mustCall(t, handler, http.MethodGet, "/api/calendar/public.ics", nil, "", http.StatusOK).Body.String()
	if !strings.Contains(body, public) || strings.Contains(body, private) {
		t.Errorf("the public feed reads:\n%s", body)
	}
	one := mustCall(t, handler, http.MethodGet, "/api/events/"+private+"/ics", nil, admin, http.StatusOK).Body.String()
	if !strings.Contains(one, "UID:"+private) {
		t.Error("one event's file does not carry it")
	}
}

/*
A channel's webhooks hear about its events, from each event's own
conversation — and never about an invite-only one, whose channel's readers
may not be people it was shown to. Sabotage-verified: forwarding invite-only
events delivers the second title.
*/
func TestAChannelsWebhooksHearAboutItsEvents(t *testing.T) {
	handler, owner, roomID := webhookFixture(t, nil)
	endpoint := newReceiver(t, answering(http.StatusNoContent))
	subscribe(t, handler, owner, roomID, endpoint.URL, "event.created", "event.cancelled")

	open := makeEvent(t, handler, owner, map[string]any{"title": "Open meetup", "channelId": roomID})["id"].(string)
	makeEvent(t, handler, owner, map[string]any{"title": "Closed meetup", "channelId": roomID, "visibility": "invite"})
	mustCall(t, handler, http.MethodPost, "/api/events/"+open+"/cancel", map[string]any{"reason": "rain"}, owner, http.StatusOK)

	eventually(t, 5*time.Second, "two deliveries", func() bool { return len(endpoint.deliveries()) >= 2 })
	time.Sleep(300 * time.Millisecond)
	types := []string{}
	for _, got := range endpoint.deliveries() {
		var payload struct {
			Type  string `json:"type"`
			Event struct {
				Content map[string]any `json:"content"`
			} `json:"event"`
		}
		if err := json.Unmarshal(got.body, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Event.Content["title"] == "Closed meetup" {
			t.Error("an invite-only event was delivered to the channel's webhook")
		}
		types = append(types, payload.Type)
	}
	if len(types) != 2 || types[0] != "event.created" || types[1] != "event.cancelled" {
		t.Errorf("the channel's webhook received %v", types)
	}
}

// A channel's reader hears of its events without being part of any: the check
// before sending asks about the channel, not the event's own conversation.
func TestAChannelReaderOutsideAnEventStillHearsOfIt(t *testing.T) {
	handler, owner, roomID := webhookFixture(t, nil)
	reader := registerAndSignIn(t, handler, "reader")
	joinRoom(t, handler, reader, roomID)
	endpoint := newReceiver(t, answering(http.StatusNoContent))
	subscribe(t, handler, reader, roomID, endpoint.URL, "event.created")

	makeEvent(t, handler, owner, map[string]any{"title": "Open meetup", "channelId": roomID})

	eventually(t, 5*time.Second, "a delivery", func() bool { return len(endpoint.deliveries()) >= 1 })
}
