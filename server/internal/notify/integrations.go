package notify

/*
ReasonWebhookDisabled tells somebody an outgoing webhook of theirs was turned
off after failing for long enough.

A notification rather than a mechanism of its own: it is something its owner
needs to know and act on, which is what notifications are, and it arrives where
they already look.
*/
const ReasonWebhookDisabled Reason = "webhook_disabled"
