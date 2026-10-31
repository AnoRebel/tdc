package projects

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/identity"
)

/*
A replica: a project another server is home to, held here for the people of
this server who are in it.

Its tables are written by Replicate alone, from its stream alone. Nothing a
person here does changes them — a change is a request to the home server, and
comes back as an event — so they say exactly what the stream says, and a
replica rebuilt from the start holds the same rows as one that followed along.
*/

// IsReplica reports whether a project is held here for another server.
func IsReplica(project dbgen.Project) bool { return strings.Contains(project.KeyFolded, "@") }

// HomeOf is the server a project lives on, or empty for one that lives here.
func HomeOf(project dbgen.Project) string {
	_, home, _ := strings.Cut(project.KeyFolded, "@")
	return home
}

/*
Replicate brings a replica up to what its stream says.

The whole stream is replayed each time rather than only what is new: the
projector starts from the latest baseline, so this is the events since then,
and it cannot drift from what a replica rebuilt from nothing would hold.

ponytail: replays the stream on every delivery; apply only the new events to
CurrentRows if large projects make that slow.

A stream the projector cannot apply — an event it has no rule for, from a
newer home server — stops the replica where it is, and says why on the
project, rather than skipping and quietly disagreeing with its home.
*/
func (s *Service) Replicate(ctx context.Context, room dbgen.Room) error {
	if room.Kind != "project" || s.rooms.HostedHere(room) {
		return nil
	}
	stream, err := s.Stream(ctx, room.ID)
	if err != nil {
		return err
	}
	rows, err := RowsFromStream(stream)
	if err != nil {
		if recordErr := s.queries.SetReplicaError(ctx, dbgen.SetReplicaErrorParams{
			ID: room.ID, ReplicaError: err.Error(),
		}); recordErr != nil {
			return errors.Join(err, recordErr)
		}
		return fmt.Errorf("replica of %s stopped: %w", room.ID, err)
	}
	if rows.Project.Key == "" {
		// Nothing has created it yet: its first events are still on the way.
		return nil
	}
	return s.inTransaction(ctx, func(q *dbgen.Queries) error {
		return s.writeReplica(ctx, q, room, rows)
	})
}

// replicaPeople resolves the addresses a replica's stream names: somebody
// here to their account, anybody else to nothing.
type replicaPeople struct {
	ctx    context.Context
	q      *dbgen.Queries
	server string
	known  map[string]*uuid.UUID
}

/*
local is the account of somebody here, by address. Somebody here the stream
names who has since gone has no account and is not anybody elsewhere either,
so both answers are false.
*/
func (p *replicaPeople) local(address string) (id *uuid.UUID, here bool) {
	name, host, _ := strings.Cut(address, "@")
	if !strings.EqualFold(host, p.server) {
		return nil, false
	}
	if id, seen := p.known[address]; seen {
		return id, id != nil
	}
	user, err := p.q.GetUserByUsername(p.ctx, identity.NormaliseUsername(name))
	if err != nil {
		p.known[address] = nil
		return nil, false
	}
	p.known[address] = &user.ID
	return &user.ID, true
}

// elsewhere is whether an address is somebody on another server.
func (p *replicaPeople) elsewhere(address string) (server string, ok bool) {
	_, host, found := strings.Cut(address, "@")
	if !found || host == "" || strings.EqualFold(host, p.server) {
		return "", false
	}
	return strings.ToLower(host), true
}

// who is somebody's account here, if they have one: the attribution columns
// of a replica name only people of this server.
func (p *replicaPeople) who(address string) *uuid.UUID {
	id, _ := p.local(address)
	return id
}

/*
writeReplica makes a replica's tables say what its rows say.

In an order that keeps every foreign key satisfied at each step: the project,
its members and definitions; then its issues, now pointing only at definitions
that exist; then what hangs from the issues, cleared and written again; and
last the definitions nothing points at any more.
*/
func (s *Service) writeReplica(ctx context.Context, q *dbgen.Queries, room dbgen.Room, rows Rows) error {
	projectID := room.ID
	home := strings.ToLower(room.HomeServer)
	key := rows.Project.Key + "@" + home
	people := &replicaPeople{ctx: ctx, q: q, server: s.rooms.ServerName(), known: map[string]*uuid.UUID{}}

	if err := q.UpsertReplicaProject(ctx, dbgen.UpsertReplicaProjectParams{
		ID: projectID, Key: key, KeyFolded: strings.ToLower(key), Name: rows.Project.Name,
		Description: rows.Project.Description, Visibility: rows.Project.Visibility,
		TimeZone: rows.Project.TimeZone, CyclePolicy: rows.Project.CyclePolicy,
		NextIssueNumber: rows.Project.NextIssueNumber, Federated: rows.Project.Federated,
	}); err != nil {
		return fmt.Errorf("write project: %w", err)
	}

	if err := q.ClearReplicaMembers(ctx, projectID); err != nil {
		return fmt.Errorf("clear members: %w", err)
	}
	if err := q.ClearReplicaRemoteMembers(ctx, projectID); err != nil {
		return fmt.Errorf("clear remote members: %w", err)
	}
	for address, member := range rows.Members {
		if id, here := people.local(address); here {
			if err := q.AddProjectMember(ctx, dbgen.AddProjectMemberParams{
				ProjectID: projectID, UserID: *id, Role: member.Role, AddedBy: people.who(member.AddedBy),
			}); err != nil {
				return fmt.Errorf("write member: %w", err)
			}
		} else if server, ok := people.elsewhere(address); ok {
			if err := q.AddProjectRemoteMember(ctx, dbgen.AddProjectRemoteMemberParams{
				ProjectID: projectID, Address: address, HomeServer: server, Role: member.Role,
				AddedBy: people.who(member.AddedBy),
			}); err != nil {
				return fmt.Errorf("write remote member: %w", err)
			}
		}
	}

	keep := struct{ states, labels, priorities, cycles, milestones []uuid.UUID }{}
	for id, state := range rows.States {
		keep.states = append(keep.states, id)
		if err := q.UpsertReplicaState(ctx, dbgen.UpsertReplicaStateParams{
			ID: id, ProjectID: projectID, Name: state.Name, NameFolded: strings.ToLower(state.Name),
			Position: state.Position, IsCompletion: state.IsCompletion,
		}); err != nil {
			return fmt.Errorf("write state: %w", err)
		}
	}
	for id, label := range rows.Labels {
		keep.labels = append(keep.labels, id)
		if err := q.UpsertReplicaLabel(ctx, dbgen.UpsertReplicaLabelParams{
			ID: id, ProjectID: projectID, Name: label.Name, NameFolded: strings.ToLower(label.Name), Colour: label.Colour,
		}); err != nil {
			return fmt.Errorf("write label: %w", err)
		}
	}
	for id, priority := range rows.Priorities {
		keep.priorities = append(keep.priorities, id)
		if err := q.UpsertReplicaPriority(ctx, dbgen.UpsertReplicaPriorityParams{
			ID: id, ProjectID: projectID, Name: priority.Name, NameFolded: strings.ToLower(priority.Name), Rank: priority.Rank,
		}); err != nil {
			return fmt.Errorf("write priority: %w", err)
		}
	}
	for id, cycle := range rows.Cycles {
		keep.cycles = append(keep.cycles, id)
		starts, err := ParseDay(cycle.StartsOn)
		if err != nil {
			return err
		}
		ends, err := ParseDay(cycle.EndsOn)
		if err != nil {
			return err
		}
		var policy *string
		if cycle.ClosedPolicy != "" {
			policy = &cycle.ClosedPolicy
		}
		if err := q.UpsertReplicaCycle(ctx, dbgen.UpsertReplicaCycleParams{
			ID: id, ProjectID: projectID, Name: cycle.Name, StartsOn: starts.pg(), EndsOn: ends.pg(),
			StartsAt: cycle.StartsAt, EndsAt: cycle.EndsAt, ClosedAt: cycle.ClosedAt, ClosedPolicy: policy,
		}); err != nil {
			return fmt.Errorf("write cycle: %w", err)
		}
	}
	for id, milestone := range rows.Milestones {
		keep.milestones = append(keep.milestones, id)
		params := dbgen.UpsertReplicaMilestoneParams{
			ID: id, ProjectID: projectID, Name: milestone.Name, Description: milestone.Description, DueAt: milestone.DueAt,
		}
		if milestone.TargetDate != "" {
			day, err := ParseDay(milestone.TargetDate)
			if err != nil {
				return err
			}
			params.TargetDate = day.pg()
		}
		if err := q.UpsertReplicaMilestone(ctx, params); err != nil {
			return fmt.Errorf("write milestone: %w", err)
		}
	}

	for id, issue := range rows.Issues {
		// Its discussion first: an issue is its discussion room's identifier.
		if err := q.AdoptReplicaRoom(ctx, dbgen.AdoptReplicaRoomParams{
			ID: id, Name: key + "-" + strconv.FormatInt(issue.Number, 10), Kind: "issue-discussion",
			Visibility: "private", HomeServer: home,
		}); err != nil {
			return fmt.Errorf("adopt discussion: %w", err)
		}
		author, here := people.local(issue.Author)
		var authorAddress *string
		if !here && issue.Author != "" {
			authorAddress = &issue.Author
		}
		if err := q.UpsertReplicaIssue(ctx, dbgen.UpsertReplicaIssueParams{
			ID: id, ProjectID: projectID, Number: issue.Number, Title: issue.Title, Description: issue.Description,
			StateID: issue.State, AuthorID: author, AuthorAddress: authorAddress, PriorityID: issue.Priority,
			Deleted: issue.Deleted,
		}); err != nil {
			return fmt.Errorf("write issue: %w", err)
		}
	}

	if err := q.ClearReplicaIssueLinks(ctx, projectID); err != nil {
		return fmt.Errorf("clear issue links: %w", err)
	}
	for id, issue := range rows.Issues {
		for label := range issue.Labels {
			if _, err := q.LabelIssue(ctx, dbgen.LabelIssueParams{IssueID: id, LabelID: label}); err != nil {
				return fmt.Errorf("write issue label: %w", err)
			}
		}
		for assignee, by := range issue.Assignees {
			if user, here := people.local(assignee); here {
				if _, err := q.AssignIssue(ctx, dbgen.AssignIssueParams{IssueID: id, UserID: *user, AssignedBy: people.who(by)}); err != nil {
					return fmt.Errorf("write assignee: %w", err)
				}
			} else if server, ok := people.elsewhere(assignee); ok {
				if err := q.AssignRemoteToIssue(ctx, dbgen.AssignRemoteToIssueParams{IssueID: id, Address: assignee, HomeServer: server}); err != nil {
					return fmt.Errorf("write remote assignee: %w", err)
				}
			}
		}
		if issue.Cycle != nil {
			if err := q.SetIssueCycle(ctx, dbgen.SetIssueCycleParams{IssueID: id, CycleID: *issue.Cycle}); err != nil {
				return fmt.Errorf("write issue cycle: %w", err)
			}
		}
		if issue.Milestone != nil {
			if err := q.SetIssueMilestone(ctx, dbgen.SetIssueMilestoneParams{IssueID: id, MilestoneID: *issue.Milestone}); err != nil {
				return fmt.Errorf("write issue milestone: %w", err)
			}
		}
		if !issue.Deleted {
			if err := q.IndexIssue(ctx, dbgen.IndexIssueParams{
				IssueID: id, ProjectID: projectID, Title: issue.Title, Description: issue.Description,
			}); err != nil {
				return fmt.Errorf("index issue: %w", err)
			}
		}
	}
	for _, relation := range rows.Relations {
		if _, err := q.RelateIssues(ctx, dbgen.RelateIssuesParams{
			FromIssue: relation.From, ToIssue: relation.To, Kind: relation.Kind, CreatedBy: people.who(relation.CreatedBy),
		}); err != nil {
			return fmt.Errorf("write relation: %w", err)
		}
	}

	for _, remove := range []func() error{
		func() error {
			return q.RemoveReplicaStatesExcept(ctx, dbgen.RemoveReplicaStatesExceptParams{ProjectID: projectID, Keep: keep.states})
		},
		func() error {
			return q.RemoveReplicaLabelsExcept(ctx, dbgen.RemoveReplicaLabelsExceptParams{ProjectID: projectID, Keep: keep.labels})
		},
		func() error {
			return q.RemoveReplicaPrioritiesExcept(ctx, dbgen.RemoveReplicaPrioritiesExceptParams{ProjectID: projectID, Keep: keep.priorities})
		},
		func() error {
			return q.RemoveReplicaCyclesExcept(ctx, dbgen.RemoveReplicaCyclesExceptParams{ProjectID: projectID, Keep: keep.cycles})
		},
		func() error {
			return q.RemoveReplicaMilestonesExcept(ctx, dbgen.RemoveReplicaMilestonesExceptParams{ProjectID: projectID, Keep: keep.milestones})
		},
	} {
		if err := remove(); err != nil {
			return fmt.Errorf("remove what the stream no longer has: %w", err)
		}
	}
	return nil
}

/*
DiscardReplica removes a replica and everything in it, at its home server's
word. Only a project whose home is that server: one server cannot have another's
replica — or a project of this server's own — removed.
*/
func (s *Service) DiscardReplica(ctx context.Context, projectID uuid.UUID, home string) (bool, error) {
	removed, err := s.queries.DiscardReplica(ctx, dbgen.DiscardReplicaParams{ID: projectID, HomeServer: strings.ToLower(home)})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("discard replica: %w", err)
	}
	return removed > 0, nil
}

/*
The home server's side: offering its projects to the servers entitled to them,
and telling the ones no longer entitled to discard their copies.
*/

/*
SyncReplicas brings which servers hold a replica of which project into line
with who is entitled: every server hosting a member of an opened project.

Set-based and cheap, so it runs on every delivery pass rather than being
remembered at each place membership or the switch changes. A newly entitled
server is to be offered the project; one no longer entitled is to discard its
copy, and is sent nothing more from this moment. A server that accepted is a
peer of the project's room and of every issue's discussion, including issues
filed since.
*/
func (s *Service) SyncReplicas(ctx context.Context) error {
	return s.inTransaction(ctx, func(q *dbgen.Queries) error {
		for _, step := range []func() error{
			func() error { return q.AddEntitledReplicas(ctx) },
			func() error { return q.DiscardUnentitledReplicas(ctx) },
			func() error { return q.StopDeliveringToDiscarding(ctx) },
			func() error { return q.FederateProjectRooms(ctx, s.rooms.ServerName()) },
			func() error { return q.DeliverToOfferedReplicas(ctx) },
		} {
			if err := step(); err != nil {
				return fmt.Errorf("sync replicas: %w", err)
			}
		}
		return nil
	})
}

// ReplicaWork is an offer to make or a discard to announce, claimed for one
// pass.
type ReplicaWork = dbgen.ProjectReplica

// ClaimReplicaWork claims offers and discards that are due.
func (s *Service) ClaimReplicaWork(ctx context.Context, limit int32) ([]ReplicaWork, error) {
	return s.queries.ClaimReplicaWork(ctx, limit)
}

// Offer is what a server is told when it is entitled to a project: which
// project, and which of its people are in it.
type Offer struct {
	Project    uuid.UUID `json:"project"`
	Name       string    `json:"name"`
	Visibility string    `json:"visibility"`
	Members    []string  `json:"members"`
}

// OfferFor is the offer a server is to be made.
func (s *Service) OfferFor(ctx context.Context, work ReplicaWork) (Offer, error) {
	project, err := s.queries.GetProjectByID(ctx, work.ProjectID)
	if err != nil {
		return Offer{}, fmt.Errorf("read project: %w", err)
	}
	members, err := s.queries.RemoteMembersOn(ctx, dbgen.RemoteMembersOnParams{ProjectID: work.ProjectID, HomeServer: work.Server})
	if err != nil {
		return Offer{}, fmt.Errorf("read members: %w", err)
	}
	return Offer{Project: project.ID, Name: project.Name, Visibility: project.Visibility, Members: members}, nil
}

// ReplicaOffered records that a server accepted a project, at the address it
// was reached at, so its events are delivered there from the next pass.
func (s *Service) ReplicaOffered(ctx context.Context, work ReplicaWork, address string) error {
	return s.queries.ReplicaOffered(ctx, dbgen.ReplicaOfferedParams{ProjectID: work.ProjectID, Server: work.Server, Address: address})
}

// ReplicaWorkFailed counts a failed offer or discard and puts the next try off.
func (s *Service) ReplicaWorkFailed(ctx context.Context, work ReplicaWork, reason string, retryAfter time.Duration) error {
	return s.queries.ReplicaWorkFailed(ctx, dbgen.ReplicaWorkFailedParams{
		ProjectID: work.ProjectID, Server: work.Server, LastError: reason,
		RetryAfter: pgtype.Interval{Microseconds: retryAfter.Microseconds(), Valid: true},
	})
}

// ForgetReplica drops a server that has confirmed it discarded its copy.
func (s *Service) ForgetReplica(ctx context.Context, work ReplicaWork) error {
	return s.queries.ForgetReplica(ctx, dbgen.ForgetReplicaParams{ProjectID: work.ProjectID, Server: work.Server})
}

// ErrNobodyHere is an offer naming nobody of this server.
var ErrNobodyHere = errors.New("the offer names nobody on this server")

/*
AcceptOffer starts a replica of a project another server is home to.

Only for somebody here: an offer naming nobody of this server is refused, so a
peer cannot make this server hold whatever it likes. The project's room is
adopted under the home's own identifier, with the home as its peer, and the
home delivers its stream from there. A room of this server's own, or another
server's, by that identifier is never touched.
*/
func (s *Service) AcceptOffer(ctx context.Context, offer Offer, home, homeAddress string) error {
	people := &replicaPeople{ctx: ctx, q: s.queries, server: s.rooms.ServerName(), known: map[string]*uuid.UUID{}}
	named := false
	for _, member := range offer.Members {
		if _, here := people.local(member); here {
			named = true
		}
	}
	if !named {
		return ErrNobodyHere
	}
	visibility := offer.Visibility
	if visibility != "public" {
		visibility = "private"
	}
	if err := s.queries.AdoptReplicaRoom(ctx, dbgen.AdoptReplicaRoomParams{
		ID: offer.Project, Name: offer.Name, Kind: "project", Visibility: visibility, HomeServer: strings.ToLower(home),
	}); err != nil {
		return fmt.Errorf("adopt project: %w", err)
	}
	room, err := s.queries.GetRoomByID(ctx, offer.Project)
	if err != nil {
		return fmt.Errorf("read project room: %w", err)
	}
	if room.Kind != "project" || !strings.EqualFold(room.HomeServer, home) {
		return ErrProjectNotFound
	}
	return s.rooms.AddPeer(ctx, room.ID, homeAddress)
}
