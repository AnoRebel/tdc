package calendar

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
	"uuid"
)

/*
Writing iCalendar (RFC 5545).

Written here rather than through a library's object model because what goes
out has to be exactly right for calendars that are strict about it, and every
line of that is a decision worth being able to read: an event that happens once
is written in UTC, which no reader can get wrong; a repeating event is written
in its own zone, with that zone's definition beside it, because a repeat
expanded in UTC drifts by an hour at every change of the clocks.
*/

// Export is what a calendar file is made of.
type Export struct {
	// Name is what a calendar application calls the subscription.
	Name string
	// Link is where each event's page is, or nil for none.
	Link   func(id uuid.UUID) string
	Events []Listed
	// Now stamps the file.
	Now time.Time
}

const icsDate = "20060102"
const icsLocal = "20060102T150405"
const icsUTC = "20060102T150405Z"

// Write writes a whole VCALENDAR.
func (e Export) Write(w io.Writer) error {
	out := &icsWriter{w: w}
	out.line("BEGIN:VCALENDAR")
	out.line("VERSION:2.0")
	out.line("PRODID:-//TDC//Events//EN")
	out.line("CALSCALE:GREGORIAN")
	out.line("METHOD:PUBLISH")
	if e.Name != "" {
		out.line("X-WR-CALNAME:" + escapeText(e.Name))
	}

	// Every zone a repeating event is written in is defined once.
	zones := []string{}
	for _, event := range e.Events {
		if writtenInZone(event) && !slices.Contains(zones, event.TimeZone) {
			zones = append(zones, event.TimeZone)
		}
	}
	slices.Sort(zones)
	for _, name := range zones {
		if zone, err := time.LoadLocation(name); err == nil {
			writeZone(out, name, zone, e.Now)
		}
	}
	for _, event := range e.Events {
		e.writeEvent(out, event)
	}
	out.line("END:VCALENDAR")
	return out.err
}

// writtenInZone is whether an event's times are written as wall-clock times
// in its zone: a repeating one, which has to be. All-day events are dates,
// which belong to no zone.
func writtenInZone(event Listed) bool {
	return event.Recurrence != "" && !event.AllDay
}

func (e Export) writeEvent(out *icsWriter, event Listed) {
	zone := SeriesOf(event.CalendarEvent).Zone
	out.line("BEGIN:VEVENT")
	out.line("UID:" + escapeText(event.Uid))
	out.line("DTSTAMP:" + event.UpdatedAt.UTC().Format(icsUTC))
	out.line("CREATED:" + event.CreatedAt.UTC().Format(icsUTC))
	out.line("LAST-MODIFIED:" + event.UpdatedAt.UTC().Format(icsUTC))
	out.line(fmt.Sprintf("SEQUENCE:%d", event.Sequence))
	out.line("SUMMARY:" + escapeText(event.Title))

	switch {
	case event.AllDay:
		out.line("DTSTART;VALUE=DATE:" + event.StartsAt.In(zone).Format(icsDate))
		out.line("DTEND;VALUE=DATE:" + event.EndsAt.In(zone).Format(icsDate))
	case writtenInZone(event):
		out.line("DTSTART;TZID=" + event.TimeZone + ":" + event.StartsAt.In(zone).Format(icsLocal))
		out.line("DTEND;TZID=" + event.TimeZone + ":" + event.EndsAt.In(zone).Format(icsLocal))
	default:
		out.line("DTSTART:" + event.StartsAt.UTC().Format(icsUTC))
		out.line("DTEND:" + event.EndsAt.UTC().Format(icsUTC))
	}
	if event.Recurrence != "" {
		out.line("RRULE:" + ruleFor(event, zone))
		for _, skipped := range event.Exceptions {
			switch {
			case event.AllDay:
				out.line("EXDATE;VALUE=DATE:" + skipped.In(zone).Format(icsDate))
			default:
				out.line("EXDATE;TZID=" + event.TimeZone + ":" + skipped.In(zone).Format(icsLocal))
			}
		}
	}

	description := event.Description
	if event.OnlineUrl != "" {
		description = strings.TrimSpace(description + "\n\n" + event.OnlineUrl)
	}
	if description != "" {
		out.line("DESCRIPTION:" + escapeText(description))
	}
	if event.Location != "" {
		out.line("LOCATION:" + escapeText(event.Location))
	} else if event.OnlineUrl != "" {
		out.line("LOCATION:" + escapeText(event.OnlineUrl))
	}
	if e.Link != nil {
		if link := e.Link(event.ID); link != "" {
			out.line("URL:" + link)
		}
	}
	out.line("CATEGORIES:" + escapeText(strings.ReplaceAll(event.Kind, "_", " ")))
	if event.CancelledAt != nil {
		out.line("STATUS:CANCELLED")
	} else {
		out.line("STATUS:CONFIRMED")
	}
	out.line("END:VEVENT")
}

/*
ruleFor is the repeat as written in the file. An all-day series' end is a
date, not an instant, because RFC 5545 requires UNTIL to be the same kind of
value as the start.
*/
func ruleFor(event Listed, zone *time.Location) string {
	if !event.AllDay {
		return event.Recurrence
	}
	parts := strings.Split(event.Recurrence, ";")
	for i, part := range parts {
		if value, ok := strings.CutPrefix(part, "UNTIL="); ok {
			if until, err := time.Parse(icsUTC, value); err == nil {
				parts[i] = "UNTIL=" + until.In(zone).Format(icsDate)
			}
		}
	}
	return strings.Join(parts, ";")
}

/*
writeZone defines a zone for the readers that want every TZID defined, from
the rules it follows now: one STANDARD for a zone with no daylight saving, and
otherwise a STANDARD and a DAYLIGHT, each a yearly "nth weekday of a month at a
time" rule, which is how every zone with daylight saving in use describes it.
*/
func writeZone(out *icsWriter, name string, zone *time.Location, now time.Time) {
	out.line("BEGIN:VTIMEZONE")
	out.line("TZID:" + name)
	changes := transitions(zone, now.Year())
	if len(changes) == 0 {
		abbreviation, offset := now.In(zone).Zone()
		out.line("BEGIN:STANDARD")
		out.line("DTSTART:19700101T000000")
		out.line("TZOFFSETFROM:" + offsetText(offset))
		out.line("TZOFFSETTO:" + offsetText(offset))
		out.line("TZNAME:" + abbreviation)
		out.line("END:STANDARD")
	}
	for _, change := range changes {
		kind := "STANDARD"
		if change.to > change.from {
			kind = "DAYLIGHT"
		}
		out.line("BEGIN:" + kind)
		out.line("DTSTART:" + change.firstIn1970().Format(icsLocal))
		out.line(fmt.Sprintf("RRULE:FREQ=YEARLY;BYMONTH=%d;BYDAY=%s", int(change.local.Month()), change.byDay()))
		out.line("TZOFFSETFROM:" + offsetText(change.from))
		out.line("TZOFFSETTO:" + offsetText(change.to))
		out.line("TZNAME:" + change.name)
		out.line("END:" + kind)
	}
	out.line("END:VTIMEZONE")
}

type transition struct {
	from, to int
	name     string
	// local is the wall-clock time the change happens at, in the offset in
	// force before it.
	local time.Time
}

var weekdays = []string{"SU", "MO", "TU", "WE", "TH", "FR", "SA"}

// ordinal is which of the month's weekdays it falls on: -1 for the last.
func (t transition) ordinal() int {
	day := t.local.Day()
	if day+7 > daysIn(t.local.Year(), t.local.Month()) {
		return -1
	}
	return (day-1)/7 + 1
}

func (t transition) byDay() string {
	return fmt.Sprintf("%d%s", t.ordinal(), weekdays[t.local.Weekday()])
}

// firstIn1970 is when the rule first applied in 1970, the conventional start.
func (t transition) firstIn1970() time.Time {
	month := t.local.Month()
	at := func(day int) time.Time {
		return time.Date(1970, month, day, t.local.Hour(), t.local.Minute(), 0, 0, time.UTC)
	}
	if ordinal := t.ordinal(); ordinal > 0 {
		day := 1
		for at(day).Weekday() != t.local.Weekday() {
			day++
		}
		return at(day + 7*(ordinal-1))
	}
	day := daysIn(1970, month)
	for at(day).Weekday() != t.local.Weekday() {
		day--
	}
	return at(day)
}

func daysIn(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// transitions finds a zone's changes of offset in a year, to the minute.
func transitions(zone *time.Location, year int) []transition {
	var found []transition
	day := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	_, previous := day.In(zone).Zone()
	for range 366 {
		next := day.Add(24 * time.Hour)
		if _, offset := next.In(zone).Zone(); offset != previous {
			low, high := day, next
			for high.Sub(low) > time.Minute {
				middle := low.Add(high.Sub(low) / 2)
				if _, at := middle.In(zone).Zone(); at == previous {
					low = middle
				} else {
					high = middle
				}
			}
			name, to := high.In(zone).Zone()
			found = append(found, transition{
				from: previous, to: to, name: name,
				local: high.Truncate(time.Minute).In(time.FixedZone("", previous)),
			})
			previous = offset
		}
		day = next
	}
	return found
}

func offsetText(seconds int) string {
	sign := "+"
	if seconds < 0 {
		sign, seconds = "-", -seconds
	}
	return fmt.Sprintf("%s%02d%02d", sign, seconds/3600, seconds%3600/60)
}

// escapeText escapes a TEXT value: backslashes, separators and line breaks.
func escapeText(text string) string {
	return strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\r\n", `\n`, "\n", `\n`, "\r", `\n`).Replace(text)
}

// icsWriter writes content lines, folded at 75 octets without splitting a
// character, ending each with CRLF.
type icsWriter struct {
	w   io.Writer
	err error
}

func (o *icsWriter) line(text string) {
	if o.err != nil {
		return
	}
	var folded strings.Builder
	width := 0
	for _, r := range text {
		size := utf8.RuneLen(r)
		if width+size > 75 {
			folded.WriteString("\r\n ")
			width = 1
		}
		folded.WriteRune(r)
		width += size
	}
	folded.WriteString("\r\n")
	_, o.err = io.WriteString(o.w, folded.String())
}
