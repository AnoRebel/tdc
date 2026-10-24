package api

import (
	"encoding/json"
	"sync"
	"testing"
)

/*
What is worth interrupting somebody for.

These are the rules the live bridge applies before raising a banner, and every
one of them is a way to annoy somebody: a notification for their own message, a
notification for a message that has been removed, a notification for a reaction.
None of them is visible from a screenshot, so they are written down here.

Whether the conversation is already on screen is deliberately not among them —
the server knows which room each socket is watching and does not send an event
somebody is looking at arrive, which is the right place for that decision
because the web client needs it too.
*/

type recordedBanner struct {
	title, body string
	target      Target
}

type spyNotifier struct {
	mu    sync.Mutex
	shown []recordedBanner
}

func (s *spyNotifier) Show(title, body string, target Target) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shown = append(s.shown, recordedBanner{title, body, target})
	return nil
}

func (s *spyNotifier) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.shown)
}

/** A frame as the server sends one, so these read like what actually arrives. */
func messageFrame(sender, body string, edit func(map[string]any)) []byte {
	payload := map[string]any{
		"id":      "evt-1",
		"type":    "message.sent",
		"sender":  sender,
		"content": map[string]any{"body": body},
	}
	frame := map[string]any{"type": "event", "roomId": "room-1", "payload": payload}
	if edit != nil {
		edit(frame)
	}
	raw, _ := json.Marshal(frame)
	return raw
}

func bridgeWith(t *testing.T, signedInAs string) (*LiveBridge, *spyNotifier) {
	t.Helper()
	service := New()
	if signedInAs != "" {
		service.SignedInAs(signedInAs)
	}
	spy := &spyNotifier{}
	return NewLiveBridge(service, func(string, ...any) bool { return true }, spy), spy
}

func TestAMessageFromSomebodyElseIsAnnounced(t *testing.T) {
	bridge, spy := bridgeWith(t, "me@example.test")
	bridge.notify(messageFrame("someone@example.test", "Are you around?", nil))

	if spy.count() != 1 {
		t.Fatalf("expected one banner, got %d", spy.count())
	}

	shown := spy.shown[0]
	if shown.title != "someone@example.test" {
		t.Errorf("the sender names the banner, got %q", shown.title)
	}
	if shown.body != "Are you around?" {
		t.Errorf("the message is the body, got %q", shown.body)
	}
	// A notification is only useful if it takes somebody to what it is about.
	if shown.target.Room != "room-1" || shown.target.Message != "evt-1" {
		t.Errorf("the banner does not know where it leads: %+v", shown.target)
	}
}

/*
The one that makes people turn notifications off.

Somebody who has just pressed Enter does not need to be told they did.
*/
func TestOnesOwnMessageIsNotAnnounced(t *testing.T) {
	bridge, spy := bridgeWith(t, "me@example.test")
	bridge.notify(messageFrame("me@example.test", "Something I typed", nil))

	if spy.count() != 0 {
		t.Fatalf("a banner was raised for this person's own message")
	}
}

// Before anybody has signed in there is nobody to be, so nothing is suppressed
// — which errs towards showing rather than swallowing.
func TestWithNobodySignedInNothingIsSuppressed(t *testing.T) {
	bridge, spy := bridgeWith(t, "")
	bridge.notify(messageFrame("someone@example.test", "Hello", nil))

	if spy.count() != 1 {
		t.Fatalf("expected the banner to be shown, got %d", spy.count())
	}
}

// Signing out forgets who it was, so the next person at this machine is not
// mistaken for the last one.
func TestSigningOutForgetsWhoItWas(t *testing.T) {
	service := New()
	service.SignedInAs("me@example.test")
	if !service.isMe("me@example.test") {
		t.Fatal("the signed-in address was not recorded")
	}

	service.SignedOut()
	if service.isMe("me@example.test") {
		t.Error("the address survived signing out")
	}
}

func TestOnlyAMessageIsAnnounced(t *testing.T) {
	cases := []struct {
		why   string
		frame []byte
	}{
		{
			"a reaction is not somebody saying something",
			messageFrame("someone@example.test", "", func(f map[string]any) {
				f["payload"].(map[string]any)["type"] = "reaction.added"
			}),
		},
		{
			"a control frame is not an event at all",
			messageFrame("someone@example.test", "x", func(f map[string]any) {
				f["type"] = "subscribed"
			}),
		},
		{
			"a removed message has nothing left to announce",
			messageFrame("someone@example.test", "gone", func(f map[string]any) {
				f["payload"].(map[string]any)["redacted"] = true
			}),
		},
		{
			"an empty body would show as a blank banner",
			messageFrame("someone@example.test", "   ", nil),
		},
		{"unreadable JSON says nothing", []byte("{not json")},
	}

	for _, test := range cases {
		bridge, spy := bridgeWith(t, "me@example.test")
		bridge.notify(test.frame)
		if spy.count() != 0 {
			t.Errorf("%s: a banner was raised anyway", test.why)
		}
	}
}

// A build with no notifier shows nothing and carries on, rather than being a
// case every caller has to check for.
func TestWithoutANotifierNothingHappens(t *testing.T) {
	bridge := NewLiveBridge(New(), func(string, ...any) bool { return true }, nil)
	bridge.notify(messageFrame("someone@example.test", "Hello", nil))
}

// Somebody calling is announced, by name, and leads to the conversation.
func TestARingIsAnnounced(t *testing.T) {
	bridge, spy := bridgeWith(t, "me@example.test")
	raw, _ := json.Marshal(map[string]any{
		"type": "call.ring", "roomId": "room-7",
		"payload": map[string]any{
			"call": "call-1", "roomId": "room-7", "roomName": "standup", "kind": "channel",
			"from": map[string]any{"userId": "u1", "username": "amina", "address": "amina@example.test"},
		},
	})
	bridge.notify(raw)

	if spy.count() != 1 {
		t.Fatalf("a ring raised %d banners, want 1", spy.count())
	}
	shown := spy.shown[0]
	if shown.title != "amina is calling" || shown.body != "In #standup" || shown.target.Room != "room-7" {
		t.Errorf("the ring's banner reads %+v", shown)
	}
}

// An invitation into a merged call names who invites, and nothing of the
// conversation the call belongs to.
func TestAConferenceInvitationIsAnnounced(t *testing.T) {
	bridge, spy := bridgeWith(t, "me@example.test")
	raw, _ := json.Marshal(map[string]any{
		"type": "call.ring",
		"payload": map[string]any{
			"call": "call-2", "roomId": "room-9", "kind": "conference", "conference": true,
			"from": map[string]any{"userId": "u1", "username": "amina", "address": "amina@example.test"},
		},
	})
	bridge.notify(raw)
	if spy.count() != 1 || spy.shown[0].body != "Invites you to join a conference call" {
		t.Errorf("the conference invitation's banner reads %+v", spy.shown)
	}
}

// A reminder of an event names it, says when, and leads to the event.
func TestAnEventReminderIsAnnounced(t *testing.T) {
	bridge, spy := bridgeWith(t, "me@example.test")
	raw, _ := json.Marshal(map[string]any{
		"type": "event.reminder", "roomId": "event-3",
		"payload": map[string]any{"event": "event-3", "title": "Go meetup", "minutes": 15},
	})
	bridge.notify(raw)
	if spy.count() != 1 {
		t.Fatalf("a reminder raised %d banners, want 1", spy.count())
	}
	if shown := spy.shown[0]; shown.title != "Go meetup" || shown.body != "Starts in 15 minutes" || shown.target.Room != "event-3" {
		t.Errorf("the reminder's banner reads %+v", shown)
	}
}
