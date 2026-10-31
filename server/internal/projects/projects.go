/*
Package projects owns projects and the issues filed in them.

Built on the chat platform rather than beside it. A project's history is a
room's event log — signed, ordered, verifiable — and an issue's discussion is a
room people talk in. What is genuinely new here is the tracking surface itself:
keys and identifiers, states, and the rules for who may move what.

The issue tables hold the current state of every issue as ordinary columns, for
the queries that need current state — a board, a list, a filter. They are
written only by the code that appends the event recording the change, in the
same transaction, so the two cannot disagree: the log is the truth and the
columns are its cache.
*/
package projects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/rooms"
)

// Errors a caller is expected to handle.
var (
	/*
		ErrProjectNotFound is returned both for a project that does not exist
		and for a private one the caller is not in. The two must be
		indistinguishable, or a private project's existence — and its key —
		could be probed.
	*/
	ErrProjectNotFound = errors.New("no such project")
	// ErrIssueNotFound is the same rule for issues: one in a project the
	// caller cannot see does not exist, as far as they can tell.
	ErrIssueNotFound = errors.New("no such issue")

	ErrKeyTaken    = errors.New("that project key is already taken")
	ErrInvalidKey  = errors.New("a project key is two to ten letters or digits, starting with a letter")
	ErrInvalidName = errors.New("a project needs a name of at most eighty characters")
	ErrVisibility  = errors.New("a project is either public or private")

	ErrStateNotFound  = errors.New("that state is not one of this project's")
	ErrStateNameTaken = errors.New("this project already has a state by that name")
	ErrStateName      = errors.New("a state needs a name of at most forty characters")
	/*
		ErrStateInUse refuses removing a state issues are still in, unless the
		caller says where they go. Deciding for them — the first state, say —
		would move work somebody was tracking into a column they did not choose.
	*/
	ErrStateInUse = errors.New("issues are still in this state; say which state they move to")
	// ErrLastState refuses removing the only state: an issue must be in one.
	ErrLastState = errors.New("a project needs at least one state")

	ErrTitle       = errors.New("an issue needs a title of at most two hundred characters")
	ErrDescription = errors.New("an issue's description is limited to twenty thousand characters")

	ErrNotAMember      = errors.New("that person is not in this project")
	ErrNoSuchPerson    = errors.New("there is nobody here by that name")
	ErrLastAdmin       = errors.New("a project needs at least one administrator")
	ErrMemberRole      = errors.New("a member's role is admin or member")
	ErrAlreadyDeleted  = errors.New("that issue has already been deleted")
	ErrReplicatedEvent = errors.New("a project's stream is written only by the server that hosts it")
)

/*
RefusalError is a permission the caller lacks.

Carries the permission so the response can name it, which is what lets
somebody ask for the right thing rather than guessing — the same wording
authz uses, so a client reads either the same way.
*/
type RefusalError struct{ Decision authz.Decision }

func (r RefusalError) Error() string { return r.Decision.Error() }

/*
ConflictError is a change decided against a value that has since moved.

Returned when two people move the same issue at once: the second is told what
the issue became, rather than having their move quietly win or quietly vanish.
*/
type ConflictError struct {
	Issue dbgen.Issue
}

func (c ConflictError) Error() string { return "this issue changed while you were looking at it" }

// Limits on what people write.
const (
	maxProjectName   = 80
	maxProjectDesc   = 2000
	maxStateName     = 40
	maxTitle         = 200
	maxDescription   = 20000
	maxKeyLength     = 10
	minKeyLength     = 2
	defaultIssuePage = 50
	maxIssuePage     = 200
)

// Service manages projects and issues.
type Service struct {
	pool    *db.Pool
	queries *dbgen.Queries
	rooms   *rooms.Service
	log     *slog.Logger

	// appended is told about each event in a project's stream once the
	// transaction that wrote it has committed. See OnAppended.
	appended func(context.Context, dbgen.Event)
	// uncommitted holds what each open transaction has appended, keyed by
	// the transaction's queries, until it commits or is abandoned.
	uncommitted sync.Map
}

/*
OnAppended sets what is told about each event in a project's stream, once it
is committed — never before, so nothing outside hears of a change that was
rolled back. Called once while the server is being built.
*/
func (s *Service) OnAppended(tell func(context.Context, dbgen.Event)) { s.appended = tell }

// NewService builds a projects service.
func NewService(pool *db.Pool, roomsService *rooms.Service, log *slog.Logger) *Service {
	return &Service{pool: pool, queries: dbgen.New(pool), rooms: roomsService, log: log}
}

/*
Actor is who is acting, and with what authority.

The permissions are resolved once per request by the caller, as everywhere
else on the server; this package consults them rather than resolving its own.
*/
type Actor struct {
	ID          uuid.UUID
	Permissions authz.Permissions
}

// NewProject describes a project to start.
type NewProject struct {
	Key         string
	Name        string
	Description string
	Visibility  string
	// States to start with, in order, the last being where finished work
	// goes. Empty means the usual three.
	States []NewState
}

// NewState describes a state to add.
type NewState struct {
	Name         string
	IsCompletion bool
}

// defaultStates are what a project starts with when nobody said otherwise.
// Three, because that is the smallest workflow that distinguishes waiting
// from under way from finished — which is what a board is for.
var defaultStates = []NewState{
	{Name: "To do"},
	{Name: "In progress"},
	{Name: "Done", IsCompletion: true},
}

/*
NormaliseKey returns a project key as it is stored, or why it cannot be one.

Letters are upper-cased rather than refused in lower case: somebody typing
"web" means WEB, and making them retype it teaches nothing. Anything else
outside the shape is refused, because the key is embedded in identifiers people
say aloud and paste into commit messages, and WEB-12 has to survive both.
*/
func NormaliseKey(key string) (string, error) {
	key, home, elsewhere := strings.Cut(strings.TrimSpace(key), "@")
	upper := strings.ToUpper(key)
	if len(upper) < minKeyLength || len(upper) > maxKeyLength {
		return "", ErrInvalidKey
	}
	for i, r := range upper {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return "", ErrInvalidKey
		}
	}
	if elsewhere {
		// A replica's key, which names the server it lives on.
		if home == "" || strings.ContainsAny(home, " /@") {
			return "", ErrInvalidKey
		}
		return upper + "@" + strings.ToLower(home), nil
	}
	return upper, nil
}

// Create starts a project, with the person creating it as its administrator.
func (s *Service) Create(ctx context.Context, actor Actor, input NewProject) (dbgen.Project, error) {
	if decision := actor.Permissions.AllowsServer(authz.PermProjectCreate); !decision.Allowed {
		return dbgen.Project{}, RefusalError{decision}
	}

	// A key naming another server belongs to a replica, never to a project
	// started here.
	if strings.Contains(input.Key, "@") {
		return dbgen.Project{}, ErrInvalidKey
	}
	key, err := NormaliseKey(input.Key)
	if err != nil {
		return dbgen.Project{}, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || utf8.RuneCountInString(name) > maxProjectName {
		return dbgen.Project{}, ErrInvalidName
	}
	description := strings.TrimSpace(input.Description)
	if utf8.RuneCountInString(description) > maxProjectDesc {
		return dbgen.Project{}, ErrInvalidName
	}
	visibility := input.Visibility
	if visibility == "" {
		visibility = "public"
	}
	if visibility != "public" && visibility != "private" {
		return dbgen.Project{}, ErrVisibility
	}

	states := input.States
	if len(states) == 0 {
		states = defaultStates
	}
	seen := map[string]bool{}
	for _, state := range states {
		folded, err := stateName(state.Name)
		if err != nil {
			return dbgen.Project{}, err
		}
		if seen[folded] {
			return dbgen.Project{}, ErrStateNameTaken
		}
		seen[folded] = true
	}

	var project dbgen.Project
	err = s.inTransaction(ctx, func(q *dbgen.Queries) error {
		creator, err := q.GetUserByID(ctx, actor.ID)
		if err != nil {
			return fmt.Errorf("read creator: %w", err)
		}

		// The stream first: the project's row refers to it.
		stream, err := q.CreateRoom(ctx, dbgen.CreateRoomParams{
			ID:         uuid.New(),
			Name:       name,
			Kind:       "project",
			Visibility: "private",
			HomeServer: s.rooms.ServerName(),
			CreatedBy:  &actor.ID,
		})
		if err != nil {
			return fmt.Errorf("create project stream: %w", err)
		}

		project, err = q.CreateProject(ctx, dbgen.CreateProjectParams{
			ID:          stream.ID,
			Key:         key,
			KeyFolded:   strings.ToLower(key),
			Name:        name,
			Description: description,
			Visibility:  visibility,
			CreatedBy:   &actor.ID,
		})
		if err != nil {
			if isUniqueViolation(err) {
				return ErrKeyTaken
			}
			return fmt.Errorf("create project: %w", err)
		}

		if err := q.AddProjectMember(ctx, dbgen.AddProjectMemberParams{
			ProjectID: project.ID, UserID: actor.ID, Role: "admin", AddedBy: &actor.ID,
		}); err != nil {
			return fmt.Errorf("add creator: %w", err)
		}

		/*
			Everything the row holds that the person chose or the server
			defaulted, so the stream alone says what the project was made as.
		*/
		if _, err := s.appendTo(ctx, q, stream, creator, events.TypeProjectCreated, map[string]any{
			"key": key, "name": name, "description": description, "visibility": visibility,
			"timeZone": project.TimeZone, "cyclePolicy": project.CyclePolicy,
		}); err != nil {
			return err
		}

		for position, state := range states {
			added, err := q.CreateIssueState(ctx, dbgen.CreateIssueStateParams{
				ID:           uuid.New(),
				ProjectID:    project.ID,
				Name:         strings.TrimSpace(state.Name),
				NameFolded:   strings.ToLower(strings.TrimSpace(state.Name)),
				Position:     int32(position + 1),
				IsCompletion: state.IsCompletion,
			})
			if err != nil {
				return fmt.Errorf("create state: %w", err)
			}
			if _, err := s.appendTo(ctx, q, stream, creator, events.TypeProjectStateAdded, stateContent(added)); err != nil {
				return err
			}
		}
		return s.seedPriorities(ctx, q, stream, creator)
	})
	return project, err
}

/*
Access is what one person may do in one project.

Resolved once per request, from three things: the person's server-wide and
project-scoped grants, their membership of the project, and — for a few acts on
a single issue — whether they wrote it.
*/
type Access struct {
	Project dbgen.Project
	// Role is "admin", "member", or empty for somebody who can see a public
	// project without being in it.
	Role        string
	userID      uuid.UUID
	permissions authz.Permissions
}

// memberGrants is what being in a project confers, beyond seeing it.
var memberGrants = map[authz.Permission]bool{
	authz.PermIssueCreate:     true,
	authz.PermIssueTransition: true,
	authz.PermIssueAssign:     true,
	authz.PermIssueLabel:      true,
	authz.PermIssueRelate:     true,
	authz.PermIssuePlan:       true,
	authz.PermProjectPlan:     true,
}

// adminGrants is what running a project confers: everything in it.
var adminGrants = map[authz.Permission]bool{
	authz.PermProjectAdminister:      true,
	authz.PermProjectConfigureStates: true,
	authz.PermIssueCreate:            true,
	authz.PermIssueTransition:        true,
	authz.PermIssueAssign:            true,
	authz.PermIssueLabel:             true,
	authz.PermIssueRelate:            true,
	authz.PermIssueDelete:            true,
	authz.PermIssuePlan:              true,
	authz.PermProjectPlan:            true,
}

/*
Allows reports whether the person may do something anywhere in the project.

A denial written for this project, or for the whole server, beats everything —
including what membership or administering the project would otherwise confer,
the same way a denial beats server administration. Then a grant, at either
scope. Then membership.
*/
func (a Access) Allows(permission authz.Permission) authz.Decision {
	// A replica is changed only by its stream: changes are made on its home.
	if IsReplica(a.Project) {
		return authz.Decision{Allowed: false, Permission: permission, Reason: "this project lives on " + HomeOf(a.Project) + ", and is changed there"}
	}
	if a.permissions.DeniedInProject(permission, a.Project.ID) {
		return authz.Decision{Allowed: false, Permission: permission, Reason: "denied for this project"}
	}
	if decision := a.permissions.AllowsProject(permission, a.Project.ID); decision.Allowed {
		return decision
	}
	switch {
	case a.Role == "admin" && adminGrants[permission]:
		return authz.Decision{Allowed: true, Permission: permission, Reason: "administers this project"}
	case a.Role == "member" && memberGrants[permission]:
		return authz.Decision{Allowed: true, Permission: permission, Reason: "member of this project"}
	}
	return authz.Decision{Allowed: false, Permission: permission, Reason: "not granted"}
}

// authorGrants is what writing an issue confers over that issue, for somebody
// not otherwise allowed: moving it — closing your own report is ordinary —
// and labelling it.
var authorGrants = map[authz.Permission]bool{
	authz.PermIssueTransition: true,
	authz.PermIssueLabel:      true,
}

/*
AllowsOn reports whether the person may do something to one issue.

Everything Allows would allow, and also a little over issues they wrote. That
difference is what makes a batch of changes come back mixed: somebody who is
not in a public project may still close and label the issues they filed there,
and nobody else's.
*/
func (a Access) AllowsOn(permission authz.Permission, issue dbgen.Issue) authz.Decision {
	decision := a.Allows(permission)
	if decision.Allowed || IsReplica(a.Project) || a.permissions.DeniedInProject(permission, a.Project.ID) {
		return decision
	}
	if authorGrants[permission] && issue.AuthorID != nil && *issue.AuthorID == a.userID {
		return authz.Decision{Allowed: true, Permission: permission, Reason: "wrote this issue"}
	}
	return decision
}

// MayEdit reports whether the person may change an issue's title and
// description: whoever wrote it, and whoever may file issues in the project.
func (a Access) MayEdit(issue dbgen.Issue) authz.Decision {
	decision := a.Allows(authz.PermIssueCreate)
	if decision.Allowed || IsReplica(a.Project) || a.permissions.DeniedInProject(authz.PermIssueCreate, a.Project.ID) {
		return decision
	}
	if issue.AuthorID != nil && *issue.AuthorID == a.userID {
		return authz.Decision{Allowed: true, Permission: authz.PermIssueCreate, Reason: "wrote this issue"}
	}
	return decision
}

/*
AccessWithRole is what somebody may do in a project, given their role in it
as a listing already read it.

For a list of projects, where each row carries the reader's role: resolving
access again per row would be a query per project for an answer already in
hand. Only for a project the list itself established the reader may see.
*/
func AccessWithRole(actor Actor, project dbgen.Project, role string) Access {
	return Access{Project: project, Role: role, userID: actor.ID, permissions: actor.Permissions}
}

// UserID is who this access belongs to.
func (a Access) UserID() uuid.UUID { return a.userID }

// ProjectByKey returns a project the actor may see, with what they may do in it.
func (s *Service) ProjectByKey(ctx context.Context, actor Actor, key string) (Access, error) {
	project, err := s.queries.GetProjectByKey(ctx, strings.ToLower(strings.TrimSpace(key)))
	if errors.Is(err, pgx.ErrNoRows) {
		return Access{}, ErrProjectNotFound
	}
	if err != nil {
		return Access{}, fmt.Errorf("read project: %w", err)
	}
	return s.accessTo(ctx, actor, project)
}

// ProjectByID is ProjectByKey for a caller holding an identifier.
func (s *Service) ProjectByID(ctx context.Context, actor Actor, id uuid.UUID) (Access, error) {
	project, err := s.queries.GetProjectByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Access{}, ErrProjectNotFound
	}
	if err != nil {
		return Access{}, fmt.Errorf("read project: %w", err)
	}
	return s.accessTo(ctx, actor, project)
}

// accessTo resolves what somebody may do in a project, refusing one they may
// not see as though it did not exist.
func (s *Service) accessTo(ctx context.Context, actor Actor, project dbgen.Project) (Access, error) {
	role, err := s.queries.GetProjectMemberRole(ctx, dbgen.GetProjectMemberRoleParams{
		ProjectID: project.ID, UserID: actor.ID,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Access{}, fmt.Errorf("read membership: %w", err)
	}

	if project.Visibility != "public" && role == "" {
		return Access{}, ErrProjectNotFound
	}

	return Access{Project: project, Role: role, userID: actor.ID, permissions: actor.Permissions}, nil
}

/*
Visible reports whether somebody may see a project at all.

For the rooms that belong to a project, which ask this rather than keeping
membership of their own. Deliberately independent of any permission: seeing is
membership or publicity, and nothing a role grants makes a private project
visible to somebody outside it.
*/
func (s *Service) Visible(ctx context.Context, projectID, userID uuid.UUID) (bool, error) {
	project, err := s.queries.GetProjectByID(ctx, projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read project: %w", err)
	}
	if project.Visibility == "public" {
		return true, nil
	}
	_, err = s.queries.GetProjectMemberRole(ctx, dbgen.GetProjectMemberRoleParams{
		ProjectID: projectID, UserID: userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read membership: %w", err)
	}
	return true, nil
}

// ListVisible returns the projects somebody may see, with their role in each.
func (s *Service) ListVisible(ctx context.Context, userID uuid.UUID) ([]dbgen.ListProjectsForUserRow, error) {
	projects, err := s.queries.ListProjectsForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	return projects, nil
}

// ProjectChanges describes a change to a project's own details. Nil fields
// are left as they are.
type ProjectChanges struct {
	Name        *string
	Description *string
	Visibility  *string
}

/*
Update renames a project or changes who can see it.

The key is not among what can change: identifiers people have already written
down must keep meaning the same issues.
*/
func (s *Service) Update(ctx context.Context, actor Actor, access Access, changes ProjectChanges) (dbgen.Project, error) {
	if decision := access.Allows(authz.PermProjectAdminister); !decision.Allowed {
		return dbgen.Project{}, RefusalError{decision}
	}

	project := access.Project
	name, description, visibility := project.Name, project.Description, project.Visibility
	if changes.Name != nil {
		name = strings.TrimSpace(*changes.Name)
		if name == "" || utf8.RuneCountInString(name) > maxProjectName {
			return dbgen.Project{}, ErrInvalidName
		}
	}
	if changes.Description != nil {
		description = strings.TrimSpace(*changes.Description)
		if utf8.RuneCountInString(description) > maxProjectDesc {
			return dbgen.Project{}, ErrInvalidName
		}
	}
	if changes.Visibility != nil {
		visibility = *changes.Visibility
		if visibility != "public" && visibility != "private" {
			return dbgen.Project{}, ErrVisibility
		}
	}

	var updated dbgen.Project
	err := s.inTransaction(ctx, func(q *dbgen.Queries) error {
		var err error
		updated, err = q.UpdateProject(ctx, dbgen.UpdateProjectParams{
			ID: project.ID, Name: name, Description: description, Visibility: visibility,
		})
		if err != nil {
			return fmt.Errorf("update project: %w", err)
		}

		changed := map[string]any{}
		if name != project.Name {
			changed["name"] = map[string]any{"previous": project.Name, "new": name}
		}
		if description != project.Description {
			changed["description"] = map[string]any{"previous": project.Description, "new": description}
		}
		if visibility != project.Visibility {
			changed["visibility"] = map[string]any{"previous": project.Visibility, "new": visibility}
		}
		if len(changed) == 0 {
			return nil
		}

		/*
			Becoming private is a revocation for everybody outside it.

			Anybody not in the project who was taking part in an issue's
			discussion loses the room with it — their membership row would
			otherwise keep delivering notifications about a conversation they
			may no longer read.
		*/
		if visibility == "private" && project.Visibility != "private" {
			if err := q.RemoveDiscussionOutsiders(ctx, project.ID); err != nil {
				return fmt.Errorf("remove outsiders from discussions: %w", err)
			}
		}

		return s.appendProjectEvent(ctx, q, project.ID, actor.ID, events.TypeProjectUpdated, changed)
	})
	return updated, err
}

/*
inTransaction runs work that must happen entirely or not at all.

An issue's columns and the event that justifies them are written here together,
which is what lets the columns be trusted as a cache of the log.
*/
func (s *Service) inTransaction(ctx context.Context, work func(*dbgen.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := dbgen.New(tx)
	var appended []dbgen.Event
	s.uncommitted.Store(q, &appended)
	defer s.uncommitted.Delete(q)

	if err := work(q); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	if s.appended != nil {
		for _, event := range appended {
			s.appended(ctx, event)
		}
	}
	return nil
}

// stored notes an event appended through q, to be told about when q's
// transaction commits — or now, if q is not a transaction's.
func (s *Service) stored(ctx context.Context, q *dbgen.Queries, event dbgen.Event) {
	if pending, ok := s.uncommitted.Load(q); ok {
		list := pending.(*[]dbgen.Event)
		*list = append(*list, event)
		return
	}
	if s.appended != nil {
		s.appended(ctx, event)
	}
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "SQLSTATE 23505")
}

func stateName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || utf8.RuneCountInString(trimmed) > maxStateName {
		return "", ErrStateName
	}
	return strings.ToLower(trimmed), nil
}

func stateContent(state dbgen.IssueState) map[string]any {
	return map[string]any{
		"state":        state.ID.String(),
		"name":         state.Name,
		"position":     state.Position,
		"isCompletion": state.IsCompletion,
	}
}

// encode is json.Marshal for content this package built itself.
func encode(content any) (json.RawMessage, error) {
	encoded, err := json.Marshal(content)
	if err != nil {
		return nil, fmt.Errorf("encode event content: %w", err)
	}
	return encoded, nil
}
