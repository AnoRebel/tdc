package calendar

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	ics "github.com/arran4/golang-ical"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
)

/*
Reading iCalendar into events.

The library reads the file's structure; what each value means is decided here,
because that is where a calendar from somewhere else is most likely to be
wrong for this server: a zone named the way Windows names it, a zone defined
only by the file, a time given with no zone at all. Each is resolved as well as
it can be and, where it cannot be resolved exactly, imported with a warning the
person sees before anything is written.
*/

// MaxImportBytes is the largest calendar file read.
const MaxImportBytes = 1 << 20

// MaxImportEvents is the most events one import considers.
const MaxImportEvents = 1000

// Incoming is one event from a calendar file, as it would be scheduled here.
type Incoming struct {
	UID       string
	Draft     Draft
	Cancelled bool
	Warnings  []string
}

// ErrUnreadable is a file that is not iCalendar.
var ErrUnreadable = errors.New("that is not a calendar file this server can read")

/*
ParseICS reads every VEVENT of a calendar file. Changes to single occurrences
of a series (RECURRENCE-ID) are left out and said so: events here repeat as a
whole.
*/
func ParseICS(data []byte) ([]Incoming, error) {
	if len(data) > MaxImportBytes {
		return nil, fmt.Errorf("%w: it is larger than %d bytes", ErrUnreadable, MaxImportBytes)
	}
	parsed, err := ics.ParseCalendar(bytes.NewReader(data))
	if err != nil || parsed == nil {
		return nil, ErrUnreadable
	}
	defaultZone := ""
	for _, property := range parsed.CalendarProperties {
		if strings.EqualFold(property.IANAToken, "X-WR-TIMEZONE") {
			defaultZone = strings.TrimSpace(property.Value)
		}
	}
	fixed := fixedZones(parsed)

	var incoming []Incoming
	for _, event := range parsed.Events() {
		if len(incoming) >= MaxImportEvents {
			break
		}
		if event.GetProperty(ics.ComponentPropertyRecurrenceId) != nil {
			continue
		}
		item, ok := readEvent(event, defaultZone, fixed)
		if ok {
			incoming = append(incoming, item)
		}
	}
	return incoming, nil
}

func text(event *ics.VEvent, property ics.ComponentProperty) string {
	if value := event.GetProperty(property); value != nil {
		return unescapeText(value.Value)
	}
	return ""
}

func unescapeText(value string) string {
	return strings.NewReplacer(`\n`, "\n", `\N`, "\n", `\,`, ",", `\;`, ";", `\\`, `\`).Replace(value)
}

func readEvent(event *ics.VEvent, defaultZone string, fixed map[string]string) (Incoming, bool) {
	item := Incoming{UID: strings.TrimSpace(text(event, ics.ComponentPropertyUniqueId))}
	start := event.GetProperty(ics.ComponentPropertyDtStart)
	if item.UID == "" || start == nil {
		return Incoming{}, false
	}
	draft := Draft{
		Kind: KindOther, Title: strings.TrimSpace(text(event, ics.ComponentPropertySummary)),
		Description: text(event, ics.ComponentPropertyDescription),
		Location:    text(event, ics.ComponentPropertyLocation),
	}
	if draft.Title == "" {
		draft.Title = "Untitled event"
	}
	if url := text(event, ics.ComponentPropertyUrl); strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://") {
		draft.OnlineURL = url
	}
	warn := func(format string, args ...any) { item.Warnings = append(item.Warnings, fmt.Sprintf(format, args...)) }

	at, zone, allDay, note := readTime(start, defaultZone, fixed)
	if note != "" {
		warn("%s", note)
	}
	if at.IsZero() {
		return Incoming{}, false
	}
	draft.StartsAt, draft.TimeZone, draft.AllDay = at, zone, allDay

	if end := event.GetProperty(ics.ComponentPropertyDtEnd); end != nil {
		draft.EndsAt, _, _, _ = readTime(end, defaultZone, fixed)
	} else if duration := event.GetProperty(ics.ComponentPropertyDuration); duration != nil {
		if length, ok := parseDuration(duration.Value); ok {
			draft.EndsAt = at.Add(length)
		}
	}
	if draft.EndsAt.IsZero() || draft.EndsAt.Before(at) {
		draft.EndsAt = at.Add(time.Hour)
		if allDay {
			draft.EndsAt = at.AddDate(0, 0, 1)
		}
	}

	if rule := event.GetProperty(ics.ComponentPropertyRrule); rule != nil {
		if normalised, err := NormaliseRule(rule.Value); err == nil {
			draft.Recurrence = normalised
		} else {
			warn("its repeat (%s) is not one events support; only its first occurrence is imported", rule.Value)
		}
	}
	if draft.Recurrence != "" {
		for _, excluded := range event.GetProperties(ics.ComponentPropertyExdate) {
			for _, value := range strings.Split(excluded.Value, ",") {
				single := &ics.IANAProperty{BaseProperty: ics.BaseProperty{Value: value, ICalParameters: excluded.ICalParameters}}
				if skipped, _, _, _ := readTime(single, defaultZone, fixed); !skipped.IsZero() {
					draft.Exceptions = append(draft.Exceptions, skipped)
				}
			}
		}
	}
	item.Cancelled = strings.EqualFold(text(event, ics.ComponentPropertyStatus), "CANCELLED")
	draft.Visibility = VisibilityPublic
	item.Draft = draft
	return item, true
}

var stampPattern = regexp.MustCompile(`^(\d{8})(T(\d{6})(Z?))?$`)

/*
readTime reads a DTSTART, DTEND or EXDATE: the instant, the zone it was meant
in, whether it is a whole day, and a note when the zone had to be guessed.
*/
func readTime(property *ics.IANAProperty, defaultZone string, fixed map[string]string) (time.Time, string, bool, string) {
	value := strings.TrimSpace(property.Value)
	match := stampPattern.FindStringSubmatch(value)
	if match == nil {
		return time.Time{}, "", false, ""
	}
	zoneName, note := "UTC", ""
	if tzid, ok := property.ICalParameters["TZID"]; ok && len(tzid) > 0 {
		zoneName, note = resolveZone(strings.Trim(tzid[0], `"`), fixed)
	} else if match[4] != "Z" {
		if defaultZone != "" {
			zoneName, note = resolveZone(defaultZone, fixed)
		} else if match[2] != "" {
			note = "its time names no zone; it is read as UTC"
		}
	} else if defaultZone != "" {
		// An instant in UTC, shown in the calendar's own zone.
		if name, guess := resolveZone(defaultZone, fixed); guess == "" {
			zoneName = name
		}
	}
	zone, err := time.LoadLocation(zoneName)
	if err != nil {
		zone, zoneName = time.UTC, "UTC"
	}

	allDay := match[2] == ""
	layout, text := "20060102", match[1]
	if !allDay {
		layout, text = "20060102T150405", match[1]+"T"+match[3]
	}
	location := zone
	if match[4] == "Z" {
		location = time.UTC
	}
	at, err := time.ParseInLocation(layout, text, location)
	if err != nil {
		return time.Time{}, "", false, ""
	}
	return at, zoneName, allDay, note
}

/*
resolveZone names a zone this server knows for a TZID: as an IANA name, as the
Windows name Outlook writes, or as the fixed offset the file itself defines —
and otherwise UTC, with a note saying so.
*/
func resolveZone(tzid string, fixed map[string]string) (string, string) {
	if tzid == "" {
		return "UTC", ""
	}
	if _, err := time.LoadLocation(tzid); err == nil && tzid != "Local" {
		return tzid, ""
	}
	if zone := ics.WindowsTimezoneToIANA(tzid); zone != nil {
		return zone.String(), ""
	}
	if name, ok := fixed[tzid]; ok {
		return name, ""
	}
	return "UTC", fmt.Sprintf("its time zone %q is not one this server knows; it is read as UTC", tzid)
}

/*
fixedZones reads the file's own VTIMEZONE definitions that have a single
offset and no daylight saving, as the Etc/GMT zone with that offset. A zone
with daylight saving is not guessed at: its rules would have to be matched to a
real zone, and a wrong match is worse than a stated one.
*/
func fixedZones(parsed *ics.Calendar) map[string]string {
	zones := map[string]string{}
	for _, component := range parsed.Components {
		timezone, ok := component.(*ics.VTimezone)
		if !ok {
			continue
		}
		tzid := ""
		if property := timezone.GetProperty(ics.ComponentPropertyTzid); property != nil {
			tzid = property.Value
		}
		var offsets []string
		daylight := false
		for _, sub := range timezone.SubComponents() {
			switch part := sub.(type) {
			case *ics.Standard:
				if property := part.GetProperty(ics.ComponentProperty(ics.PropertyTzoffsetto)); property != nil {
					offsets = append(offsets, property.Value)
				}
			case *ics.Daylight:
				daylight = true
			}
		}
		if tzid == "" || daylight || len(offsets) != 1 {
			continue
		}
		if name, ok := etcZone(offsets[0]); ok {
			zones[tzid] = name
		}
	}
	return zones
}

// etcZone is the Etc/GMT zone for a whole-hour offset like "+0300", whose
// sign is the opposite of what it reads, as POSIX has it.
func etcZone(offset string) (string, bool) {
	if len(offset) < 5 || offset[3:5] != "00" {
		return "", false
	}
	hours, err := strconv.Atoi(offset[1:3])
	if err != nil {
		return "", false
	}
	switch {
	case hours == 0:
		return "UTC", true
	case offset[0] == '+':
		return fmt.Sprintf("Etc/GMT-%d", hours), true
	default:
		return fmt.Sprintf("Etc/GMT+%d", hours), true
	}
}

var durationPattern = regexp.MustCompile(`^P(?:(\d+)W)?(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?)?$`)

func parseDuration(value string) (time.Duration, bool) {
	match := durationPattern.FindStringSubmatch(strings.TrimPrefix(strings.TrimSpace(value), "+"))
	if match == nil {
		return 0, false
	}
	units := []time.Duration{7 * 24 * time.Hour, 24 * time.Hour, time.Hour, time.Minute, time.Second}
	var total time.Duration
	for i, unit := range units {
		if match[i+1] != "" {
			count, _ := strconv.Atoi(match[i+1])
			total += time.Duration(count) * unit
		}
	}
	return total, true
}

// Plan is what importing one event would do.
type Plan struct {
	Incoming
	// Action is "create", "update" or "skip".
	Action string
	// Reason says why an event is skipped.
	Reason string
}

/*
PlanImport says what importing would do with each event, without doing it: a
UID new here is created; one matching an event the person may edit is updated;
one matching somebody else's is skipped, never overwritten.
*/
func (s *Service) PlanImport(ctx context.Context, viewer Viewer, incoming []Incoming) []Plan {
	plans := make([]Plan, 0, len(incoming))
	for _, item := range incoming {
		plan := Plan{Incoming: item, Action: "create"}
		existing, err := s.queries.GetCalendarEventByUID(ctx, dbgen.GetCalendarEventByUIDParams{Uid: item.UID})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			plan.Action, plan.Reason = "skip", "it could not be checked"
		case viewer.MayEdit(existing):
			plan.Action = "update"
		default:
			plan.Action, plan.Reason = "skip", "an event with the same identity is already here, and is not yours to change"
		}
		plans = append(plans, plan)
	}
	return plans
}

// ImportChoice is where imported events go and what they are.
type ImportChoice struct {
	ChannelID  *uuid.UUID
	Visibility string
	Kind       string
	// UIDs chosen from the preview; empty imports everything that can be.
	UIDs []string
}

// Imported counts what an import did.
type Imported struct {
	Created, Updated, Skipped int
	Logs                      []dbgen.Event
}

// Import schedules the chosen events from a file, as the person importing.
func (s *Service) Import(ctx context.Context, actor dbgen.User, viewer Viewer, incoming []Incoming, choice ImportChoice) (Imported, error) {
	var result Imported
	chosen := map[string]bool{}
	for _, uid := range choice.UIDs {
		chosen[uid] = true
	}
	for _, plan := range s.PlanImport(ctx, viewer, incoming) {
		if (len(chosen) > 0 && !chosen[plan.UID]) || plan.Action == "skip" {
			result.Skipped++
			continue
		}
		draft := plan.Draft
		draft.ChannelID, draft.Visibility = choice.ChannelID, choice.Visibility
		if choice.Kind != "" {
			draft.Kind = choice.Kind
		}
		if plan.Action == "update" {
			existing, err := s.queries.GetCalendarEventByUID(ctx, dbgen.GetCalendarEventByUIDParams{Uid: plan.UID})
			if err != nil {
				result.Skipped++
				continue
			}
			changed, err := s.Update(ctx, actor, viewer, existing.ID, draft)
			if err != nil {
				return result, fmt.Errorf("%q: %w", draft.Title, err)
			}
			result.Updated++
			result.Logs = append(result.Logs, changed.Log)
			continue
		}
		created, err := s.create(ctx, actor, viewer, draft, nil, plan.UID)
		if err != nil {
			return result, fmt.Errorf("%q: %w", draft.Title, err)
		}
		result.Created++
		result.Logs = append(result.Logs, created.Log)
		if plan.Cancelled {
			if cancelled, err := s.Cancel(ctx, actor, viewer, created.Event.ID, ""); err == nil {
				result.Logs = append(result.Logs, cancelled.Log)
			}
		}
		// The new event is the person's own now; later ones in the same file
		// may update it.
		viewer.Hosting[created.Event.ID] = true
	}
	return result, nil
}

// Synced counts what refreshing a subscription did.
type Synced struct {
	Created, Updated, Cancelled int
	Logs                        []dbgen.Event
}

/*
Sync brings a subscription's events into line with what its calendar holds
now: new UIDs are created, changed ones updated, and events that have
disappeared from it are cancelled. Done as the person who made the
subscription, whose permission to do so was checked when they made it; changes
after that are the calendar's and are recorded as the server's.
*/
func (s *Service) Sync(ctx context.Context, subscription dbgen.CalendarSubscription, incoming []Incoming) (Synced, error) {
	var result Synced
	if subscription.CreatedBy == nil {
		return result, errors.New("the account that made this subscription is gone")
	}
	owner, err := s.queries.GetUserByID(ctx, *subscription.CreatedBy)
	if err != nil {
		return result, fmt.Errorf("read the subscription's owner: %w", err)
	}
	channelVisibility, channelName := "", ""
	if subscription.ChannelID != nil {
		channel, err := s.queries.GetRoomByID(ctx, *subscription.ChannelID)
		if err != nil {
			return result, fmt.Errorf("read the subscription's channel: %w", err)
		}
		channelVisibility, channelName = channel.Visibility, channel.Name
	}
	existing, err := s.queries.ListSubscriptionEventUIDs(ctx, &subscription.ID)
	if err != nil {
		return result, fmt.Errorf("read the subscription's events: %w", err)
	}
	present := map[string]bool{}
	for _, item := range incoming {
		present[item.UID] = true
		draft := item.Draft
		draft.ChannelID, draft.Visibility, draft.Kind = subscription.ChannelID, subscription.Visibility, subscription.Kind
		if err := draft.Normalise(channelVisibility); err != nil {
			continue
		}
		current, err := s.queries.GetCalendarEventByUID(ctx, dbgen.GetCalendarEventByUIDParams{
			Uid: item.UID, SubscriptionID: &subscription.ID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			created, err := s.createChecked(ctx, owner, draft, &subscription.ID, item.UID, channelName, channelVisibility)
			if err != nil {
				return result, err
			}
			result.Created++
			result.Logs = append(result.Logs, created.Log)
			continue
		}
		if err != nil {
			return result, fmt.Errorf("read event: %w", err)
		}
		if entry, changed, err := s.refreshFromSource(ctx, current, draft, item.Cancelled); err != nil {
			return result, err
		} else if changed {
			result.Updated++
			result.Logs = append(result.Logs, entry)
		}
	}
	for _, row := range existing {
		if present[row.Uid] || row.CancelledAt != nil {
			continue
		}
		event, err := s.queries.CancelCalendarEvent(ctx, dbgen.CancelCalendarEventParams{
			ID: row.ID, CancelReason: "no longer in the subscribed calendar",
		})
		if err != nil {
			continue
		}
		room, err := s.queries.GetRoomByID(ctx, row.ID)
		if err != nil {
			continue
		}
		content := Summary(event)
		content["reason"] = event.CancelReason
		if entry, err := s.append(ctx, s.queries, room, nil, events.TypeEventCancelled, content); err == nil {
			result.Logs = append(result.Logs, entry)
		}
		result.Cancelled++
	}
	return result, nil
}

/*
createChecked schedules an event whose permission was settled elsewhere — a
subscription's, checked when it was made — with a draft already normalised.
*/
func (s *Service) createChecked(
	ctx context.Context, owner dbgen.User, draft Draft, subscription *uuid.UUID, uid, channelName, channelVisibility string,
) (Created, error) {
	return s.insert(ctx, owner, draft, subscription, uid, channelName, channelVisibility)
}

// refreshFromSource applies what a subscribed calendar now says about one of
// its events, when anything differs.
func (s *Service) refreshFromSource(ctx context.Context, current dbgen.CalendarEvent, draft Draft, cancelled bool) (dbgen.Event, bool, error) {
	lastEnd, err := Series{
		Start: draft.StartsAt, End: draft.EndsAt, Zone: mustZone(draft.TimeZone),
		Rule: draft.Recurrence, Exceptions: draft.Exceptions,
	}.LastEnd()
	if err != nil {
		return dbgen.Event{}, false, nil
	}
	proposed := current
	proposed.Title, proposed.Description, proposed.StartsAt, proposed.EndsAt = draft.Title, draft.Description, draft.StartsAt, draft.EndsAt
	proposed.TimeZone, proposed.AllDay, proposed.Recurrence, proposed.Exceptions = draft.TimeZone, draft.AllDay, draft.Recurrence, draft.Exceptions
	proposed.Location, proposed.OnlineUrl = draft.Location, draft.OnlineURL
	fields := differences(current, proposed)
	nowCancelled := cancelled != (current.CancelledAt != nil)
	if len(fields) == 0 && !nowCancelled {
		return dbgen.Event{}, false, nil
	}
	var entry dbgen.Event
	err = s.inTransaction(ctx, func(q *dbgen.Queries) error {
		updated, err := q.UpdateCalendarEvent(ctx, dbgen.UpdateCalendarEventParams{
			ID: current.ID, ChannelID: current.ChannelID, Kind: current.Kind, Title: draft.Title,
			Description: draft.Description, StartsAt: draft.StartsAt, EndsAt: draft.EndsAt,
			TimeZone: draft.TimeZone, AllDay: draft.AllDay, Recurrence: draft.Recurrence,
			Exceptions: draft.Exceptions, LastEndsAt: lastEnd, Location: draft.Location,
			Online: current.Online, OnlineUrl: draft.OnlineURL, Capacity: current.Capacity,
			Visibility: current.Visibility,
		})
		if err != nil {
			return fmt.Errorf("update event: %w", err)
		}
		kind := events.TypeEventUpdated
		if nowCancelled && cancelled {
			if updated, err = q.CancelCalendarEvent(ctx, dbgen.CancelCalendarEventParams{ID: current.ID}); err != nil {
				return fmt.Errorf("cancel event: %w", err)
			}
			kind = events.TypeEventCancelled
		} else if nowCancelled {
			if updated, err = q.RestoreCalendarEvent(ctx, current.ID); err != nil {
				return fmt.Errorf("restore event: %w", err)
			}
		}
		room, err := q.GetRoomByID(ctx, current.ID)
		if err != nil {
			return err
		}
		content := Summary(updated)
		content["changed"] = fields
		entry, err = s.append(ctx, q, room, nil, kind, content)
		return err
	})
	return entry, err == nil, err
}
