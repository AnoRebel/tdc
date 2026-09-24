package audit

/*
Programs acting on this server, recorded under their own names.

Each of these gives something outside the server a standing way to read from
it or write into it — a webhook sends a channel's events somewhere, an incoming
webhook lets anybody holding its address post into a channel, an application
acts with permissions somebody granted it. Who set each up, and who took it
away, is exactly what somebody investigating unexpected activity needs to find.
*/
const (
	ActionWebhookSubscribed   Action = "integration.webhook.subscribed"
	ActionWebhookUnsubscribed Action = "integration.webhook.unsubscribed"
)
