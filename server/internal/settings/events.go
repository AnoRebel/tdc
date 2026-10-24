package settings

import (
	"fmt"
	"strconv"
)

/*
Events: meetups, talks, workshops and office hours.

On by default. Everything events do happens inside the server — the one thing
that reaches outside, subscribing to somebody else's calendar, is behind a
permission nobody holds until an operator gives it.
*/
const FeatureEvents Feature = "events"

const (
	/*
		EventsReminderMinutes is how long before an occurrence the people
		taking part are reminded, in minutes, comma-separated. The day before
		to plan around it, and a quarter of an hour before to turn up.
		Empty sends no reminders.
	*/
	EventsReminderMinutes Key = "events.reminder_minutes"

	/*
		EventsPublicFeed publishes the server's public events as a calendar
		feed anybody may subscribe to. Off by default: "public" means public
		to the people on this server, and showing it to the whole internet is
		a separate decision.
	*/
	EventsPublicFeed Key = "events.public_feed"

	// EventsSubscriptionRefreshMinutes is how often a subscribed external
	// calendar is fetched again.
	EventsSubscriptionRefreshMinutes Key = "events.subscription_refresh_minutes"
)

func init() {
	defineFeature(FeatureEvents, true,
		"Events: meetups, talks, workshops and office hours, with invitations, reminders and calendar feeds.")

	define(map[Key]definition{
		EventsReminderMinutes: {
			fallback:    "1440,15",
			description: "Minutes before an event that the people going are reminded, comma-separated (1440 is a day). Empty sends none.",
			validate:    minuteList,
		},
		EventsPublicFeed: {
			fallback:    "false",
			description: "Whether the server's public events are published as a calendar feed anybody may subscribe to, without signing in.",
			validate:    boolean,
		},
		EventsSubscriptionRefreshMinutes: {
			fallback:    "60",
			description: "How often, in minutes, a subscribed external calendar is fetched again. At least 15.",
			validate: func(value string) error {
				minutes, err := strconv.Atoi(value)
				if err != nil || minutes < 15 {
					return fmt.Errorf("%w: at least 15 minutes", ErrInvalidValue)
				}
				return nil
			},
		},
	})
}

// minuteList accepts a comma-separated list of lead times, each between a
// minute and four weeks, so a typo is refused when set.
func minuteList(value string) error {
	parts := ListOf(value)
	if len(parts) > 5 {
		return fmt.Errorf("%w: at most five reminders", ErrInvalidValue)
	}
	for _, part := range parts {
		minutes, err := strconv.Atoi(part)
		if err != nil || minutes < 1 || minutes > 40320 {
			return fmt.Errorf("%w: %q should be minutes between 1 and 40320", ErrInvalidValue, part)
		}
	}
	return nil
}

// ReminderLeads reads the lead times, in minutes, ignoring anything unreadable.
func ReminderLeads(value string) []int {
	var leads []int
	for _, part := range ListOf(value) {
		if minutes, err := strconv.Atoi(part); err == nil && minutes > 0 {
			leads = append(leads, minutes)
		}
	}
	return leads
}
