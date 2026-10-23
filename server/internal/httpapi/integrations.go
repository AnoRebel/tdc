package httpapi

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/applications"
	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/delivery"
	"github.com/AnoRebel/tdc/server/internal/notify"
	"github.com/AnoRebel/tdc/server/internal/projects"
	"github.com/AnoRebel/tdc/server/internal/settings"
	"github.com/AnoRebel/tdc/server/internal/webhooks"
)

/*
What programs acting on this server need the HTTP layer to hold.

Built on first use rather than at construction, so a server nobody has pointed
a webhook at carries none of it, and so adding it did not change how every test
and the command build a server.
*/
type integrationsState struct {
	once         sync.Once
	signer       *delivery.Signer
	client       *delivery.Client
	webhooks     *webhooks.Service
	applications *applications.Service
}

/*
maxDeliveryTimeout is the ceiling the outbound client is built with.

The operator's setting is applied per attempt, beneath it, so changing the
setting takes effect without a restart; this only stops a mistyped setting from
holding a worker for an hour.
*/
const maxDeliveryTimeout = 30 * time.Second

// buildIntegrations constructs everything once.
func (s *Server) buildIntegrations() {
	s.integrations.once.Do(func() {
		if s.rooms != nil {
			s.integrations.signer = delivery.NewSigner(s.rooms.Keyring())
		}
		s.integrations.client = delivery.NewClient(maxDeliveryTimeout, func() bool {
			return s.privateDeliveriesAllowed(context.Background())
		})

		// The queue needs the database it lives in and the rooms it reads
		// access from; a server built without either has no webhooks.
		if s.db == nil || s.rooms == nil {
			return
		}
		var policy webhooks.Settings
		if s.settings != nil {
			policy = s.settings
		}
		s.integrations.webhooks = webhooks.New(s.queries, webhooks.Config{
			Server:       s.serverName(),
			CanRead:      s.canRead,
			Holds:        s.holdsForWebhooks,
			Describe:     s.describeForWebhooks,
			AllowPrivate: s.privateDeliveriesAllowed,
			Send: func(ctx context.Context, endpoint, id, kind string, body, secret []byte) delivery.Outcome {
				outcome, err := s.sendSigned(ctx, endpoint, id, kind, body, secret)
				if err != nil {
					return delivery.Outcome{Error: err.Error()}
				}
				return outcome
			},
			Limiter:  s.limiter,
			Settings: policy,
			Disabled: s.tellOwnerWebhookDisabled,
		}, s.log)

		if s.identity != nil && s.apiTokens != nil {
			s.integrations.applications = applications.NewService(s.queries, s.identity, s.rooms,
				s.apiTokens, s.log)
		}
	})
}

// applicationService returns the application registry, or nil on a server
// without what it needs.
func (s *Server) applicationService() *applications.Service {
	s.buildIntegrations()
	return s.integrations.applications
}

// deliveries returns the signer and the outbound client.
func (s *Server) deliveries() (*delivery.Signer, *delivery.Client) {
	s.buildIntegrations()
	return s.integrations.signer, s.integrations.client
}

// webhookService returns the queue, or nil on a server without a database.
func (s *Server) webhookService() *webhooks.Service {
	s.buildIntegrations()
	return s.integrations.webhooks
}

/*
RunDeliveries delivers queued webhooks until ctx ends.

Started by Run alongside the listener. Exported so a test can run the same
workers against a handler it built without a listener, rather than a copy of
them that could drift.
*/
func (s *Server) RunDeliveries(ctx context.Context) {
	if service := s.webhookService(); service != nil {
		service.Run(ctx)
	}
}

/*
queueWebhooks hands a stored event to the subscriptions that want it.

Off the request, like search indexing and notifications: a message is posted
whether or not any webhook could be queued, and the person posting never waits
on anybody's endpoint.
*/
func (s *Server) queueWebhooks(ctx context.Context, event dbgen.Event) {
	service := s.webhookService()
	if service == nil {
		return
	}
	s.inBackground(ctx, "queue webhooks", func(ctx context.Context) {
		service.Enqueue(ctx, event)
	})
}

/*
canRead is whether somebody can read a room right now, which is what every
webhook decision about a room turns on.

For a person: they can see it, and channel.read has not been withdrawn from
them there — without the second, a webhook would go on carrying a channel to
somebody it had been closed to. A project's stream is as readable as the
project, which is membership or publicity and nothing a role grants.

For an application, its own rule, the one every request it makes is held to:
it reads only where it was put. A channel must be one of its own and its grant
must allow reading there; a project, or an issue's discussion, must be one it
was made a member of — a public project it was not put in is no more its
business than a public channel it was not given.
*/
func (s *Server) canRead(ctx context.Context, roomID, userID uuid.UUID) bool {
	room, err := s.queries.GetRoomByID(ctx, roomID)
	if err != nil {
		return false
	}
	projectID, inProject := projectOfRoom(ctx, s.queries, room)

	if application, ok := s.applicationAccount(ctx, userID); ok {
		if application.State != "approved" {
			return false
		}
		if inProject {
			if room.Kind == "issue-discussion" && !s.discussionVisible(ctx, roomID, userID) {
				return false
			}
			_, err := s.queries.GetProjectMemberRole(ctx, dbgen.GetProjectMemberRoleParams{
				ProjectID: projectID, UserID: userID,
			})
			return err == nil
		}
		service := s.applicationService()
		if service == nil {
			return false
		}
		channels, err := service.Channels(ctx, application.ID)
		if err != nil || !slices.Contains(channels, roomID) {
			return false
		}
		permissions, err := service.Permissions(ctx, application)
		return err == nil && permissions.AllowsChannel(authz.PermChannelRead, roomID).Allowed
	}

	if room.Kind == "project" {
		if s.projects == nil {
			return false
		}
		visible, err := s.projects.Visible(ctx, roomID, userID)
		return err == nil && visible
	}
	if _, err := s.rooms.Visible(ctx, roomID, userID); err != nil {
		return false
	}
	if s.authz == nil {
		return true
	}
	permissions, err := s.authz.PermissionsFor(ctx, userID)
	return err == nil && permissions.AllowsChannel(authz.PermChannelRead, roomID).Allowed
}

func (s *Server) discussionVisible(ctx context.Context, roomID, userID uuid.UUID) bool {
	if s.projects == nil {
		return false
	}
	visible, err := s.projects.DiscussionVisible(ctx, roomID, userID)
	return err == nil && visible
}

// projectOfRoom is the project a room belongs to — its stream, or one of its
// issues' discussions — if it belongs to one.
func projectOfRoom(ctx context.Context, queries *dbgen.Queries, room dbgen.Room) (uuid.UUID, bool) {
	switch room.Kind {
	case "project":
		return room.ID, true
	case "issue-discussion":
		issue, err := queries.GetIssueByID(ctx, room.ID)
		if err != nil {
			return uuid.Nil, false
		}
		return issue.ProjectID, true
	}
	return uuid.Nil, false
}

/*
holdsForWebhooks is whether somebody holds a server permission right now, for
the events that belong to no room. An application is asked about its grant,
never its account's roles, as on every request it makes.
*/
func (s *Server) holdsForWebhooks(ctx context.Context, userID uuid.UUID, permission string) bool {
	if application, ok := s.applicationAccount(ctx, userID); ok {
		service := s.applicationService()
		if service == nil || application.State != "approved" {
			return false
		}
		permissions, err := service.Permissions(ctx, application)
		return err == nil && permissions.AllowsServer(authz.Permission(permission)).Allowed
	}
	if s.authz == nil {
		return false
	}
	permissions, err := s.authz.PermissionsFor(ctx, userID)
	return err == nil && permissions.AllowsServer(authz.Permission(permission)).Allowed
}

// applicationAccount is the approved application an account acts for, if it
// acts for one.
func (s *Server) applicationAccount(ctx context.Context, userID uuid.UUID) (dbgen.Application, bool) {
	application, err := s.queries.GetApplicationByAccount(ctx, &userID)
	if err != nil {
		return dbgen.Application{}, false
	}
	// Reported whatever its state, so that an application removed or not yet
	// approved is refused as one rather than judged by its account's roles.
	return application, true
}

// describeForWebhooks says what a room belongs to, so a delivery can name the
// project and issue as well as the room.
func (s *Server) describeForWebhooks(ctx context.Context, roomID uuid.UUID) webhooks.Source {
	room, err := s.queries.GetRoomByID(ctx, roomID)
	if err != nil {
		return webhooks.Source{Kind: "channel"}
	}
	source := webhooks.Source{Kind: room.Kind}
	projectID, inProject := projectOfRoom(ctx, s.queries, room)
	if !inProject {
		return source
	}
	project, err := s.queries.GetProjectByID(ctx, projectID)
	if err != nil {
		return source
	}
	source.Project = &webhooks.Ref{ID: project.ID.String(), Key: project.Key}
	if room.Kind == "issue-discussion" {
		if issue, err := s.queries.GetIssueByID(ctx, room.ID); err == nil {
			source.Issue = &webhooks.Ref{ID: issue.ID.String(), Key: projects.Identifier(project, issue)}
		}
	}
	return source
}

/*
queueServerEvent hands an event that belongs to no room to the server-wide
subscriptions that asked for it, off the request, as every webhook is.
*/
func (s *Server) queueServerEvent(ctx context.Context, kind, sender string, content any) {
	service := s.webhookService()
	if service == nil {
		return
	}
	s.inBackground(ctx, "queue webhooks", func(ctx context.Context) {
		service.EnqueueServer(ctx, kind, sender, content)
	})
}

// accountRef is how an account appears in an event that belongs to no room:
// who it is, and never how to reach them.
func (s *Server) accountRef(user dbgen.User) map[string]any {
	return map[string]any{
		"id":          user.ID.String(),
		"username":    user.Username,
		"address":     s.rooms.UserAddress(user.Username),
		"displayName": user.DisplayName,
	}
}

// tellOwnerWebhookDisabled raises the notification that a webhook was turned
// off, attributed to the server rather than to any person. A notification
// belongs to a room and an event; a server subscription turned off over an
// event in no room says so only in its own state.
func (s *Server) tellOwnerWebhookDisabled(ctx context.Context, subscription dbgen.WebhookSubscription, eventID *string) {
	if s.notify == nil || eventID == nil {
		return
	}
	roomID := subscription.RoomID
	if roomID == nil {
		event, err := s.rooms.Event(ctx, *eventID)
		if err != nil {
			return
		}
		roomID = &event.RoomID
	}
	s.notify.About(ctx, subscription.OwnerID, uuid.Nil, s.rooms.UserAddress("webhooks"),
		*roomID, *eventID, notify.ReasonWebhookDisabled, subscription.Label)
}

// privateDeliveriesAllowed reads the operator's decision, defaulting to no.
func (s *Server) privateDeliveriesAllowed(ctx context.Context) bool {
	return s.settings != nil && s.settings.Get(ctx, settings.DeliveriesAllowPrivateAddresses) == "true"
}

// deliveryTimeout is how long one attempt may take, as the operator set it.
func (s *Server) deliveryTimeout(ctx context.Context) time.Duration {
	seconds := int64(5)
	if s.settings != nil {
		if configured := s.settings.Int(ctx, settings.DeliveryTimeoutSeconds); configured > 0 {
			seconds = configured
		}
	}
	if timeout := time.Duration(seconds) * time.Second; timeout < maxDeliveryTimeout {
		return timeout
	}
	return maxDeliveryTimeout
}
