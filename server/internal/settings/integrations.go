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
	})
}
