package authz

/*
Permissions for letting programs act on the server.

Added to the catalogue from a file of their own, so this feature's permissions
sit beside what they govern. Neither is in the default role: each gives
something outside the server a standing way into it, which is a decision for
somebody an administrator chose rather than for everybody.
*/
const (
	/*
		PermChannelWebhooks allows adding and removing a channel's incoming
		webhooks.

		An incoming webhook is an address anybody holding it can post into
		the channel through, so creating one is handing out a way to speak
		there — grantable per channel, for the people who run it.
	*/
	PermChannelWebhooks Permission = "channel.webhooks.manage"

	/*
		PermApplicationsManage allows registering, approving, restricting and
		removing applications.

		Administrative: approving an application grants it permissions, so
		whoever holds this can grant what they hold to a program that is not
		them — and the floor that protects administration protects this too.
	*/
	PermApplicationsManage Permission = "server.applications.manage"
)

func init() {
	catalogue[PermChannelWebhooks] = Definition{
		Name:        PermChannelWebhooks,
		Description: "Add and remove a channel's incoming webhooks, through which outside systems post",
		Scopes:      []Scope{ScopeServer, ScopeChannel},
	}
	catalogue[PermApplicationsManage] = Definition{
		Name:           PermApplicationsManage,
		Description:    "Register, approve, restrict and remove applications, and decide what they may do",
		Scopes:         []Scope{ScopeServer},
		Administrative: true,
	}
}
