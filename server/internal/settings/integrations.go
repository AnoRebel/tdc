package settings

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
)

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
	})
}
