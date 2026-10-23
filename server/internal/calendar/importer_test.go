package calendar

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// A file shaped like the ones Google Calendar and Outlook export.
const exported = "BEGIN:VCALENDAR\r\n" +
	"VERSION:2.0\r\nPRODID:-//Google Inc//Google Calendar 70.9054//EN\r\n" +
	"X-WR-TIMEZONE:Africa/Dar_es_Salaam\r\n" +
	"BEGIN:VTIMEZONE\r\nTZID:Custom Nairobi\r\nBEGIN:STANDARD\r\nDTSTART:19700101T000000\r\n" +
	"TZOFFSETFROM:+0300\r\nTZOFFSETTO:+0300\r\nEND:STANDARD\r\nEND:VTIMEZONE\r\n" +
	// Weekly, in London, with a skipped week.
	"BEGIN:VEVENT\r\nUID:weekly@example.com\r\nSUMMARY:Go night\\, weekly\r\n" +
	"DTSTART;TZID=Europe/London:20261013T190000\r\nDTEND;TZID=Europe/London:20261013T210000\r\n" +
	"RRULE:FREQ=WEEKLY;COUNT=6\r\nEXDATE;TZID=Europe/London:20261027T190000\r\nEND:VEVENT\r\n" +
	// Outlook's name for East Africa.
	"BEGIN:VEVENT\r\nUID:outlook@example.com\r\nSUMMARY:Outlook meeting\r\n" +
	"DTSTART;TZID=E. Africa Standard Time:20261107T180000\r\nDURATION:PT90M\r\nEND:VEVENT\r\n" +
	// A zone the file defines for itself.
	"BEGIN:VEVENT\r\nUID:custom@example.com\r\nSUMMARY:Custom zone\r\n" +
	"DTSTART;TZID=Custom Nairobi:20261108T090000\r\nDTEND;TZID=Custom Nairobi:20261108T100000\r\nEND:VEVENT\r\n" +
	// All day.
	"BEGIN:VEVENT\r\nUID:allday@example.com\r\nSUMMARY:Hackathon\r\n" +
	"DTSTART;VALUE=DATE:20261201\r\nDTEND;VALUE=DATE:20261203\r\nEND:VEVENT\r\n" +
	// A repeat events do not support.
	"BEGIN:VEVENT\r\nUID:yearly@example.com\r\nSUMMARY:Anniversary\r\n" +
	"DTSTART:20261201T090000Z\r\nDTEND:20261201T100000Z\r\nRRULE:FREQ=YEARLY\r\nSTATUS:CANCELLED\r\nEND:VEVENT\r\n" +
	// A changed occurrence, which is left out.
	"BEGIN:VEVENT\r\nUID:weekly@example.com\r\nRECURRENCE-ID;TZID=Europe/London:20261020T190000\r\n" +
	"SUMMARY:Moved\r\nDTSTART;TZID=Europe/London:20261021T190000\r\nDTEND;TZID=Europe/London:20261021T210000\r\nEND:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

func TestImportedEventsKeepTheirZonesAndRepeats(t *testing.T) {
	incoming, err := ParseICS([]byte(exported))
	if err != nil {
		t.Fatal(err)
	}
	byUID := map[string]Incoming{}
	for _, item := range incoming {
		byUID[item.UID] = item
	}
	if len(byUID) != 5 || len(incoming) != 5 {
		t.Fatalf("read %d events: %v", len(incoming), byUID)
	}

	weekly := byUID["weekly@example.com"].Draft
	london, _ := time.LoadLocation("Europe/London")
	if weekly.TimeZone != "Europe/London" || !weekly.StartsAt.Equal(time.Date(2026, 10, 13, 19, 0, 0, 0, london)) ||
		weekly.Recurrence != "FREQ=WEEKLY;COUNT=6" || len(weekly.Exceptions) != 1 || weekly.Title != "Go night, weekly" {
		t.Errorf("the weekly event read as %+v", weekly)
	}
	outlook := byUID["outlook@example.com"].Draft
	if outlook.TimeZone != "Africa/Nairobi" || !outlook.StartsAt.Equal(time.Date(2026, 11, 7, 15, 0, 0, 0, time.UTC)) ||
		outlook.EndsAt.Sub(outlook.StartsAt) != 90*time.Minute {
		t.Errorf("the Outlook event read as %s %v %v", outlook.TimeZone, outlook.StartsAt, outlook.EndsAt)
	}
	custom := byUID["custom@example.com"]
	if custom.Draft.TimeZone != "Etc/GMT-3" || !custom.Draft.StartsAt.Equal(time.Date(2026, 11, 8, 6, 0, 0, 0, time.UTC)) || len(custom.Warnings) != 0 {
		t.Errorf("the file's own zone read as %s %v %v", custom.Draft.TimeZone, custom.Draft.StartsAt, custom.Warnings)
	}
	allDay := byUID["allday@example.com"].Draft
	if !allDay.AllDay || allDay.EndsAt.Sub(allDay.StartsAt) != 48*time.Hour {
		t.Errorf("the all-day event read as %+v", allDay)
	}
	yearly := byUID["yearly@example.com"]
	if yearly.Draft.Recurrence != "" || len(yearly.Warnings) == 0 || !yearly.Cancelled {
		t.Errorf("an unsupported repeat read as %+v", yearly)
	}

	if _, err := ParseICS([]byte("not a calendar")); err == nil {
		t.Error("something that is not a calendar was read")
	}
}

/*
A calendar on a public address that redirects to a private one is refused at
the connection, not at the address: the guard is asked on every connection.
Simulated by an address that is allowed until its first connection is made.
Sabotage-verified: a dialer that ignores the guard after the first connection
fetches the private calendar.
*/
func TestAFetchCannotBeRedirectedOntoAPrivateNetwork(t *testing.T) {
	private := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(exported))
	}))
	defer private.Close()
	public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, private.URL+"/calendar.ics", http.StatusFound)
	}))
	defer public.Close()

	var connections atomic.Int32
	fetcher := NewFetcher(func() bool { return connections.Add(1) == 1 })
	_, err := fetcher.Fetch(context.Background(), public.URL, "", "")
	if err == nil || !errors.Is(err, ErrAddress) {
		t.Fatalf("a redirect onto a private address was followed: %v", err)
	}

	everything := NewFetcher(func() bool { return true })
	if fetched, err := everything.Fetch(context.Background(), public.URL, "", ""); err != nil || len(fetched.Body) == 0 {
		t.Errorf("with private addresses allowed, the redirect was refused: %v", err)
	}
}
