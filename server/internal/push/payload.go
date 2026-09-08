package push

import "strings"

/*
Deciding what a notification is allowed to say.

A push payload is shown on a lock screen: to whoever is holding the phone, and
to anybody standing beside them. Somebody in a private conversation may want to
know something arrived without the carriage announcing what it said.

Separate from delivery because it is the decision, and delivery is the
plumbing. This is arithmetic on strings and a boolean, and it is where a
mistake would leak.
*/

// Describing is what a notification may disclose.
type Describing struct {
	// Sender is who sent it, as a display name.
	Sender string
	// Room is the conversation's name.
	Room string
	// Body is what was said.
	Body string
	// Private marks a direct conversation or a private channel — somewhere
	// whose contents are not already visible to everybody on the server.
	Private bool
	// HideContent is the recipient's own choice to keep messages off a lock
	// screen.
	HideContent bool
}

/*
maxBodyLength bounds what a notification carries.

A phone truncates anyway, and a payload is limited by the push service. Cutting
it here means the cut happens where somebody chose the wording rather than
wherever the bytes ran out.
*/
const maxBodyLength = 140

/*
Describe turns a message into what may be said about it.

The rule: content is withheld when the recipient asked for it to be, and the
notification then says only that something arrived and where. Who sent it is
still named for a channel, because a channel's membership is not the secret —
but a private conversation withholds even that, since "Amina sent you
something" at two in the morning discloses the thing worth hiding.
*/
func Describe(about Describing) Payload {
	if !about.HideContent {
		return Payload{
			Title: titleFor(about),
			Body:  truncate(about.Body, maxBodyLength),
		}
	}

	/*
		Content hidden.

		A private conversation says nothing about who: the fact of the
		correspondence is what somebody hiding content is usually hiding. A
		channel names itself, because anybody who can see the phone can see the
		channel exists anyway.
	*/
	if about.Private {
		return Payload{Title: "New message", Body: "Open TDC to read it"}
	}

	return Payload{
		Title: about.Room,
		Body:  "New message",
	}
}

func titleFor(about Describing) string {
	if about.Private || about.Room == "" {
		return about.Sender
	}
	// "Amina in general", which says both things somebody needs to decide
	// whether to look now.
	return about.Sender + " in " + about.Room
}

func truncate(text string, limit int) string {
	trimmed := strings.TrimSpace(text)

	runes := []rune(trimmed)
	if len(runes) <= limit {
		return trimmed
	}
	// An ellipsis rather than a hard cut, so a truncated line reads as
	// truncated rather than as somebody trailing off.
	return strings.TrimSpace(string(runes[:limit-1])) + "…"
}
