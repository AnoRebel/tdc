package projects

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/federation"
)

/*
Sharing a project with other servers: the switch that allows it, and the
people from elsewhere it holds.

This server is the project's home, and the only place its membership is
decided. Nothing here sends anything anywhere: a remote member is an address
and a role, and what reaches their server, and when, is decided elsewhere.
*/

var (
	ErrNotOpened     = errors.New("this project is not shared with other servers; open it to federation first")
	ErrRemoteAddress = errors.New("somebody on another server is written name@server")
)

/*
SetFederated opens a project to federation, or closes it.

Closing keeps its members from elsewhere — they stay listed, and come back
into effect if it is opened again — but nothing about the project is shared
with anybody while it is closed.
*/
func (s *Service) SetFederated(ctx context.Context, actor Actor, access Access, federated bool) error {
	if decision := access.Allows(authz.PermProjectAdminister); !decision.Allowed {
		return RefusalError{decision}
	}
	return s.inTransaction(ctx, func(q *dbgen.Queries) error {
		changed, err := q.SetProjectFederated(ctx, dbgen.SetProjectFederatedParams{ID: access.Project.ID, Federated: federated})
		if err != nil {
			return fmt.Errorf("set federated: %w", err)
		}
		if changed == 0 {
			return nil
		}
		return s.appendProjectEvent(ctx, q, access.Project.ID, actor, events.TypeProjectFederationChanged,
			map[string]any{"federated": federated})
	})
}

// RemoteMember is somebody in a project who is on another server.
type RemoteMember = dbgen.ListProjectRemoteMembersRow

// RemoteMembers lists the people in a project who are on other servers.
func (s *Service) RemoteMembers(ctx context.Context, access Access) ([]RemoteMember, error) {
	members, err := s.queries.ListProjectRemoteMembers(ctx, access.Project.ID)
	if err != nil {
		return nil, fmt.Errorf("list remote members: %w", err)
	}
	return members, nil
}

/*
remoteAddress says whether somebody named in a request is on another server,
and if so their address and server.

A name without a server, or with this server's, is somebody here: "amina" and
"amina@thisserver" are the same person, and treating the second as remote would
give one person two memberships.
*/
func (s *Service) remoteAddress(who string) (address, server string, remote bool, err error) {
	local, host, found := strings.Cut(strings.ToLower(strings.TrimSpace(who)), "@")
	if !found || strings.EqualFold(host, s.rooms.ServerName()) {
		return "", "", false, nil
	}
	server, err = federation.ServerOf(local + "@" + host)
	if err != nil || local == "" || strings.ContainsAny(local+host, " \t/@") {
		return "", "", false, ErrRemoteAddress
	}
	return local + "@" + server, server, true, nil
}

// localName is the username in somebody named here, with or without this
// server's name after it.
func (s *Service) localName(who string) string {
	name, _, _ := strings.Cut(strings.TrimSpace(who), "@")
	return name
}

/*
setRemoteMember adds somebody from another server, or changes their role.

Only to a project opened to federation: a member elsewhere of a project that
is never shared would be a membership nobody could use.
*/
func (s *Service) setRemoteMember(ctx context.Context, actor Actor, access Access, address, server, role string) error {
	if !access.Project.Federated {
		return ErrNotOpened
	}
	return s.inTransaction(ctx, func(q *dbgen.Queries) error {
		previous, err := q.GetProjectRemoteMemberRole(ctx, dbgen.GetProjectRemoteMemberRoleParams{
			ProjectID: access.Project.ID, Address: address,
		})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("read membership: %w", err)
		}
		if previous == role {
			return nil
		}
		if err := q.AddProjectRemoteMember(ctx, dbgen.AddProjectRemoteMemberParams{
			ProjectID: access.Project.ID, Address: address, HomeServer: server, Role: role, AddedBy: &actor.ID,
		}); err != nil {
			return fmt.Errorf("add remote member: %w", err)
		}
		return s.appendProjectEvent(ctx, q, access.Project.ID, actor, events.TypeProjectMemberAdded,
			map[string]any{"member": address, "role": role, "previous": previous})
	})
}

/*
removeRemoteMember takes somebody from another server out of a project.

Always by an administrator here: the person cannot act on this server
themselves until changes from other servers arrive.
*/
func (s *Service) removeRemoteMember(ctx context.Context, actor Actor, access Access, address string) error {
	if decision := access.Allows(authz.PermProjectAdminister); !decision.Allowed {
		return RefusalError{decision}
	}
	return s.inTransaction(ctx, func(q *dbgen.Queries) error {
		previous, err := q.GetProjectRemoteMemberRole(ctx, dbgen.GetProjectRemoteMemberRoleParams{
			ProjectID: access.Project.ID, Address: address,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotAMember
		}
		if err != nil {
			return fmt.Errorf("read membership: %w", err)
		}
		if err := q.RemoveProjectRemoteMember(ctx, dbgen.RemoveProjectRemoteMemberParams{
			ProjectID: access.Project.ID, Address: address,
		}); err != nil {
			return fmt.Errorf("remove remote member: %w", err)
		}
		return s.appendProjectEvent(ctx, q, access.Project.ID, actor, events.TypeProjectMemberRemoved,
			map[string]any{"member": address, "previous": previous})
	})
}

/*
RemoteAccess is what somebody on another server may do in a project, for a
request their server makes on their behalf.

Their project role alone decides it. Nothing their own server grants them
reaches here, and nothing this server grants its own people applies to them:
the access carries no permissions of its own, so Allows answers from the role.

A project not opened to federation, and one the person is not in, are both
reported as not existing — a federated request learns nothing about a project
it has no part in, including that it is there.
*/
func (s *Service) RemoteAccess(ctx context.Context, projectID uuid.UUID, address string) (Access, error) {
	project, err := s.queries.GetProjectByID(ctx, projectID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !project.Federated) {
		return Access{}, ErrProjectNotFound
	}
	if err != nil {
		return Access{}, fmt.Errorf("read project: %w", err)
	}
	role, err := s.queries.GetProjectRemoteMemberRole(ctx, dbgen.GetProjectRemoteMemberRoleParams{
		ProjectID: projectID, Address: strings.ToLower(address),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Access{}, ErrProjectNotFound
	}
	if err != nil {
		return Access{}, fmt.Errorf("read membership: %w", err)
	}
	return Access{Project: project, Role: role}, nil
}
