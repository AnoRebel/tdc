package events

/*
What happens to an event, in its own conversation's log.

An event is a room too, and these are entries in that room. Kept in the log
rather than only in the event's row for the reason everything is: a
notification points at an entry, a webhook delivers one, and a person reading
the event's conversation sees what changed and when.
*/
const (
	// TypeEventCreated records an event being scheduled.
	TypeEventCreated Type = "event.created"
	// TypeEventUpdated records a change to its details; the content names
	// which ones.
	TypeEventUpdated Type = "event.updated"
	// TypeEventCancelled records its cancellation, with the reason given.
	TypeEventCancelled Type = "event.cancelled"
	// TypeEventRSVP records somebody answering — only somebody who lets others
	// see the events they go to.
	TypeEventRSVP Type = "event.rsvp"
	// TypeEventReminder records that the people taking part were reminded of
	// an occurrence, sent by the server rather than by anybody.
	TypeEventReminder Type = "event.reminder"
)
