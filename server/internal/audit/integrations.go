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

	ActionIncomingWebhookCreated Action = "integration.incoming_webhook.created"
	ActionIncomingWebhookRevoked Action = "integration.incoming_webhook.revoked"

	ActionApplicationRegistered       Action = "integration.application.registered"
	ActionApplicationApproved         Action = "integration.application.approved"
	ActionApplicationRestricted       Action = "integration.application.restricted"
	ActionApplicationRemoved          Action = "integration.application.removed"
	ActionApplicationCredentialIssued Action = "integration.application.credential_issued"
	ActionApplicationCredentialRevoke Action = "integration.application.credential_revoked"
)
