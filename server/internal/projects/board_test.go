package projects_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/projects"
)

/*
seedBoard files count issues spread evenly over a project's states, each as
heavy as a card gets: a long title, three labels, two assignees and a priority.
The budget is only meaningful against the worst card, not a typical one.
*/
func seedBoard(t *testing.T, f fixture, owner projects.Actor, access projects.Access, from, count int) {
	t.Helper()
	ctx := context.Background()
	states := f.states(t, access)
	labels, err := f.projects.Labels(ctx, access.Project.ID)
	if err != nil {
		t.Fatalf("labels: %v", err)
	}
	priorities, err := f.projects.Priorities(ctx, access.Project.ID)
	if err != nil {
		t.Fatalf("priorities: %v", err)
	}
	for i := from; i < from+count; i++ {
		state := states[i%len(states)].ID
		issue, err := f.projects.CreateIssue(ctx, owner, access, projects.NewIssue{
			Title:       fmt.Sprintf("Issue %04d: the export to CSV drops rows whose description contains a long line of text", i),
			Description: strings.Repeat("A description a card must never carry. ", 40),
			StateID:     &state,
		})
		if err != nil {
			t.Fatalf("file issue %d: %v", i, err)
		}
		for _, label := range labels {
			if _, err := f.projects.SetLabel(ctx, owner, access, issue, label.ID, true); err != nil {
				t.Fatalf("label: %v", err)
			}
		}
		for _, name := range []string{"amina", "bola"} {
			if _, _, err := f.projects.Assign(ctx, owner, access, issue, name); err != nil {
				t.Fatalf("assign: %v", err)
			}
		}
		priority := priorities[i%len(priorities)].ID
		if _, err := f.projects.SetPriority(ctx, owner, access, issue, &priority); err != nil {
			t.Fatalf("priority: %v", err)
		}
	}
}

// boardProject is a project with six states and three labels, and two
// members to assign.
func boardProject(t *testing.T, f fixture) (projects.Actor, projects.Access) {
	t.Helper()
	ctx := context.Background()
	owner := f.person(t, "owner")
	access := f.project(t, owner, "BRD")
	for _, name := range []string{"Triage", "Review", "Released"} {
		if _, err := f.projects.AddState(ctx, owner, access, projects.NewState{Name: name}); err != nil {
			t.Fatalf("add state: %v", err)
		}
	}
	for _, name := range []string{"needs-design", "frontend", "regression"} {
		if _, err := f.projects.AddLabel(ctx, access, name, "#2563eb"); err != nil {
			t.Fatalf("add label: %v", err)
		}
	}
	for _, name := range []string{"amina", "bola"} {
		f.person(t, name)
		if err := f.projects.SetMember(ctx, owner, access, name, "member"); err != nil {
			t.Fatalf("add member: %v", err)
		}
	}
	return owner, access
}

func boardBytes(t *testing.T, f fixture, access projects.Access) int {
	t.Helper()
	board, err := f.projects.Board(context.Background(), access, 0)
	if err != nil {
		t.Fatalf("board: %v", err)
	}
	encoded, err := json.Marshal(board)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return len(encoded)
}

/*
A board's first load stays within its budget, and does not grow with the
project.

Measured on the exact structure the route sends, for a project whose every
column is already full, and again after six times as many issues have been
filed into it: the second must be no heavier than the first, beyond the extra
digits in larger issue numbers. A board that shipped every issue, or that let
the description onto a card, fails here — and this runs in CI, so it fails the
build.
*/
func TestTheBoardStaysWithinItsBudgetAtAnySize(t *testing.T) {
	f := newFixture(t)
	owner, access := boardProject(t, f)

	seedBoard(t, f, owner, access, 0, 6*projects.DefaultBoardPage)
	small := boardBytes(t, f, access)

	/*
		Six hundred more, written straight into the tables: what is measured is
		whether the board grows with the project, and filing them one at a time
		through every event would make this test take a minute to say so. They
		have no priority, so they sort after the heavy cards and the first page
		of each column stays as heavy as it was.
	*/
	if _, err := f.pool.Exec(context.Background(), `
		WITH made AS (
			INSERT INTO rooms (id, name, kind, visibility, home_server)
			SELECT gen_random_uuid(), 'bulk', 'issue-discussion', 'private', 'tdc.test'
			FROM generate_series(1, 600)
			RETURNING id
		), numbered AS (
			SELECT id, row_number() OVER () + 1000 AS n FROM made
		)
		INSERT INTO issues (id, project_id, number, title, state_id)
		SELECT numbered.id, $1, numbered.n, 'Filed in bulk ' || numbered.n,
		       (SELECT id FROM issue_states WHERE project_id = $1 ORDER BY position OFFSET (numbered.n % 6) LIMIT 1)
		FROM numbered`, access.Project.ID); err != nil {
		t.Fatalf("file in bulk: %v", err)
	}
	large := boardBytes(t, f, access)

	t.Logf("first load: %d bytes with 120 issues, %d bytes with 720 (budget %d)",
		small, large, projects.BoardBudgetBytes)
	if large > projects.BoardBudgetBytes {
		t.Errorf("the board weighs %d bytes, over its budget of %d", large, projects.BoardBudgetBytes)
	}
	if large-small > 1024 {
		t.Errorf("six times the issues made the board %d bytes heavier; it should not grow with the project",
			large-small)
	}

	board, err := f.projects.Board(context.Background(), access, 0)
	if err != nil {
		t.Fatalf("board: %v", err)
	}
	for _, column := range board.Columns {
		if column.Total != 120 || len(column.Cards) != projects.DefaultBoardPage || column.Next == "" {
			t.Errorf("column %s: total %d, %d cards, next %q", column.Name, column.Total, len(column.Cards), column.Next)
		}
	}
}

/*
Following a column's cursor visits every issue in it exactly once, in order,
even while cards are being moved out of the column between pages.
*/
func TestPagingAColumnVisitsEveryCardOnce(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	owner := f.person(t, "owner")
	access := f.project(t, owner, "PAGE")
	states := f.states(t, access)
	priorities, err := f.projects.Priorities(ctx, access.Project.ID)
	if err != nil {
		t.Fatalf("priorities: %v", err)
	}

	var filed []dbgen.Issue
	for i := 0; i < 23; i++ {
		issue := f.issue(t, owner, access, fmt.Sprintf("Card %d", i))
		if i%3 == 0 {
			priority := priorities[i%len(priorities)].ID
			if issue, err = f.projects.SetPriority(ctx, owner, access, issue, &priority); err != nil {
				t.Fatalf("priority: %v", err)
			}
		}
		filed = append(filed, issue)
	}

	seen := map[string]int{}
	after := ""
	previous := ""
	for page := 0; ; page++ {
		column, err := f.projects.Column(ctx, access, states[0].ID, after, 5)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		for _, card := range column.Cards {
			seen[card.Key]++
		}
		if page == 1 {
			// Move a card the next page has not reached yet, and one already
			// seen: neither may make the walk skip or repeat a card.
			if _, err := f.projects.Transition(ctx, owner, access, filed[0], states[1].ID, nil); err != nil {
				t.Fatalf("move: %v", err)
			}
		}
		if column.Next == "" {
			break
		}
		if column.Next == previous {
			t.Fatal("the cursor did not advance")
		}
		previous, after = column.Next, column.Next
	}
	for _, issue := range filed[1:] {
		key := fmt.Sprintf("PAGE-%d", issue.Number)
		if seen[key] != 1 {
			t.Errorf("%s was seen %d times", key, seen[key])
		}
	}

	if _, err := f.projects.Column(ctx, access, states[0].ID, "not-a-cursor", 5); !errors.Is(err, projects.ErrBadCursor) {
		t.Errorf("a made-up cursor: %v", err)
	}
}

/*
Two people dragging the same card at once converge: exactly one move happens,
the other is told what the card became, and the history records one move.
*/
func TestSimultaneousMovesConverge(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	amina := f.person(t, "amina")
	access := f.project(t, amina, "RACE")
	bola := f.person(t, "bola")
	if err := f.projects.SetMember(ctx, amina, access, "bola", "member"); err != nil {
		t.Fatalf("add member: %v", err)
	}
	bolaAccess, err := f.projects.ProjectByID(ctx, bola, access.Project.ID)
	if err != nil {
		t.Fatalf("bola's access: %v", err)
	}
	states := f.states(t, access)

	for round := 0; round < 10; round++ {
		issue := f.issue(t, amina, access, fmt.Sprintf("Contested %d", round))
		expected := issue.StateID

		var wait sync.WaitGroup
		start := make(chan struct{})
		results := make([]error, 2)
		moves := []struct {
			actor  projects.Actor
			access projects.Access
			to     uuid.UUID
		}{
			{amina, access, states[1].ID},
			{bola, bolaAccess, states[2].ID},
		}
		for i, move := range moves {
			wait.Add(1)
			go func(i int, actor projects.Actor, access projects.Access, to uuid.UUID) {
				defer wait.Done()
				<-start
				_, results[i] = f.projects.Transition(ctx, actor, access, issue, to, &expected)
			}(i, move.actor, move.access, move.to)
		}
		close(start)
		wait.Wait()

		winners := 0
		var conflict projects.ConflictError
		for i, result := range results {
			switch {
			case result == nil:
				winners++
			case errors.As(result, &conflict):
				if conflict.Issue.StateID == expected {
					t.Errorf("round %d: the loser of the race was told the card had not moved", round)
				}
			default:
				t.Fatalf("round %d, move %d: %v", round, i, result)
			}
		}
		if winners != 1 {
			t.Fatalf("round %d: %d moves succeeded, want exactly one", round, winners)
		}

		final, err := f.queries.GetIssueByID(ctx, issue.ID)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if conflict.Issue.StateID != final.StateID {
			t.Errorf("round %d: the loser was told %v, but the card is in %v", round, conflict.Issue.StateID, final.StateID)
		}
		history, err := f.projects.History(ctx, access, issue)
		if err != nil {
			t.Fatalf("history: %v", err)
		}
		if len(history) != 2 {
			t.Errorf("round %d: history has %d entries, want the filing and one move", round, len(history))
		}
	}
}
