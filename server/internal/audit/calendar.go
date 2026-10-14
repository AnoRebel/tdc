package audit

/*
Events: the acts worth an account afterwards.

Cancelling or deleting an event takes it away from everybody going; acting on
somebody else's under event.manage.any is authority exercised over another
person's work; a feed address is a credential to somebody's calendar; and a
subscription makes the server fetch an address on a schedule.
*/
const (
	ActionEventCancelled Action = "event.cancelled"
	ActionEventDeleted   Action = "event.deleted"
	// Somebody holding event.manage.any changed an event they neither made
	// nor host.
	ActionEventManaged Action = "event.managed"

	ActionCalendarFeedCreated Action = "calendar.feed.created"
	ActionCalendarFeedRevoked Action = "calendar.feed.revoked"

	ActionCalendarSubscriptionCreated Action = "calendar.subscription.created"
	ActionCalendarSubscriptionDeleted Action = "calendar.subscription.deleted"
)
