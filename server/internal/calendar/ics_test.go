package calendar

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

func sampleExport() Export {
	london, _ := time.LoadLocation("Europe/London")
	dar, _ := time.LoadLocation("Africa/Dar_es_Salaam")
	weekly := time.Date(2026, time.October, 13, 19, 0, 0, 0, london)
	once := time.Date(2026, time.November, 7, 18, 0, 0, 0, dar)
	day := time.Date(2026, time.December, 1, 0, 0, 0, 0, dar)
	cancelled := time.Now()
	stamp := time.Date(2026, time.October, 1, 9, 0, 0, 0, time.UTC)
	event := func(title string, start, end time.Time, zone, rule string) Listed {
		return Listed{CalendarEvent: dbgen.CalendarEvent{
			ID: uuid.New(), Kind: KindMeetup, Title: title, StartsAt: start.UTC(), EndsAt: end.UTC(),
			TimeZone: zone, Recurrence: rule, Uid: uuid.NewString() + "@tdc.test",
			CreatedAt: stamp, UpdatedAt: stamp,
		}}
	}
	series := event("London Go night", weekly, weekly.Add(2*time.Hour), "Europe/London", "FREQ=WEEKLY;COUNT=6")
	series.Exceptions = []time.Time{weekly.AddDate(0, 0, 14).UTC()}
	single := event("Dar meetup, with a comma; a semicolon and a long description that runs past the seventy-five octet line limit so it has to be folded", once, once.Add(3*time.Hour), "Africa/Dar_es_Salaam", "")
	single.Description = "Line one\nLine two — with a dash"
	single.Location = "Kijitonyama, Dar es Salaam"
	allDay := event("Hackathon", day, day.AddDate(0, 0, 2), "Africa/Dar_es_Salaam", "")
	allDay.AllDay = true
	off := event("Called off", once.AddDate(0, 0, 7), once.AddDate(0, 0, 7).Add(time.Hour), "Africa/Dar_es_Salaam", "")
	off.CancelledAt = &cancelled
	return Export{
		Name: "TDC", Now: time.Date(2026, time.October, 13, 0, 0, 0, 0, time.UTC),
		Link:   func(id uuid.UUID) string { return "https://tdc.test/events/" + id.String() },
		Events: []Listed{series, single, allDay, off},
	}
}

func TestTheExportIsWellFormed(t *testing.T) {
	var out bytes.Buffer
	if err := sampleExport().Write(&out); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	// Written for a second implementation to read, when asked.
	if path := os.Getenv("TDC_ICS_SAMPLE"); path != "" {
		_ = os.WriteFile(path, out.Bytes(), 0o600)
	}
	for _, line := range strings.Split(strings.TrimSuffix(text, "\r\n"), "\r\n") {
		if len(line) > 75 {
			t.Errorf("a line of %d octets: %q", len(line), line)
		}
	}
	for _, want := range []string{
		"BEGIN:VTIMEZONE\r\nTZID:Europe/London",
		"BEGIN:DAYLIGHT", "RRULE:FREQ=YEARLY;BYMONTH=3;BYDAY=-1SU", "RRULE:FREQ=YEARLY;BYMONTH=10;BYDAY=-1SU",
		"DTSTART;TZID=Europe/London:20261013T190000",
		"EXDATE;TZID=Europe/London:20261027T190000",
		"DTSTART:20261107T150000Z",
		"DTSTART;VALUE=DATE:20261201", "DTEND;VALUE=DATE:20261203",
		"STATUS:CANCELLED",
		`DESCRIPTION:Line one\nLine two`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the export lacks %q", want)
		}
	}
	// A zone with no daylight saving is defined by one fixed offset; and only
	// zones a repeating event is written in are defined at all.
	if strings.Contains(text, "TZID:Africa/Dar_es_Salaam") {
		t.Error("a zone no repeating event uses was defined")
	}
}
