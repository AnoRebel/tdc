package calendar

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/teambition/rrule-go"
)

/*
Repeats.

A subset of RFC 5545's RRULE, enough for the life of a community: something
that happens every day, every week on some weekdays, or every month on a date
or an "nth weekday", every so many of those, ending after a number of times, on
a date, or not at all. Yearly rules, rules by hour or minute, and BYSETPOS are
refused when written: each is real, none is something a meetup needs, and every
one accepted is one more thing the form, the export and the people reading a
listing have to understand.

Expansion is in the event's own zone, which is what keeps a weekly 19:00 at
19:00 when the clocks change: rrule-go steps in wall-clock time in the
location of the start it is given.
*/

// ErrRecurrence reports a repeat rule outside what events support.
var ErrRecurrence = errors.New("that repeat is not one events support")

// MaxCount is the most occurrences a counted series may have.
const MaxCount = 500

// expansionCap bounds how many occurrences any one expansion produces, so a
// daily series without end asked about a decade cannot run away.
const expansionCap = 2000

/*
NormaliseRule checks a rule and returns it in one canonical spelling, so two
ways of writing the same repeat are stored alike and export alike. Empty is
an event that happens once.
*/
func NormaliseRule(rule string) (string, error) {
	rule = strings.TrimPrefix(strings.TrimSpace(rule), "RRULE:")
	if rule == "" {
		return "", nil
	}
	option, err := rrule.StrToROption(rule)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrRecurrence, err)
	}
	if err := supported(option); err != nil {
		return "", err
	}
	return option.RRuleString(), nil
}

func supported(option *rrule.ROption) error {
	switch option.Freq {
	case rrule.DAILY, rrule.WEEKLY, rrule.MONTHLY:
	default:
		return fmt.Errorf("%w: it repeats daily, weekly or monthly", ErrRecurrence)
	}
	if len(option.Bysetpos)+len(option.Bymonth)+len(option.Byyearday)+len(option.Byweekno)+
		len(option.Byhour)+len(option.Byminute)+len(option.Bysecond)+len(option.Byeaster) > 0 {
		return fmt.Errorf("%w: only weekdays and days of the month can be chosen", ErrRecurrence)
	}
	if option.Interval < 0 || option.Interval > 52 {
		return fmt.Errorf("%w: every 1 to 52", ErrRecurrence)
	}
	if option.Count < 0 || option.Count > MaxCount {
		return fmt.Errorf("%w: at most %d times", ErrRecurrence, MaxCount)
	}
	if option.Count > 0 && !option.Until.IsZero() {
		return fmt.Errorf("%w: it ends after a number of times or on a date, not both", ErrRecurrence)
	}
	for _, day := range option.Byweekday {
		if day.N() != 0 && option.Freq != rrule.MONTHLY {
			return fmt.Errorf("%w: \"the second Tuesday\" is a monthly repeat", ErrRecurrence)
		}
	}
	for _, day := range option.Bymonthday {
		if day == 0 || day < -31 || day > 31 {
			return fmt.Errorf("%w: a day of the month is 1 to 31", ErrRecurrence)
		}
	}
	return nil
}

// Series is what expanding an event needs: when it first happens, how long
// each occurrence lasts, where it is meant, how it repeats and what is skipped.
type Series struct {
	Start      time.Time
	End        time.Time
	Zone       *time.Location
	Rule       string
	Exceptions []time.Time
}

// Occurrence is one time an event happens.
type Occurrence struct {
	Start time.Time
	End   time.Time
}

func (s Series) rule() (*rrule.RRule, error) {
	option, err := rrule.StrToROptionInLocation(s.Rule, s.Zone)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRecurrence, err)
	}
	option.Dtstart = s.Start.In(s.Zone)
	return rrule.NewRRule(*option)
}

func (s Series) skipped(start time.Time) bool {
	return slices.ContainsFunc(s.Exceptions, start.Equal)
}

/*
Between lists the occurrences that overlap a range: any that has not ended by
since and has started before until.
*/
func (s Series) Between(since, until time.Time) ([]Occurrence, error) {
	length := s.End.Sub(s.Start)
	if s.Rule == "" {
		if s.Start.Before(until) && s.End.After(since) {
			return []Occurrence{{Start: s.Start, End: s.End}}, nil
		}
		return nil, nil
	}
	rule, err := s.rule()
	if err != nil {
		return nil, err
	}
	// Started up to one occurrence's length before the range, so one already
	// under way when it opens is included.
	starts := rule.Between(since.Add(-length), until, true)
	occurrences := make([]Occurrence, 0, len(starts))
	for _, start := range starts {
		if len(occurrences) >= expansionCap {
			break
		}
		end := start.Add(length)
		if s.skipped(start) || !end.After(since) || !start.Before(until) {
			continue
		}
		occurrences = append(occurrences, Occurrence{Start: start.UTC(), End: end.UTC()})
	}
	return occurrences, nil
}

// Next is the first occurrence starting at or after a moment, if there is one.
func (s Series) Next(from time.Time) (Occurrence, bool) {
	length := s.End.Sub(s.Start)
	if s.Rule == "" {
		if !s.Start.Before(from) {
			return Occurrence{Start: s.Start, End: s.End}, true
		}
		return Occurrence{}, false
	}
	rule, err := s.rule()
	if err != nil {
		return Occurrence{}, false
	}
	at := from
	for range expansionCap {
		start := rule.After(at, true)
		if start.IsZero() {
			return Occurrence{}, false
		}
		if !s.skipped(start) {
			return Occurrence{Start: start.UTC(), End: start.Add(length).UTC()}, true
		}
		at = start.Add(time.Second)
	}
	return Occurrence{}, false
}

/*
LastEnd is when the last occurrence ends, or nil for a series without end.

Stored beside the event, so a query for a range can leave out a series that
finished long ago without expanding it.
*/
func (s Series) LastEnd() (*time.Time, error) {
	if s.Rule == "" {
		end := s.End
		return &end, nil
	}
	rule, err := s.rule()
	if err != nil {
		return nil, err
	}
	if rule.OrigOptions.Count == 0 && rule.OrigOptions.Until.IsZero() {
		return nil, nil
	}
	starts := rule.All()
	for i := len(starts) - 1; i >= 0; i-- {
		if !s.skipped(starts[i]) {
			end := starts[i].Add(s.End.Sub(s.Start)).UTC()
			return &end, nil
		}
	}
	// Every occurrence skipped: it ends where it began.
	end := s.End
	return &end, nil
}
