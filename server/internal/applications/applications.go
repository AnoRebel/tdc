/*
Package applications installs programs on a server as themselves.

An application is registered — named, its purpose stated, the permissions it
wants listed, and anything it sends off the server disclosed — and then does
nothing at all until somebody allowed to approves it, having been shown exactly
what it will hold. Approval gives it an account to act through, the channels it
may act in, and a credential. Removal takes all of that away at once, and,
if the operator chooses, what it posted.

Its authority is its grant. Not the roles its account happens to hold, and
above all not the permissions of whoever approved it: an administrator approving
an application that asked to post messages has approved an application that can
post messages, and nothing an administrator can do comes with it.
*/
package applications

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/apitokens"
	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/identity"
	"github.com/AnoRebel/tdc/server/internal/rooms"
)

var (
	// ErrNotFound reports an application that does not exist, or is gone.
	ErrNotFound = errors.New("no such application")
	// ErrInvalid reports a registration missing what an approver needs.
	ErrInvalid = errors.New("that is not an application this server can register")
	// ErrNotRegistered reports approving one that is not waiting for approval.
	ErrNotRegistered = errors.New("that application is not waiting to be approved")
	// ErrRequestChanged reports approving permissions other than the ones the
	// application currently asks for — which is what approving a request one
	// has not been shown would look like.
	ErrRequestChanged = errors.New("those are not the permissions this application asks for")
	// ErrNotHeld reports an approver granting what they do not hold.
	ErrNotHeld = errors.New("you cannot grant an application a permission you do not hold")
	// ErrNoChannels reports an application that would act in channels and has
	// been given none.
	ErrNoChannels = errors.New("an application that acts in channels must be given at least one")
	// ErrUnknownChannel reports a channel that does not exist.
	ErrUnknownChannel = errors.New("no such channel")
)

// Service manages applications.
type Service struct {
	store  dbgen.Querier
	people *identity.Service
	rooms  *rooms.Service
	tokens *apitokens.Service
	log    *slog.Logger
}

// NewService builds the service.
func NewService(store dbgen.Querier, people *identity.Service, conversations *rooms.Service,
	tokens *apitokens.Service, log *slog.Logger) *Service {
	return &Service{store: store, people: people, rooms: conversations, tokens: tokens, log: log}
}

// NewApplication is a registration.
type NewApplication struct {
	Name                string
	Purpose             string
	Permissions         []string
	SendsDataExternally bool
	// ExternalDisclosure says what leaves the server and where it goes.
	// Required when SendsDataExternally is set.
	ExternalDisclosure string
}

const (
	maxName    = 60
	maxPurpose = 500
)

/*
Register records an application, which can then do nothing until approved.

The permissions are checked to exist and kept sorted, so the list an approver
is shown and the list approval compares against are the same list.
*/
func (s *Service) Register(ctx context.Context, by uuid.UUID, input NewApplication) (dbgen.Application, error) {
	name := strings.TrimSpace(input.Name)
	purpose := strings.TrimSpace(input.Purpose)
	disclosure := strings.TrimSpace(input.ExternalDisclosure)
	switch {
	case name == "" || utf8.RuneCountInString(name) > maxName:
		return dbgen.Application{}, fmt.Errorf("%w: a name of at most %d characters is required", ErrInvalid, maxName)
	case purpose == "" || utf8.RuneCountInString(purpose) > maxPurpose:
		return dbgen.Application{}, fmt.Errorf("%w: say what it is for, in at most %d characters", ErrInvalid, maxPurpose)
	case input.SendsDataExternally && disclosure == "":
		return dbgen.Application{}, fmt.Errorf(
			"%w: an application that sends data elsewhere must say what it sends and where", ErrInvalid)
	}

	permissions, err := normalise(input.Permissions)
	if err != nil {
		return dbgen.Application{}, err
	}

	return s.store.CreateApplication(ctx, dbgen.CreateApplicationParams{
		ID:                   uuid.New(),
		Name:                 name,
		Purpose:              purpose,
		RequestedPermissions: permissions,
		SendsDataExternally:  input.SendsDataExternally,
		ExternalDisclosure:   disclosure,
		RegisteredBy:         &by,
	})
}

// normalise refuses unknown permissions and returns the rest sorted, once each.
func normalise(requested []string) ([]string, error) {
	permissions := make([]string, 0, len(requested))
	for _, name := range requested {
		name = strings.TrimSpace(name)
		if _, known := authz.Lookup(authz.Permission(name)); !known {
			return nil, fmt.Errorf("%w: %q is not a permission this server has", ErrInvalid, name)
		}
		if !slices.Contains(permissions, name) {
			permissions = append(permissions, name)
		}
	}
	slices.Sort(permissions)
	return permissions, nil
}

// Get returns an application that has not been removed.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (dbgen.Application, error) {
	application, err := s.store.GetApplication(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.Application{}, ErrNotFound
	}
	return application, err
}

// Approval is what approving gives back: the application, and its credential,
// shown once.
type Approval struct {
	Application dbgen.Application
	Token       apitokens.Token
	Secret      string
}

/*
Approve grants an application exactly what it asked for, in the channels given.

shown is the permission list the approver was shown. It must be the list the
application asks for now; the database compares the two as part of the update,
so a request changed after it was read is refused rather than approved unseen.

The approver must hold every permission themselves. Approval delegates — it is
how a permission somebody has reaches a program — and nobody can delegate what
they do not have.
*/
func (s *Service) Approve(
	ctx context.Context,
	id uuid.UUID,
	approver dbgen.User,
	held authz.Permissions,
	shown []string,
	channels []uuid.UUID,
) (Approval, error) {
	application, err := s.Get(ctx, id)
	if err != nil {
		return Approval{}, err
	}
	if application.State != "registered" {
		return Approval{}, ErrNotRegistered
	}

	shownSorted, err := normalise(shown)
	if err != nil || !slices.Equal(shownSorted, application.RequestedPermissions) {
		return Approval{}, ErrRequestChanged
	}
	for _, permission := range application.RequestedPermissions {
		if !held.AllowsServer(authz.Permission(permission)).Allowed {
			return Approval{}, fmt.Errorf("%w: %q", ErrNotHeld, permission)
		}
	}
	if needsChannels(application.RequestedPermissions) && len(channels) == 0 {
		return Approval{}, ErrNoChannels
	}
	for _, roomID := range channels {
		if _, err := s.store.GetRoomByID(ctx, roomID); err != nil {
			return Approval{}, fmt.Errorf("%w: %s", ErrUnknownChannel, roomID)
		}
	}

	account, err := s.people.CreateUser(ctx, identity.NewUser{
		Username:    accountName(application),
		DisplayName: application.Name,
	})
	if err != nil {
		return Approval{}, fmt.Errorf("create the application's account: %w", err)
	}

	approved, err := s.store.ApproveApplication(ctx, dbgen.ApproveApplicationParams{
		ID:         id,
		ApprovedBy: &approver.ID,
		AccountID:  &account.ID,
		Shown:      shownSorted,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Approval{}, ErrRequestChanged
	}
	if err != nil {
		return Approval{}, fmt.Errorf("approve: %w", err)
	}

	if err := s.Restrict(ctx, approved, channels); err != nil {
		return Approval{}, err
	}

	token, secret, err := s.IssueCredential(ctx, approved)
	if err != nil {
		return Approval{}, err
	}
	return Approval{Application: approved, Token: token, Secret: secret}, nil
}

// needsChannels reports whether any permission is one a channel grants, which
// an application could only exercise somewhere.
func needsChannels(permissions []string) bool {
	for _, permission := range permissions {
		if authz.GrantableAt(authz.Permission(permission), authz.ScopeChannel) {
			return true
		}
	}
	return false
}

var unsafeName = regexp.MustCompile(`[^a-z0-9]+`)

// accountName derives the account's username: recognisably an application's,
// and unique by the identifier it ends with.
func accountName(application dbgen.Application) string {
	slug := strings.Trim(unsafeName.ReplaceAllString(strings.ToLower(application.Name), "-"), "-")
	if len(slug) > 16 {
		slug = strings.Trim(slug[:16], "-")
	}
	if slug == "" {
		slug = "application"
	}
	return "app-" + slug + "-" + strings.ReplaceAll(application.ID.String(), "-", "")[:6]
}

/*
Restrict sets the channels an application may act in, and nothing else.

Its account joins the channels added and leaves the ones taken away, so its
membership — which is what decides what it receives and where it can post — is
always exactly its restriction. Joining goes the way an invitation does,
because the application's approval is the authority a private channel admits it
by.
*/
func (s *Service) Restrict(ctx context.Context, application dbgen.Application, channels []uuid.UUID) error {
	if application.AccountID == nil {
		return ErrNotRegistered
	}
	before, err := s.store.ListApplicationChannels(ctx, application.ID)
	if err != nil {
		return fmt.Errorf("read channels: %w", err)
	}

	if err := s.store.ClearApplicationChannels(ctx, application.ID); err != nil {
		return fmt.Errorf("clear channels: %w", err)
	}
	for _, roomID := range channels {
		if err := s.store.SetApplicationChannel(ctx, dbgen.SetApplicationChannelParams{
			ApplicationID: application.ID, RoomID: roomID,
		}); err != nil {
			return fmt.Errorf("allow a channel: %w", err)
		}
		if err := s.rooms.AddMemberByInvitation(ctx, roomID, *application.AccountID); err != nil {
			return fmt.Errorf("join a channel: %w", err)
		}
	}
	for _, roomID := range before {
		if !slices.Contains(channels, roomID) {
			if err := s.rooms.Leave(ctx, roomID, *application.AccountID); err != nil &&
				!errors.Is(err, rooms.ErrNotAMember) {
				return fmt.Errorf("leave a channel: %w", err)
			}
		}
	}
	return nil
}

// Channels lists where an application may act.
func (s *Service) Channels(ctx context.Context, id uuid.UUID) ([]uuid.UUID, error) {
	return s.store.ListApplicationChannels(ctx, id)
}

/*
Permissions resolves what an application may do, from its grant alone.

A permission a channel can grant is granted in each of its channels and nowhere
else; one only the server grants is granted at server scope. Everything else is
explicitly denied, which in this model beats even server administration — so a
grant that somehow included it could still not reach past the list.
*/
func (s *Service) Permissions(ctx context.Context, application dbgen.Application) (authz.Permissions, error) {
	channels, err := s.store.ListApplicationChannels(ctx, application.ID)
	if err != nil {
		return authz.Permissions{}, fmt.Errorf("read channels: %w", err)
	}

	granted := map[authz.Permission]bool{}
	for _, name := range application.GrantedPermissions {
		granted[authz.Permission(name)] = true
	}

	grants := make([]authz.Grant, 0, len(authz.Catalogue())+len(channels))
	for _, definition := range authz.Catalogue() {
		permission := definition.Name
		switch {
		case !granted[permission]:
			grants = append(grants, authz.Grant{Permission: permission, Granted: false})
		case authz.GrantableAt(permission, authz.ScopeChannel):
			for _, roomID := range channels {
				grants = append(grants, authz.Grant{Permission: permission, ChannelID: &roomID, Granted: true})
			}
		default:
			grants = append(grants, authz.Grant{Permission: permission, Granted: true})
		}
	}
	return authz.Resolve(grants), nil
}

// IssueCredential gives an approved application a new credential, shown once.
// More than one may be live at a time, which is what lets one be rotated
// without the application going quiet in between.
func (s *Service) IssueCredential(ctx context.Context, application dbgen.Application) (apitokens.Token, string, error) {
	if application.State != "approved" || application.AccountID == nil {
		return apitokens.Token{}, "", ErrNotRegistered
	}
	scope := make([]authz.Permission, 0, len(application.GrantedPermissions))
	for _, name := range application.GrantedPermissions {
		scope = append(scope, authz.Permission(name))
	}
	return s.tokens.IssueForApplication(ctx, application.ID, *application.AccountID,
		application.Name+" credential", scope)
}

// Disposition is what removal does with what an application posted.
type Disposition string

const (
	// Retain keeps its messages, still marked as its.
	Retain Disposition = "retain"
	// Erase removes them, leaving the placeholder any removed message leaves.
	Erase Disposition = "remove"
)

// Removal is what removing an application did.
type Removal struct {
	Application dbgen.Application
	// Removed are the messages taken down, for announcing to their rooms.
	Removed []dbgen.Event
	// Left are the membership events from leaving its channels.
	Left []uuid.UUID
}

/*
Remove ends an application: its credentials stop working at once, what it
remembered is discarded, it leaves its channels, and what it posted is kept or
removed as the operator chose.

Credentials first, before anything slower, so the application cannot act in
the window while the rest happens.
*/
func (s *Service) Remove(ctx context.Context, id uuid.UUID, by dbgen.User, disposition Disposition) (Removal, error) {
	if disposition != Retain && disposition != Erase {
		return Removal{}, fmt.Errorf("%w: say whether to retain or remove what it posted", ErrInvalid)
	}

	removed, err := s.store.RemoveApplication(ctx, dbgen.RemoveApplicationParams{ID: id, RemovedBy: &by.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Removal{}, ErrNotFound
	}
	if err != nil {
		return Removal{}, fmt.Errorf("remove: %w", err)
	}
	if err := s.store.RevokeApplicationTokens(ctx, &id); err != nil {
		return Removal{}, fmt.Errorf("revoke credentials: %w", err)
	}

	result := Removal{Application: removed}
	if removed.AccountID == nil {
		return result, nil
	}
	account := *removed.AccountID

	if err := s.rooms.ForgetAllContext(ctx, account); err != nil {
		s.log.Warn("could not discard an application's context", "application", id, "error", err)
	}

	if disposition == Erase {
		posted, err := s.store.ListEventIDsBySender(ctx, &account)
		if err != nil {
			return result, fmt.Errorf("find what it posted: %w", err)
		}
		for _, message := range posted {
			event, err := s.rooms.RemoveAsOperator(ctx, message.ID, by)
			if err != nil {
				s.log.Warn("could not remove an application's message", "event", message.ID, "error", err)
				continue
			}
			result.Removed = append(result.Removed, event)
		}
	}

	channels, err := s.store.ListApplicationChannels(ctx, id)
	if err == nil {
		for _, roomID := range channels {
			if err := s.rooms.Leave(ctx, roomID, account); err == nil {
				result.Left = append(result.Left, roomID)
			}
		}
	}
	return result, nil
}
