package settings

import (
	"fmt"
	"strconv"
)

/*
Emailed summaries of missed activity.

On by default, but offered only where it can work: a server with no mail
configured does not advertise it and refuses to switch it on for anybody,
whatever this toggle says. The toggle exists for the operator who has mail for
recovery and does not want it carrying anything else.

Nobody receives one because this is on. Each person chooses, and the default
for every account is not to.
*/
const FeatureNotificationDigests Feature = "notification-digests"

const (
	/*
		NotificationBatchSeconds is how long after telling somebody about a
		conversation the server holds back from telling them about it again.

		A burst of messages in one conversation is one thing worth an
		interruption: the first message reaches their devices, and what follows
		inside this window is gathered into the conversation's entry in their
		notification list instead of arriving one push at a time. Reading the
		conversation ends the window early, so the next message after that is
		news again.

		Zero sends every notification to devices as it is raised.
	*/
	NotificationBatchSeconds Key = "notifications.batch_seconds"

	/*
		NotificationDigestMaxConversations bounds how many conversations one
		summary lists. The busiest are kept and the rest are counted, so a
		summary stays something a person reads rather than scrolls.
	*/
	NotificationDigestMaxConversations Key = "notifications.digest_max_conversations"
)

// maxBatchSeconds is an hour. A window longer than that stops being a burst
// and starts being a silence nobody chose.
const maxBatchSeconds = 3600

func init() {
	defineFeature(FeatureNotificationDigests, true,
		"Emailed summaries of missed activity, for people who choose them. Offered only when this server can send mail.")

	define(map[Key]definition{
		NotificationBatchSeconds: {
			fallback:    "60",
			description: "Seconds after a notification during which further messages in the same conversation are gathered into it rather than each reaching devices. Zero sends each one.",
			validate:    batchWindow,
		},
		NotificationDigestMaxConversations: {
			fallback:    "20",
			description: "The most conversations one emailed summary lists; the rest are counted.",
			validate:    atLeastOne,
		},
	})
}

// batchWindow accepts zero to an hour.
func batchWindow(value string) error {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 || parsed > maxBatchSeconds {
		return fmt.Errorf("%w: expected a whole number of seconds from 0 to %d",
			ErrInvalidValue, maxBatchSeconds)
	}
	return nil
}
