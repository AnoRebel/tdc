package notify

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Frequency is how often somebody wants a summary of what they missed.
type Frequency string

const (
	// FrequencyOff is no summary: notifications arrive as they are raised.
	// Everybody's setting until they choose otherwise.
	FrequencyOff Frequency = "off"
	// FrequencyDaily is one summary a day, at the hour they chose.
	FrequencyDaily Frequency = "daily"
	// FrequencyWeekly is one a week, on the day and at the hour they chose.
	FrequencyWeekly Frequency = "weekly"
)

// ErrInvalidSchedule is a schedule this server cannot keep.
var ErrInvalidSchedule = errors.New("that is not a schedule a summary can keep")

/*
DigestSchedule is when somebody's summary goes out, as they put it.

Kept as an hour, a day and a zone rather than an instant, because "eight in the
morning" means eight in the morning on both sides of a change to summer time —
and only the zone's own rules know when that is.
*/
type DigestSchedule struct {
	Frequency Frequency
	// Hour is the hour of the day in Zone, 0 to 23.
	Hour int
	// Weekday is the day of a weekly summary. Ignored for a daily one.
	Weekday time.Weekday
	// Zone is an IANA name, such as Africa/Dar_es_Salaam.
	Zone string
}

/*
Validate refuses a schedule that cannot be kept.

The zone must be a real IANA name. "Local" is refused as well as nonsense: it
means wherever this server's process happens to think it is, which is not where
the person is and changes when the server moves.
*/
func (d DigestSchedule) Validate() error {
	switch d.Frequency {
	case FrequencyOff, FrequencyDaily, FrequencyWeekly:
	default:
		return fmt.Errorf(`%w: the frequency must be "off", "daily" or "weekly"`, ErrInvalidSchedule)
	}
	if d.Hour < 0 || d.Hour > 23 {
		return fmt.Errorf("%w: the hour must be from 0 to 23", ErrInvalidSchedule)
	}
	if d.Weekday < time.Sunday || d.Weekday > time.Saturday {
		return fmt.Errorf("%w: the day must be from 0 (Sunday) to 6 (Saturday)", ErrInvalidSchedule)
	}
	if _, err := d.location(); err != nil {
		return err
	}
	return nil
}

func (d DigestSchedule) location() (*time.Location, error) {
	zone := strings.TrimSpace(d.Zone)
	if zone == "" || zone == "Local" || len(zone) > 64 {
		return nil, fmt.Errorf("%w: %q is not a time zone", ErrInvalidSchedule, d.Zone)
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		return nil, fmt.Errorf("%w: %q is not a time zone", ErrInvalidSchedule, d.Zone)
	}
	return location, nil
}

/*
Next is the first moment strictly after the given one that the schedule falls
on, or nil for a schedule that is off.

Built from the calendar date in the person's own zone, with time.Date in that
zone — never by taking a UTC date and setting its hour, which is right only for
people who live at UTC and wrong by their offset for everybody else. Walking
forward a day at a time through time.Date, rather than adding twenty-four
hours, is what keeps eight o'clock at eight o'clock across a change to summer
time, when one of those days is twenty-three hours long.
*/
func (d DigestSchedule) Next(after time.Time) (*time.Time, error) {
	if d.Frequency == FrequencyOff {
		return nil, nil
	}
	if err := d.Validate(); err != nil {
		return nil, err
	}
	location, _ := d.location()

	local := after.In(location)
	// Eight days covers a weekly schedule whose day is today but whose hour
	// has passed.
	for offset := 0; offset <= 7; offset++ {
		candidate := time.Date(local.Year(), local.Month(), local.Day()+offset,
			d.Hour, 0, 0, 0, location)
		if !candidate.After(after) {
			continue
		}
		if d.Frequency == FrequencyWeekly && candidate.Weekday() != d.Weekday {
			continue
		}
		return &candidate, nil
	}

	// Unreachable for a valid schedule; returned rather than panicked so a
	// zone database with something odd in it costs one summary, not a server.
	return nil, fmt.Errorf("%w: no time found within a week", ErrInvalidSchedule)
}

/*
DigestConversation is one line of a summary: a conversation and how much in it
went unread.

Counts, never what was said. A summary leaves this server for somebody's mail
provider, and a message written in a private channel has no business sitting in
a third party's inbox. The person opens the conversation to read it, here,
where its permissions still apply.
*/
type DigestConversation struct {
	// Name is the conversation's name. Empty for a direct conversation, which
	// is described rather than named.
	Name string
	// Direct marks a conversation between people rather than a channel.
	Direct bool
	// Notifications is how many unread notifications it holds.
	Notifications int64
	// People is how many different people are behind them.
	People int64
	// Addressed is how many of them were a mention or a direct message.
	Addressed int64
	// Link opens the conversation.
	Link string
}

// Digest is one summary, ready to send.
type Digest struct {
	Subject string
	Body    string
}

/*
ComposeDigest writes a summary.

Plain text, like everything else this server mails, and in the same voice. More
is the number of further conversations left off the list by the operator's
limit, so the person is told they exist rather than led to think the list was
everything.
*/
func ComposeDigest(
	serverName, recipient string,
	frequency Frequency,
	conversations []DigestConversation,
	more int,
	settingsLink string,
) Digest {
	period := "today"
	if frequency == FrequencyWeekly {
		period = "this week"
	}

	var total int64
	for _, conversation := range conversations {
		total += conversation.Notifications
	}

	var body strings.Builder
	fmt.Fprintf(&body, "Hello %s,\n\n", recipient)
	fmt.Fprintf(&body, "Here is what is waiting for you on %s from %s.\n\n", serverName, period)

	for _, conversation := range conversations {
		label := conversation.Name
		if conversation.Direct || label == "" {
			label = "A direct conversation"
		}

		line := fmt.Sprintf("%s: %s", label, plural(conversation.Notifications, "notification"))
		if conversation.People > 1 {
			line += fmt.Sprintf(" from %d people", conversation.People)
		}
		if conversation.Addressed > 0 && !conversation.Direct {
			line += fmt.Sprintf(", %d addressed to you", conversation.Addressed)
		}
		body.WriteString(line + "\n")
		if conversation.Link != "" {
			body.WriteString("  " + conversation.Link + "\n")
		}
		body.WriteString("\n")
	}

	if more > 0 {
		noun := "conversations"
		if more == 1 {
			noun = "conversation"
		}
		fmt.Fprintf(&body, "And %d more %s.\n\n", more, noun)
	}

	body.WriteString("What was said is not in this message. Open the conversation to read it.\n")
	if settingsLink != "" {
		fmt.Fprintf(&body, "\nTo change how often this arrives, or to stop it:\n%s\n", settingsLink)
	}

	return Digest{
		Subject: fmt.Sprintf("%s waiting for you on %s", plural(total, "notification"), serverName),
		Body:    body.String(),
	}
}

// plural writes a count with its noun, which a summary does a lot of.
func plural(count int64, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", count, noun)
}
