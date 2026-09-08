package push_test

import (
	"strings"
	"testing"

	"github.com/AnoRebel/tdc/server/internal/push"
)

/*
The scenario 8.6 names, and the one where a mistake actually costs something.

A push payload is shown on a lock screen — to whoever holds the phone, and to
anybody standing beside them. Somebody who asked for content to be hidden has
asked for exactly this, and a payload that carried the message anyway would
have broken the promise in the place it was made.
*/
func TestHiddenContentNeverCarriesTheMessage(t *testing.T) {
	secret := "Nitakutumia pesa kesho asubuhi"

	for _, about := range []push.Describing{
		{Sender: "Amina", Room: "general", Body: secret, HideContent: true},
		{Sender: "Amina", Body: secret, Private: true, HideContent: true},
		{Sender: "Amina", Room: "siri", Body: secret, Private: true, HideContent: true},
	} {
		payload := push.Describe(about)

		if strings.Contains(payload.Title, secret) || strings.Contains(payload.Body, secret) {
			t.Errorf("a hidden notification carried the message: %+v", payload)
		}
	}
}

/*
A private conversation hides who as well as what.

"Amina sent you something" at two in the morning discloses the thing somebody
hiding content is usually hiding — that the correspondence is happening at all.
*/
func TestAHiddenPrivateConversationDoesNotNameTheSender(t *testing.T) {
	payload := push.Describe(push.Describing{
		Sender:      "Amina",
		Body:        "Habari",
		Private:     true,
		HideContent: true,
	})

	if strings.Contains(payload.Title, "Amina") || strings.Contains(payload.Body, "Amina") {
		t.Errorf("a hidden private notification named the sender: %+v", payload)
	}
}

/*
A channel still names itself when content is hidden.

Anybody who can see the phone can see which channels exist, so withholding the
name buys nothing and costs somebody the ability to tell an urgent channel from
a quiet one without unlocking.
*/
func TestAHiddenChannelStillSaysWhere(t *testing.T) {
	payload := push.Describe(push.Describing{
		Sender:      "Amina",
		Room:        "matukio",
		Body:        "Habari",
		HideContent: true,
	})

	if !strings.Contains(payload.Title, "matukio") {
		t.Errorf("a hidden channel notification does not say where: %+v", payload)
	}
}

// The ordinary case: somebody who has not asked for hiding sees the message.
func TestAnOrdinaryNotificationCarriesTheMessage(t *testing.T) {
	payload := push.Describe(push.Describing{
		Sender: "Amina",
		Room:   "general",
		Body:   "Tutaonana kesho",
	})

	if !strings.Contains(payload.Body, "Tutaonana kesho") {
		t.Errorf("an ordinary notification lost its message: %+v", payload)
	}
	if !strings.Contains(payload.Title, "Amina") {
		t.Errorf("an ordinary notification does not say who: %+v", payload)
	}
	if !strings.Contains(payload.Title, "general") {
		t.Errorf("a channel notification does not say where: %+v", payload)
	}
}

/*
A long message is cut here rather than wherever the bytes ran out.

A push service bounds the payload, and a phone truncates for its own reasons.
Cutting deliberately means the cut lands where somebody chose the wording.
*/
func TestALongMessageIsCutDeliberately(t *testing.T) {
	long := strings.Repeat("neno ", 200)

	payload := push.Describe(push.Describing{Sender: "Amina", Body: long})

	if len([]rune(payload.Body)) > 140 {
		t.Errorf("the body is %d characters, which is more than was intended",
			len([]rune(payload.Body)))
	}
	if !strings.HasSuffix(payload.Body, "…") {
		t.Error("a truncated line does not read as truncated, so it looks " +
			"like somebody trailing off")
	}
}

// A message that fits is left exactly as it was written.
func TestAShortMessageIsNotAltered(t *testing.T) {
	payload := push.Describe(push.Describing{Sender: "Amina", Body: "Ndiyo"})

	if payload.Body != "Ndiyo" {
		t.Errorf("a short message became %q", payload.Body)
	}
}

/*
Gone is what separates a subscription that is finished from a service having a
bad afternoon.

Removing on a 500 would throw away devices whose push service was briefly
unwell; retrying a 410 forever would mean a request per notification for an
account that has uninstalled the application.
*/
func TestOnlyAFinalAnswerRetiresASubscription(t *testing.T) {
	for _, status := range []int{404, 410} {
		if !push.Gone(status) {
			t.Errorf("%d should retire a subscription", status)
		}
	}

	for _, status := range []int{200, 201, 400, 429, 500, 502, 503} {
		if push.Gone(status) {
			t.Errorf("%d should not retire a subscription", status)
		}
	}
}

/*
Keys an operator can generate themselves.

The requirement is that push works with no commercial service configured, and
this is what makes that true: a pair generated here, by this server, with no
account anywhere.
*/
func TestKeysCanBeGeneratedWithoutAnybodyElse(t *testing.T) {
	keys, err := push.GenerateKeys()
	if err != nil {
		t.Fatalf("GenerateKeys: %v", err)
	}

	if keys.Public == "" || keys.Private == "" {
		t.Fatal("a generated pair is missing a half")
	}

	// Two calls must not produce the same pair, or every server sharing this
	// software would share an identity.
	second, err := push.GenerateKeys()
	if err != nil {
		t.Fatalf("GenerateKeys: %v", err)
	}
	if keys.Private == second.Private {
		t.Error("two generated pairs are identical")
	}
}
