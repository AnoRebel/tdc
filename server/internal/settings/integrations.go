package settings

import (
	"fmt"
	"strconv"
)

/*
Settings for programs that act on this server: interactive messages, webhooks
and applications.

Kept in a file of their own and registered through define, so this feature's
policy sits beside what it governs rather than in the middle of everybody
else's.
*/

const (
	/*
		PressesPerUserPerMinute bounds how quickly one person may activate
		controls on messages.

		Thirty a minute is far more than anybody answering questions does, and
		far less than a script clicking every button in a channel.
	*/
	PressesPerUserPerMinute Key = "integrations.presses_per_user_per_minute"

	/*
		PressesPerIntegrationPerMinute bounds how many presses of one
		program's controls are accepted in a minute, from everybody together.

		Separate from the per-person limit because the harm is different: a
		program whose message went to a very large channel could otherwise be
		asked to handle more than it can, and everything it would then do in
		response — updating the message, posting replies — is load on this
		server too.
	*/
	PressesPerIntegrationPerMinute Key = "integrations.presses_per_integration_per_minute"

	/*
		DeliveriesAllowPrivateAddresses permits deliveries to endpoints on
		private networks.

		Off by default, matching the guard on every other outbound request
		this server makes. An endpoint is named by whoever set up a webhook,
		and on a server where anybody may, reaching the server's own network
		is exactly what somebody would try. An operator whose receivers live
		beside the server on a network they control turns it on knowingly.
	*/
	DeliveriesAllowPrivateAddresses Key = "integrations.allow_private_addresses"

	/*
		DeliveryTimeoutSeconds bounds one delivery attempt, connection to
		answer.

		Five seconds. A receiver that has not answered by then is not going
		to, and every second waited is a delivery worker nobody else can use.
	*/
	DeliveryTimeoutSeconds Key = "integrations.delivery_timeout_seconds"

	/*
		WebhookMaxAttempts is how many times one delivery is tried before it
		is abandoned.

		Twelve, with the backoff below, spans most of a day: a receiver that
		is down overnight still receives what happened while it was away,
		and one that is gone for good stops being tried.
	*/
	WebhookMaxAttempts Key = "integrations.webhook_max_attempts"

	/*
		WebhookRetryBaseSeconds is the first retry's delay; each later one
		doubles it, up to WebhookRetryCapSeconds.

		A minute, then two, four, eight — quick enough that a receiver
		restarting is caught up within minutes, spaced enough that one that is
		down is not hammered.
	*/
	WebhookRetryBaseSeconds Key = "integrations.webhook_retry_base_seconds"
	WebhookRetryCapSeconds  Key = "integrations.webhook_retry_cap_seconds"

	/*
		WebhookDisableAfterFailures turns a subscription off after this many
		failed attempts in a row, across all its deliveries.

		The other way a subscription is turned off is a delivery exhausting
		every attempt. This one catches the busy subscription sooner: a
		receiver that has failed twenty-four times running, whatever it was
		sent, is not coming back by itself, and every delivery queued behind
		it is work spent on nothing.
	*/
	WebhookDisableAfterFailures Key = "integrations.webhook_disable_after_failures"

	/*
		WebhookDeliveriesPerMinute bounds how many deliveries one subscription
		is sent in a minute.

		A delivery past it is held back, not dropped, and the subscription
		records that it happened, so a very active channel delays a receiver
		rather than silently losing it events.
	*/
	WebhookDeliveriesPerMinute Key = "integrations.webhook_deliveries_per_minute"

	/*
		DeliveryWorkers is how many deliveries this server attempts at once,
		across every subscription.

		The ceiling on outbound connections, whatever arrives. Each
		subscription is additionally held to one delivery in flight, so a
		slow receiver occupies one worker and never all of them. Read at
		startup.
	*/
	DeliveryWorkers Key = "integrations.delivery_workers"

	// DeliveryHistoryDays is how long finished deliveries and their attempts
	// are kept for owners to inspect.
	DeliveryHistoryDays Key = "integrations.delivery_history_days"

	/*
		IncomingWebhookPostsPerMinute bounds how often one incoming webhook
		may post.

		Sixty a minute is more than any build system or alerting tool sends to
		a channel people read, and a bound on what whoever finds a leaked
		address can do with it: one channel, slowly, and its owner told.
	*/
	IncomingWebhookPostsPerMinute Key = "integrations.incoming_webhook_posts_per_minute"
)

// positive accepts a whole number of at least one, for a bound where zero
// would mean "never" rather than "no limit".
func positive(value string) error {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 1 {
		return fmt.Errorf("%w: expected a whole number, one or more", ErrInvalidValue)
	}
	return nil
}

func init() {
	define(map[Key]definition{
		PressesPerUserPerMinute: {
			fallback:    "30",
			description: "How many message controls one person may activate per minute, or 0 for no limit.",
			validate:    nonNegative,
		},
		PressesPerIntegrationPerMinute: {
			fallback:    "600",
			description: "How many activations of one integration's controls are accepted per minute, or 0 for no limit.",
			validate:    nonNegative,
		},
		DeliveriesAllowPrivateAddresses: {
			fallback:    "false",
			description: "Allow webhook and integration deliveries to endpoints on private network addresses.",
			validate:    boolean,
		},
		DeliveryTimeoutSeconds: {
			fallback:    "5",
			description: "How long one delivery attempt may take, in seconds.",
			validate:    positive,
		},
		WebhookMaxAttempts: {
			fallback:    "12",
			description: "How many times one webhook delivery is tried before it is abandoned.",
			validate:    positive,
		},
		WebhookRetryBaseSeconds: {
			fallback:    "60",
			description: "The delay before a failed delivery's first retry, in seconds; each later retry doubles it.",
			validate:    positive,
		},
		WebhookRetryCapSeconds: {
			fallback:    "21600",
			description: "The longest delay between retries of one delivery, in seconds.",
			validate:    positive,
		},
		WebhookDisableAfterFailures: {
			fallback:    "24",
			description: "Turn a webhook subscription off after this many failed attempts in a row.",
			validate:    positive,
		},
		WebhookDeliveriesPerMinute: {
			fallback:    "120",
			description: "How many deliveries one webhook subscription is sent per minute, or 0 for no limit.",
			validate:    nonNegative,
		},
		DeliveryWorkers: {
			fallback:    "4",
			description: "How many deliveries are attempted at once across the server. Read at startup.",
			validate:    positive,
		},
		DeliveryHistoryDays: {
			fallback:    "30",
			description: "How many days of finished deliveries are kept for their owners to inspect.",
			validate:    positive,
		},
		IncomingWebhookPostsPerMinute: {
			fallback:    "60",
			description: "How many messages one incoming webhook may post per minute, or 0 for no limit.",
			validate:    nonNegative,
		},
	})
}
