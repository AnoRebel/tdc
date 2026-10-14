/*
Package calendar holds events: meetups, talks, workshops and office hours.

An event belongs to a channel or to the whole server, has a time in a named
zone and perhaps a repeat, and is answered, reminded of, hosted and exchanged
with other calendars. Every event is a room too, of kind "event", whose id is
the event's: its log records what happens to the event, its members are the
people taking part, and its call is the event's call.

Who may see an event is decided in one place, Viewer.Sees, from facts loaded
once per request; every route, the feeds and the reminder pass ask it.
*/
package calendar

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

// Kinds of event.
const (
	KindMeetup      = "meetup"
	KindTalk        = "talk"
	KindWorkshop    = "workshop"
	KindOfficeHours = "office_hours"
	KindOther       = "other"
)

// Kinds lists every kind, in the order a form offers them.
var Kinds = []string{KindMeetup, KindTalk, KindWorkshop, KindOfficeHours, KindOther}

// Who may see an event.
const (
	// VisibilityPublic is everybody on the server.
	VisibilityPublic = "public"
	// VisibilityChannel is the members of the event's channel.
	VisibilityChannel = "channel"
	// VisibilityInvite is the people invited, directly or through a channel.
	VisibilityInvite = "invite"
)

// Answers.
const (
	Going      = "going"
	Maybe      = "maybe"
	Declined   = "declined"
	Waitlisted = "waitlisted"
)

// Roles of the people running an event.
const (
	RoleHost    = "host"
	RoleSpeaker = "speaker"
)

// Limits on what an event holds, counted in characters.
const (
	MaxTitle       = 200
	MaxDescription = 20000
	MaxLocation    = 500
	MaxURL         = 2000
	MaxCapacity    = 100000
	MaxHosts       = 20
	MaxSpeakers    = 50
	MaxSessions    = 50
	MaxExceptions  = 500
	// MaxLength is the longest one occurrence may last: a conference, not a
	// season.
	MaxLength = 31 * 24 * time.Hour
)

var (
	// ErrNotFound is an event that does not exist or that the person may not
	// see. One error for both, so asking learns nothing.
	ErrNotFound = errors.New("no such event")
	// ErrInvalid reports a detail an event cannot have; the message says which.
	ErrInvalid = errors.New("that is not something an event can have")
	// ErrCancelled is acting on an event that has been cancelled.
	ErrCancelled = errors.New("this event has been cancelled")
	// ErrEnded is answering an event that is over.
	ErrEnded = errors.New("this event is over")
	// ErrNotAllowed is a person who may see an event acting on it beyond
	// what they may do.
	ErrNotAllowed = errors.New("you cannot change this event")
)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// Listed is an event with the channel it belongs to, as a list shows it.
type Listed struct {
	dbgen.CalendarEvent
	ChannelName       string
	ChannelVisibility string
}

// Series is the event's repeat, ready to expand.
func (e Listed) Series() Series { return SeriesOf(e.CalendarEvent) }

// SeriesOf reads an event's repeat.
func SeriesOf(event dbgen.CalendarEvent) Series {
	zone, err := time.LoadLocation(event.TimeZone)
	if err != nil {
		zone = time.UTC
	}
	return Series{
		Start: event.StartsAt, End: event.EndsAt, Zone: zone,
		Rule: event.Recurrence, Exceptions: event.Exceptions,
	}
}

// Session is one part of an event's agenda, as written.
type Session struct {
	Title         string
	Description   string
	StartsAt      time.Time
	EndsAt        time.Time
	Speakers      []uuid.UUID
	GuestSpeakers string
}

/*
Draft is an event as somebody asks for it, before it is checked.

People and sessions are pointers to slices so an edit can leave them alone: nil
is "unchanged", an empty slice is "none".
*/
type Draft struct {
	ChannelID   *uuid.UUID
	Kind        string
	Title       string
	Description string
	StartsAt    time.Time
	EndsAt      time.Time
	TimeZone    string
	AllDay      bool
	Recurrence  string
	Exceptions  []time.Time
	Location    string
	Online      bool
	OnlineURL   string
	Capacity    *int32
	Visibility  string

	Hosts    *[]uuid.UUID
	Speakers *[]uuid.UUID
	Sessions *[]Session
}

/*
Normalise checks a draft and puts it in the form it is stored in.

channelVisibility is the channel's own visibility, or empty for an event of the
whole server: a public event may belong only to a public channel, so it can
never disclose the name of a private one.
*/
func (d *Draft) Normalise(channelVisibility string) error {
	d.Title = strings.TrimSpace(d.Title)
	d.Description = strings.TrimSpace(d.Description)
	d.Location = strings.TrimSpace(d.Location)
	d.OnlineURL = strings.TrimSpace(d.OnlineURL)

	if d.Kind == "" {
		d.Kind = KindMeetup
	}
	if !slices.Contains(Kinds, d.Kind) {
		return invalid("a kind is one of %s", strings.Join(Kinds, ", "))
	}
	if d.Title == "" || utf8.RuneCountInString(d.Title) > MaxTitle {
		return invalid("a title is 1 to %d characters", MaxTitle)
	}
	if utf8.RuneCountInString(d.Description) > MaxDescription {
		return invalid("a description is at most %d characters", MaxDescription)
	}
	if utf8.RuneCountInString(d.Location) > MaxLocation {
		return invalid("a place is at most %d characters", MaxLocation)
	}
	if d.OnlineURL != "" {
		parsed, err := url.Parse(d.OnlineURL)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" ||
			len(d.OnlineURL) > MaxURL {
			return invalid("a meeting address is an http or https link")
		}
	}
	if d.Capacity != nil && (*d.Capacity < 1 || *d.Capacity > MaxCapacity) {
		return invalid("a capacity is 1 to %d people, or none", MaxCapacity)
	}

	zone, err := time.LoadLocation(d.TimeZone)
	if err != nil || d.TimeZone == "" || d.TimeZone == "Local" {
		return invalid("%q is not a time zone this server knows", d.TimeZone)
	}
	if d.StartsAt.IsZero() || d.EndsAt.IsZero() {
		return invalid("an event needs a start and an end")
	}
	if d.AllDay {
		d.StartsAt, d.EndsAt = wholeDays(d.StartsAt, d.EndsAt, zone)
	}
	d.StartsAt, d.EndsAt = d.StartsAt.UTC(), d.EndsAt.UTC()
	if d.EndsAt.Before(d.StartsAt) {
		return invalid("an event ends after it starts")
	}
	if d.EndsAt.Sub(d.StartsAt) > MaxLength {
		return invalid("one occurrence lasts at most 31 days")
	}

	rule, err := NormaliseRule(d.Recurrence)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	d.Recurrence = rule
	if len(d.Exceptions) > MaxExceptions {
		return invalid("at most %d skipped occurrences", MaxExceptions)
	}
	if rule == "" {
		d.Exceptions = nil
	}
	for i := range d.Exceptions {
		d.Exceptions[i] = d.Exceptions[i].UTC()
	}
	if d.Exceptions == nil {
		d.Exceptions = []time.Time{}
	}

	switch d.Visibility {
	case "":
		d.Visibility = VisibilityPublic
		if d.ChannelID != nil {
			d.Visibility = VisibilityChannel
		}
	case VisibilityPublic, VisibilityInvite:
	case VisibilityChannel:
		if d.ChannelID == nil {
			return invalid("an event for a channel's members belongs to a channel")
		}
	default:
		return invalid("visibility is public, channel or invite")
	}
	if d.Visibility == VisibilityPublic && d.ChannelID != nil && channelVisibility != "public" {
		return invalid("an event in a private channel cannot be public")
	}

	if d.Hosts != nil && len(*d.Hosts) > MaxHosts {
		return invalid("at most %d hosts", MaxHosts)
	}
	if d.Speakers != nil && len(*d.Speakers) > MaxSpeakers {
		return invalid("at most %d speakers", MaxSpeakers)
	}
	if d.Sessions != nil {
		if len(*d.Sessions) > MaxSessions {
			return invalid("at most %d sessions", MaxSessions)
		}
		for i, session := range *d.Sessions {
			session.Title = strings.TrimSpace(session.Title)
			session.Description = strings.TrimSpace(session.Description)
			session.GuestSpeakers = strings.TrimSpace(session.GuestSpeakers)
			if session.Title == "" || utf8.RuneCountInString(session.Title) > MaxTitle {
				return invalid("a session's title is 1 to %d characters", MaxTitle)
			}
			if utf8.RuneCountInString(session.Description) > MaxDescription/4 ||
				utf8.RuneCountInString(session.GuestSpeakers) > MaxLocation {
				return invalid("a session's description is too long")
			}
			if session.EndsAt.Before(session.StartsAt) ||
				session.StartsAt.Before(d.StartsAt) || session.EndsAt.After(d.EndsAt) {
				return invalid("a session happens within its event")
			}
			if len(session.Speakers) > MaxSpeakers {
				return invalid("at most %d speakers in a session", MaxSpeakers)
			}
			session.StartsAt, session.EndsAt = session.StartsAt.UTC(), session.EndsAt.UTC()
			(*d.Sessions)[i] = session
		}
	}
	return nil
}

/*
wholeDays stretches a span to the days it touches in its zone: from the first
day's midnight to the midnight after the last. An end that already falls on a
midnight is the exclusive end iCalendar uses, and is kept.
*/
func wholeDays(start, end time.Time, zone *time.Location) (time.Time, time.Time) {
	midnight := func(at time.Time) time.Time {
		local := at.In(zone)
		return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, zone)
	}
	from := midnight(start)
	to := midnight(end)
	if !to.Equal(end.In(zone)) || !to.After(from) {
		to = to.AddDate(0, 0, 1)
	}
	return from, to
}
