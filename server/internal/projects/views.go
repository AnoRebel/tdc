package projects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

// Errors a caller is expected to handle.
var (
	ErrViewNotFound         = errors.New("no such view")
	ErrViewName             = errors.New("a view needs a name of at most sixty characters")
	ErrViewLayout           = errors.New("a view is a list or a board, and a board belongs to one project")
	ErrViewFilter           = errors.New("that is not something a view can filter by")
	ErrNotViewOwner         = errors.New("only whoever saved a view may change it")
	ErrShareNeedsMembership = errors.New("only a project's members may share a view of it")
)

/*
ViewFilter is how a view asks: the same names a search takes, so a view is a
search that has been given a name. Nothing in it identifies anything, which is
what lets it mean the same thing to everybody who opens it — and show each of
them only what they may see.
*/
type ViewFilter struct {
	Q        string `json:"q,omitempty"`
	State    string `json:"state,omitempty"`
	Label    string `json:"label,omitempty"`
	Priority string `json:"priority,omitempty"`
	Assignee string `json:"assignee,omitempty"`
	/*
		Cycle is "current" for whichever cycle is under way when the view is
		opened, "none" for issues in no cycle, or one cycle's identifier.

		An identifier rather than a name, unlike the rest: a project may have
		two cycles of the same name, and a board view belongs to one project, so
		the identifier is as lasting as the view. "current" is what a team in
		the middle of its work usually means — the board as it stands this
		cycle, not the cycle that was under way when somebody saved it.
	*/
	Cycle string `json:"cycle,omitempty"`
	// Sort is "newest" or "priority".
	Sort string `json:"sort,omitempty"`
}

func (f ViewFilter) valid() bool {
	for _, value := range []string{f.Q, f.State, f.Label, f.Priority, f.Assignee} {
		if utf8.RuneCountInString(value) > 200 {
			return false
		}
	}
	if !validCycleFilter(f.Cycle) {
		return false
	}
	return f.Sort == "" || f.Sort == "newest" || f.Sort == "priority"
}

// validCycleFilter is whether a cycle filter says something a board can ask.
func validCycleFilter(cycle string) bool {
	switch cycle {
	case "", CycleNone, CycleCurrent:
		return true
	}
	_, err := uuid.Parse(cycle)
	return err == nil
}

// NewView describes a view to save.
type NewView struct {
	Name   string
	Layout string
	Filter ViewFilter
	Shared bool
}

// View is a saved view and who saved it.
type View struct {
	dbgen.ProjectView
	Owner  string
	Filter ViewFilter
}

func viewOf(row dbgen.ProjectView, owner string) (View, error) {
	var filter ViewFilter
	if err := json.Unmarshal(row.Filter, &filter); err != nil {
		return View{}, fmt.Errorf("decode view filter: %w", err)
	}
	return View{ProjectView: row, Owner: owner, Filter: filter}, nil
}

func checkView(input NewView, project *Access) (string, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || utf8.RuneCountInString(name) > 60 {
		return "", ErrViewName
	}
	if input.Layout != "list" && input.Layout != "board" {
		return "", ErrViewLayout
	}
	if input.Layout == "board" && project == nil {
		return "", ErrViewLayout
	}
	if !input.Filter.valid() {
		return "", ErrViewFilter
	}
	if input.Shared && project != nil && project.Role == "" {
		return "", ErrShareNeedsMembership
	}
	return name, nil
}

/*
SaveView keeps a way of looking at issues: one project's, given its access, or
every project's when project is nil.

Anybody who can see a project may keep a view of it for themselves. Sharing one
with everybody who can see the project is for its members, so a project's list
of shared views is the team's and not anybody passing by.
*/
func (s *Service) SaveView(ctx context.Context, actor Actor, project *Access, input NewView) (View, error) {
	name, err := checkView(input, project)
	if err != nil {
		return View{}, err
	}
	filter, err := json.Marshal(input.Filter)
	if err != nil {
		return View{}, fmt.Errorf("encode view filter: %w", err)
	}
	var projectID *uuid.UUID
	if project != nil {
		projectID = &project.Project.ID
	}
	row, err := s.queries.CreateProjectView(ctx, dbgen.CreateProjectViewParams{
		ID: uuid.New(), ProjectID: projectID, OwnerID: actor.ID, Name: name,
		Layout: input.Layout, Filter: filter, Shared: input.Shared,
	})
	if err != nil {
		return View{}, fmt.Errorf("save view: %w", err)
	}
	owner, err := s.queries.GetUserByID(ctx, actor.ID)
	if err != nil {
		return View{}, fmt.Errorf("read owner: %w", err)
	}
	return viewOf(row, owner.Username)
}

/*
OpenView returns a view somebody may use, and the access its project gives them.

Their own, or a shared one in a project they can see. Anything else — another
person's unshared view, or one in a private project they are not in — answers
as a view that does not exist.
*/
func (s *Service) OpenView(ctx context.Context, actor Actor, id uuid.UUID) (View, *Access, error) {
	row, err := s.queries.GetProjectView(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return View{}, nil, ErrViewNotFound
	}
	if err != nil {
		return View{}, nil, fmt.Errorf("read view: %w", err)
	}
	if row.OwnerID != actor.ID && !row.Shared {
		return View{}, nil, ErrViewNotFound
	}
	var access *Access
	if row.ProjectID != nil {
		found, err := s.ProjectByID(ctx, actor, *row.ProjectID)
		if errors.Is(err, ErrProjectNotFound) {
			return View{}, nil, ErrViewNotFound
		}
		if err != nil {
			return View{}, nil, err
		}
		access = &found
	}
	owner, err := s.queries.GetUserByID(ctx, row.OwnerID)
	if err != nil {
		return View{}, nil, fmt.Errorf("read owner: %w", err)
	}
	view, err := viewOf(row, owner.Username)
	return view, access, err
}

// UpdateView changes a view, for whoever saved it.
func (s *Service) UpdateView(ctx context.Context, actor Actor, id uuid.UUID, input NewView) (View, error) {
	current, access, err := s.OpenView(ctx, actor, id)
	if err != nil {
		return View{}, err
	}
	if current.OwnerID != actor.ID {
		return View{}, ErrNotViewOwner
	}
	name, err := checkView(input, access)
	if err != nil {
		return View{}, err
	}
	filter, err := json.Marshal(input.Filter)
	if err != nil {
		return View{}, fmt.Errorf("encode view filter: %w", err)
	}
	row, err := s.queries.UpdateProjectView(ctx, dbgen.UpdateProjectViewParams{
		ID: id, Name: name, Layout: input.Layout, Filter: filter, Shared: input.Shared,
	})
	if err != nil {
		return View{}, fmt.Errorf("update view: %w", err)
	}
	return viewOf(row, current.Owner)
}

// DeleteView removes a view, for whoever saved it.
func (s *Service) DeleteView(ctx context.Context, actor Actor, id uuid.UUID) error {
	current, _, err := s.OpenView(ctx, actor, id)
	if err != nil {
		return err
	}
	if current.OwnerID != actor.ID {
		return ErrNotViewOwner
	}
	return s.queries.DeleteProjectView(ctx, id)
}

// Views lists the views somebody may use: one project's, given its access, or
// those across projects when project is nil.
func (s *Service) Views(ctx context.Context, actor Actor, project *Access) ([]View, error) {
	views := []View{}
	if project != nil {
		rows, err := s.queries.ListViewsOfProject(ctx, dbgen.ListViewsOfProjectParams{
			ProjectID: &project.Project.ID, UserID: actor.ID,
		})
		if err != nil {
			return nil, fmt.Errorf("list views: %w", err)
		}
		for _, row := range rows {
			view, err := viewOf(dbgen.ProjectView{
				ID: row.ID, ProjectID: row.ProjectID, OwnerID: row.OwnerID, Name: row.Name,
				Layout: row.Layout, Filter: row.Filter, Shared: row.Shared,
				CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			}, row.OwnerUsername)
			if err != nil {
				return nil, err
			}
			views = append(views, view)
		}
		return views, nil
	}
	rows, err := s.queries.ListViewsAcrossProjects(ctx, actor.ID)
	if err != nil {
		return nil, fmt.Errorf("list views: %w", err)
	}
	for _, row := range rows {
		view, err := viewOf(dbgen.ProjectView{
			ID: row.ID, ProjectID: row.ProjectID, OwnerID: row.OwnerID, Name: row.Name,
			Layout: row.Layout, Filter: row.Filter, Shared: row.Shared,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}, row.OwnerUsername)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}
