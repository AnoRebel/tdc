package projects

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"uuid"

	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
Following a public project from another server, and asking to join one.

A public project opened to federation may be held by any server this one
trusts. That server follows it — asks for a replica nobody of its own is a
member of — and its people read the project there. To work in it they ask to
join, through their own server; the project's administrators here decide.
*/

// ErrJoinRequestNotFound is a decision about a request nobody made.
var ErrJoinRequestNotFound = errors.New("nobody by that name asked to join")

// maxPublishedProjects bounds what one peer browsing this server is shown.
const maxPublishedProjects = 100

// PublishedProject is a project a trusted peer may follow.
type PublishedProject struct {
	ID          uuid.UUID `json:"id"`
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
}

// Published lists the projects this server lets trusted peers follow.
func (s *Service) Published(ctx context.Context) ([]PublishedProject, error) {
	rows, err := s.queries.PublishedProjects(ctx, maxPublishedProjects)
	if err != nil {
		return nil, fmt.Errorf("list published projects: %w", err)
	}
	published := make([]PublishedProject, 0, len(rows))
	for _, row := range rows {
		published = append(published, PublishedProject{ID: row.ID, Key: row.Key, Name: row.Name, Description: row.Description})
	}
	return published, nil
}

// Follow records that a server follows a project of this server's, to be
// offered a replica of it. Only a public project opened to federation may be.
func (s *Service) Follow(ctx context.Context, projectID uuid.UUID, server, address string) error {
	followed, err := s.queries.FollowProject(ctx, dbgen.FollowProjectParams{
		Project: projectID, Server: strings.ToLower(server), Address: address,
	})
	if err != nil {
		return fmt.Errorf("follow project: %w", err)
	}
	if followed == 0 {
		return ErrProjectNotFound
	}
	return nil
}

// Unfollow records that a server no longer follows a project; its replica is
// discarded unless people of that server are members.
func (s *Service) Unfollow(ctx context.Context, projectID uuid.UUID, server string) error {
	return s.queries.UnfollowProject(ctx, dbgen.UnfollowProjectParams{ProjectID: projectID, Server: strings.ToLower(server)})
}

/*
Following is this server's side: a project another server agreed it may
follow, its room adopted under the home's identifier so the offer that follows
is accepted with nobody of this server named in it.
*/
func (s *Service) Following(ctx context.Context, project uuid.UUID, name, home string) error {
	if err := s.queries.AdoptReplicaRoom(ctx, dbgen.AdoptReplicaRoomParams{
		ID: project, Name: name, Kind: "project", Visibility: "public", HomeServer: strings.ToLower(home),
	}); err != nil {
		return fmt.Errorf("adopt project: %w", err)
	}
	return nil
}

// AskToJoin records that somebody on another server asked to be a member, and
// says whether they had not asked already.
func (s *Service) AskToJoin(ctx context.Context, projectID uuid.UUID, who string) (bool, error) {
	address, server, remote, err := s.remoteAddress(who)
	if err != nil || !remote {
		return false, ErrRemoteAddress
	}
	asked, err := s.queries.AskToJoinProject(ctx, dbgen.AskToJoinProjectParams{ProjectID: projectID, Address: address, HomeServer: server})
	return asked > 0, err
}

// JoinRequests lists who asked to join a project, for its administrators.
func (s *Service) JoinRequests(ctx context.Context, access Access) ([]dbgen.ProjectJoinRequest, error) {
	if decision := access.Allows(authz.PermProjectAdminister); !decision.Allowed {
		return nil, RefusalError{decision}
	}
	return s.queries.ProjectJoinRequests(ctx, access.Project.ID)
}

// DecideJoinRequest lets somebody who asked in, as a member, or declines them;
// either way the request is answered and forgotten.
func (s *Service) DecideJoinRequest(ctx context.Context, actor Actor, access Access, who string, accept bool) error {
	if decision := access.Allows(authz.PermProjectAdminister); !decision.Allowed {
		return RefusalError{decision}
	}
	address, _, remote, err := s.remoteAddress(who)
	if err != nil || !remote {
		return ErrJoinRequestNotFound
	}
	requests, err := s.queries.ProjectJoinRequests(ctx, access.Project.ID)
	if err != nil {
		return fmt.Errorf("read join requests: %w", err)
	}
	if !slices.ContainsFunc(requests, func(request dbgen.ProjectJoinRequest) bool { return request.Address == address }) {
		return ErrJoinRequestNotFound
	}
	if accept {
		if err := s.SetMember(ctx, actor, access, address, "member"); err != nil {
			return err
		}
	}
	_, err = s.queries.ForgetJoinRequest(ctx, dbgen.ForgetJoinRequestParams{ProjectID: access.Project.ID, Address: address})
	return err
}
