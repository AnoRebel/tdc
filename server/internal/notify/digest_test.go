package notify_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/notify"
)

func at(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

/*
The hour is the person's hour.

This machine and its database disagree about the time zone by three hours, so
a schedule that leaned on either's local time, or that set the hour on a UTC
date, would be wrong here in a way it would not be on a server at UTC.
*/
func TestADailyDigestFallsOnTheChosenHourInTheChosenZone(t *testing.T) {
	schedule := notify.DigestSchedule{Frequency: notify.FrequencyDaily, Hour: 8, Zone: "Africa/Nairobi"}

	for _, tc := range []struct{ from, want string }{
		// 07:00 in Nairobi: later this morning.
		{"2026-10-04T04:00:00Z", "2026-10-04T05:00:00Z"},
		// Exactly 08:00 there: strictly after, so tomorrow.
		{"2026-10-04T05:00:00Z", "2026-10-05T05:00:00Z"},
		// 23:30 UTC is already the next morning's small hours in Nairobi.
		{"2026-10-04T23:30:00Z", "2026-10-05T05:00:00Z"},
	} {
		next, err := schedule.Next(at(t, tc.from))
		if err != nil {
			t.Fatal(err)
		}
		if next == nil || !next.Equal(at(t, tc.want)) {
			t.Errorf("from %s the next summary is %v, want %s", tc.from, next, tc.want)
		}
	}
}

/*
Eight o'clock stays eight o'clock across a change to summer time, when one day
is twenty-five hours long. Adding twenty-four hours would send it at seven.
*/
func TestADigestKeepsItsHourAcrossAClockChange(t *testing.T) {
	daily := notify.DigestSchedule{Frequency: notify.FrequencyDaily, Hour: 8, Zone: "America/New_York"}

	// 08:00 EDT on Saturday 31 October 2026; the clocks go back on 1 November.
	next, err := daily.Next(at(t, "2026-10-31T12:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if want := at(t, "2026-11-01T13:00:00Z"); next == nil || !next.Equal(want) {
		t.Errorf("the day after the change, the summary is at %v, want %s (08:00 EST)", next, want)
	}

	weekly := notify.DigestSchedule{
		Frequency: notify.FrequencyWeekly, Hour: 8, Weekday: time.Monday, Zone: "America/New_York",
	}
	next, err = weekly.Next(at(t, "2026-10-31T12:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if want := at(t, "2026-11-02T13:00:00Z"); next == nil || !next.Equal(want) {
		t.Errorf("the weekly summary is at %v, want %s (Monday 08:00 EST)", next, want)
	}
}

// A weekly summary whose day is today but whose hour has passed is next week.
func TestAWeeklyDigestWhoseHourHasPassedIsNextWeek(t *testing.T) {
	weekly := notify.DigestSchedule{
		Frequency: notify.FrequencyWeekly, Hour: 8, Weekday: time.Sunday, Zone: "Africa/Dar_es_Salaam",
	}
	// Sunday 4 October 2026, 10:00 in Dar es Salaam.
	next, err := weekly.Next(at(t, "2026-10-04T07:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if want := at(t, "2026-10-11T05:00:00Z"); next == nil || !next.Equal(want) {
		t.Errorf("the summary is at %v, want %s", next, want)
	}
}

func TestAnOffDigestHasNothingScheduled(t *testing.T) {
	next, err := notify.DigestSchedule{Frequency: notify.FrequencyOff, Zone: "UTC"}.Next(time.Now())
	if err != nil || next != nil {
		t.Errorf("an off summary is scheduled for %v (%v)", next, err)
	}
}

func TestADigestScheduleThatCannotBeKeptIsRefused(t *testing.T) {
	for name, schedule := range map[string]notify.DigestSchedule{
		"the process's own zone": {Frequency: notify.FrequencyDaily, Zone: "Local"},
		"no zone":                {Frequency: notify.FrequencyDaily, Zone: ""},
		"a made-up zone":         {Frequency: notify.FrequencyDaily, Zone: "Africa/Atlantis"},
		"hour 24":                {Frequency: notify.FrequencyDaily, Hour: 24, Zone: "UTC"},
		"a negative hour":        {Frequency: notify.FrequencyDaily, Hour: -1, Zone: "UTC"},
		"an eighth day":          {Frequency: notify.FrequencyWeekly, Weekday: 7, Zone: "UTC"},
		"hourly":                 {Frequency: "hourly", Zone: "UTC"},
	} {
		if err := schedule.Validate(); !errors.Is(err, notify.ErrInvalidSchedule) {
			t.Errorf("%s: %v, want ErrInvalidSchedule", name, err)
		}
	}
}

// A summary counts; it never quotes. What was said stays on the server.
func TestAComposedDigestCountsWithoutQuoting(t *testing.T) {
	digest := notify.ComposeDigest("TDC", "Amina", notify.FrequencyDaily, []notify.DigestConversation{
		{Name: "general", Notifications: 5, People: 3, Addressed: 2, Link: "https://tdc.test/rooms/1"},
		{Name: "", Direct: true, Notifications: 1, People: 1, Addressed: 1},
	}, 4, "https://tdc.test/settings")

	for _, want := range []string{
		"Hello Amina",
		"general: 5 notifications from 3 people, 2 addressed to you",
		"A direct conversation: 1 notification",
		"And 4 more conversations.",
		"https://tdc.test/settings",
	} {
		if !strings.Contains(digest.Body, want) {
			t.Errorf("the summary lacks %q:\n%s", want, digest.Body)
		}
	}
	if digest.Subject != "6 notifications waiting for you on TDC" {
		t.Errorf("subject %q", digest.Subject)
	}
}

// eventIn appends a message to a room at a given position.
func eventIn(t *testing.T, queries *dbgen.Queries, room, sender uuid.UUID, sequence int64) dbgen.Event {
	t.Helper()
	event, err := queries.AppendEvent(context.Background(), dbgen.AppendEventParams{
		ID:            uuid.NewString(),
		RoomID:        room,
		Sequence:      sequence,
		SenderID:      &sender,
		SenderAddress: "someone@tdc.test",
		RoomAddress:   "general@tdc.test",
		OriginServer:  "tdc.test",
		Type:          "m.message",
		Content:       []byte(`{"body":"@juma habari"}`),
		CreatedAt:     time.Now().UnixMilli(),
		Nonce:         uuid.NewString(),
		Signature:     "signature",
		KeyID:         "test-key",
	})
	if err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	return event
}

/*
A burst in one conversation reaches the devices once. The rest are still raised
— counted, listed, individually dismissible — but a second push about a
conversation somebody was told about moments ago and has not opened says
nothing the first did not.
*/
func TestABurstInOneConversationIsPushedOnce(t *testing.T) {
	pool := testPool(t)
	queries := dbgen.New(pool)
	ctx := context.Background()

	sender := createUser(t, queries, "amina")
	recipient := createUser(t, queries, "juma")
	general := createRoom(t, queries, "general")
	random := createRoom(t, queries, "random")

	devices := &recorder{}
	service := notify.NewService(pool, quietLogger()).WithPush(devices).
		WithBatching(func(context.Context) time.Duration { return time.Minute })

	sequence := int64(0)
	send := func(room uuid.UUID) {
		sequence++
		event := eventIn(t, queries, room, sender.id, sequence)
		service.ForMessageIn(ctx, event, "@juma habari", members(recipient), false, notify.Delivery{})
	}

	for range 5 {
		send(general)
	}
	if len(devices.reached) != 1 {
		t.Fatalf("five messages in quick succession reached the devices %d times, want 1", len(devices.reached))
	}

	// Every one of them is still a notification.
	if count, err := service.UnreadCount(ctx, recipient.id); err != nil || count != 5 {
		t.Errorf("unread count %d (%v), want 5: batching must not drop notifications", count, err)
	}

	// Another conversation is news.
	send(random)
	if len(devices.reached) != 2 {
		t.Errorf("a message in a second conversation reached the devices %d times in all, want 2", len(devices.reached))
	}

	// Reading the conversation ends the burst: the next message is news again.
	if err := service.MarkRoomRead(ctx, recipient.id, general); err != nil {
		t.Fatal(err)
	}
	send(general)
	if len(devices.reached) != 3 {
		t.Errorf("after reading the conversation, a new message reached the devices %d times in all, want 3", len(devices.reached))
	}
}

// A window of zero is the operator choosing to push every notification.
func TestAZeroBatchWindowPushesEveryNotification(t *testing.T) {
	pool := testPool(t)
	queries := dbgen.New(pool)

	sender := createUser(t, queries, "amina")
	recipient := createUser(t, queries, "juma")
	room := createRoom(t, queries, "general")

	devices := &recorder{}
	service := notify.NewService(pool, quietLogger()).WithPush(devices).
		WithBatching(func(context.Context) time.Duration { return 0 })

	for sequence := int64(1); sequence <= 3; sequence++ {
		event := eventIn(t, queries, room, sender.id, sequence)
		service.ForMessageIn(context.Background(), event, "@juma habari", members(recipient), false, notify.Delivery{})
	}
	if len(devices.reached) != 3 {
		t.Errorf("with no window, three messages reached the devices %d times, want 3", len(devices.reached))
	}
}

/*
Choosing a summary replaces the interruptions: nothing reaches a device, and
everything is still raised, because the raised notifications are what the
summary is made from.
*/
func TestSomebodyWhoChoseADigestIsNotPushed(t *testing.T) {
	pool := testPool(t)
	queries := dbgen.New(pool)
	ctx := context.Background()

	sender := createUser(t, queries, "amina")
	summarised := createUser(t, queries, "juma")
	interrupted := createUser(t, queries, "neema")
	room := createRoom(t, queries, "general")

	devices := &recorder{}
	service := notify.NewService(pool, quietLogger()).WithPush(devices).
		WithDigests(func(_ context.Context, userID uuid.UUID) bool { return userID == summarised.id })

	event := eventIn(t, queries, room, sender.id, 1)
	service.ForMessageIn(ctx, event, "@juma @neema habari", members(summarised, interrupted), false, notify.Delivery{})

	if len(devices.reached) != 1 || devices.reached[0] != interrupted.id {
		t.Errorf("reached %v, want only the person who did not choose a summary", devices.reached)
	}
	if count, _ := service.UnreadCount(ctx, summarised.id); count != 1 {
		t.Errorf("the person with a summary has %d notifications, want 1 for it to summarise", count)
	}
}
