package calendar

import (
	"testing"
	"time"
)

// A weekly 19:00 in London stays at 19:00 London time when summer time ends
// on the last Sunday of October, which in UTC is an hour later.
func TestAWeeklySeriesKeepsItsLocalTimeAcrossTheClockChange(t *testing.T) {
	london, _ := time.LoadLocation("Europe/London")
	start := time.Date(2026, time.October, 13, 19, 0, 0, 0, london)
	series := Series{Start: start.UTC(), End: start.Add(time.Hour).UTC(), Zone: london, Rule: "FREQ=WEEKLY;COUNT=4"}

	occurrences, err := series.Between(start.Add(-time.Hour), start.AddDate(0, 1, 0))
	if err != nil || len(occurrences) != 4 {
		t.Fatalf("expanded %d occurrences, %v", len(occurrences), err)
	}
	for _, occurrence := range occurrences {
		if local := occurrence.Start.In(london); local.Hour() != 19 {
			t.Errorf("an occurrence starts at %v London time", local)
		}
	}
	if occurrences[0].Start.UTC().Hour() == occurrences[3].Start.UTC().Hour() {
		t.Error("the UTC hour did not move with the clocks, so the local time must have")
	}
}

// Expansion depends on the event's zone, never the process's: the same series
// expanded with the process in Honolulu lands on the same instants.
func TestExpansionIgnoresTheProcessZone(t *testing.T) {
	dar, _ := time.LoadLocation("Africa/Dar_es_Salaam")
	start := time.Date(2026, time.November, 2, 18, 0, 0, 0, dar)
	series := Series{Start: start.UTC(), End: start.Add(time.Hour).UTC(), Zone: dar, Rule: "FREQ=DAILY;COUNT=3"}

	first, _ := series.Between(start.Add(-time.Hour), start.AddDate(0, 0, 5))
	saved := time.Local
	time.Local, _ = time.LoadLocation("Pacific/Honolulu")
	second, _ := series.Between(start.Add(-time.Hour), start.AddDate(0, 0, 5))
	time.Local = saved

	if len(first) != 3 || len(second) != 3 {
		t.Fatalf("expanded %d and %d", len(first), len(second))
	}
	for i := range first {
		if !first[i].Start.Equal(second[i].Start) || first[i].Start.In(dar).Hour() != 18 {
			t.Errorf("occurrence %d: %v against %v", i, first[i].Start, second[i].Start)
		}
	}
}

func TestOnlyTheSupportedRepeatsAreAccepted(t *testing.T) {
	for rule, ok := range map[string]bool{
		"FREQ=WEEKLY;BYDAY=TU,TH;COUNT=8":           true,
		"FREQ=MONTHLY;BYDAY=2TU":                    true,
		"FREQ=MONTHLY;BYMONTHDAY=-1":                true,
		"RRULE:FREQ=DAILY;UNTIL=20261231T000000Z":   true,
		"FREQ=YEARLY":                               false,
		"FREQ=WEEKLY;BYDAY=2TU":                     false,
		"FREQ=DAILY;BYHOUR=9":                       false,
		"FREQ=DAILY;COUNT=501":                      false,
		"FREQ=DAILY;COUNT=2;UNTIL=20261231T000000Z": false,
	} {
		_, err := NormaliseRule(rule)
		if (err == nil) != ok {
			t.Errorf("%s: accepted=%v, want %v (%v)", rule, err == nil, ok, err)
		}
	}
}

// A day-long event is midnight to midnight in its own zone.
func TestAllDayIsWholeDaysInTheEventsZone(t *testing.T) {
	dar, _ := time.LoadLocation("Africa/Dar_es_Salaam")
	draft := Draft{
		Title: "Hackathon", TimeZone: "Africa/Dar_es_Salaam", AllDay: true,
		StartsAt: time.Date(2026, 11, 7, 10, 0, 0, 0, dar), EndsAt: time.Date(2026, 11, 8, 15, 0, 0, 0, dar),
	}
	if err := draft.Normalise(""); err != nil {
		t.Fatal(err)
	}
	if start := draft.StartsAt.In(dar); start.Hour() != 0 || start.Day() != 7 {
		t.Errorf("starts %v", start)
	}
	if end := draft.EndsAt.In(dar); end.Hour() != 0 || end.Day() != 9 {
		t.Errorf("ends %v", end)
	}
}
