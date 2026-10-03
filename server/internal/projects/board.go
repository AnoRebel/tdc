package projects

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
A board is not a stored thing. It is the project's states, in order, each with
a page of its issues — so a state added or reordered is a column added or
reordered, with nothing else to keep in step.

What a board sends is held to a budget, because the people using it are on
phones and metered data. Every column sends one page of cards whatever the
project holds — a project of eight hundred issues sends the same few dozen
cards as one of eighty — and a card carries only what a card shows. The rest
of a column is fetched a page at a time as somebody scrolls to it.

The shapes below are the wire format, tagged here rather than in the HTTP
layer so the budget test measures exactly the bytes the route sends.
*/

const (
	// DefaultBoardPage is how many cards each column sends at first.
	DefaultBoardPage = 20
	// MaxBoardPage bounds what one request for a column may ask for.
	MaxBoardPage = 50

	// BoardBudgetBytes is what a board's first load may weigh, as JSON before
	// compression, for a project of any size: six columns of a full first
	// page of long-titled, labelled, assigned cards come in well under it.
	// docs/projects.md says why it is this number.
	BoardBudgetBytes = 48 * 1024

	unranked = 2147483647
)

// ErrBadCursor refuses a page cursor this server did not issue.
var ErrBadCursor = errors.New("that is not a place in this column")

// Board is a project's issues as columns.
type Board struct {
	Columns []BoardColumn `json:"columns"`
}

// BoardColumn is one state and a page of the issues in it.
type BoardColumn struct {
	StateID      string `json:"stateId"`
	Name         string `json:"name"`
	IsCompletion bool   `json:"isCompletion"`
	// Total is how many issues the state holds, all of them, so the column
	// can say so without sending them.
	Total int64      `json:"total"`
	Cards []CardJSON `json:"cards"`
	// Next continues the column, absent when this page was the last.
	Next string `json:"next,omitempty"`
}

// CardJSON is an issue as a card shows it, and nothing more.
type CardJSON struct {
	Key      string        `json:"key"`
	Title    string        `json:"title"`
	Priority *CardPriority `json:"priority,omitempty"`
	Labels   []CardLabel   `json:"labels,omitempty"`
	// Assignees are usernames: a card draws initials from them, and anything
	// more about a person is a request away.
	Assignees []string `json:"assignees,omitempty"`
}

// CardPriority is a card's priority, by name and place on the scale.
type CardPriority struct {
	Name string `json:"name"`
	Rank int32  `json:"rank"`
}

// CardLabel is a label as a card shows it.
type CardLabel struct {
	Name   string `json:"name"`
	Colour string `json:"colour"`
}

func pageSize(limit int) int32 {
	if limit <= 0 {
		return DefaultBoardPage
	}
	if limit > MaxBoardPage {
		return MaxBoardPage
	}
	return int32(limit)
}

/*
BoardFilter narrows a board, by names as a saved view keeps them: a label, a
priority level, somebody assigned. Every one given must hold, and the column
counts are of what the filtered board shows.
*/
type BoardFilter struct {
	Label    string
	Priority string
	Assignee *uuid.UUID
	// Cycle is a cycle's identifier, or "none" for issues in no cycle.
	Cycle string
}

// Board returns a project's board: every state, each with its first page.
func (s *Service) Board(ctx context.Context, access Access, filter BoardFilter, limit int) (Board, error) {
	states, err := s.queries.ListIssueStates(ctx, access.Project.ID)
	if err != nil {
		return Board{}, fmt.Errorf("list states: %w", err)
	}
	counts, err := s.queries.CountBoardColumns(ctx, dbgen.CountBoardColumnsParams{
		ProjectID: access.Project.ID, LabelName: filter.Label,
		PriorityName: filter.Priority, AssigneeID: filter.Assignee, CycleFilter: filter.Cycle,
	})
	if err != nil {
		return Board{}, fmt.Errorf("count columns: %w", err)
	}
	totals := map[uuid.UUID]int64{}
	for _, count := range counts {
		totals[count.StateID] = count.Total
	}

	board := Board{Columns: make([]BoardColumn, 0, len(states))}
	for _, state := range states {
		column, err := s.column(ctx, access, state, filter, "", limit)
		if err != nil {
			return Board{}, err
		}
		column.Total = totals[state.ID]
		board.Columns = append(board.Columns, column)
	}
	return board, nil
}

// Column returns one more page of one column, after the cursor a previous
// page gave.
func (s *Service) Column(ctx context.Context, access Access, stateID uuid.UUID, filter BoardFilter, after string, limit int) (BoardColumn, error) {
	state, err := stateOf(ctx, s.queries, access.Project.ID, stateID)
	if err != nil {
		return BoardColumn{}, err
	}
	return s.column(ctx, access, state, filter, after, limit)
}

func (s *Service) column(ctx context.Context, access Access, state dbgen.IssueState, filter BoardFilter, after string, limit int) (BoardColumn, error) {
	rank, number, err := parseCursor(after)
	if err != nil {
		return BoardColumn{}, err
	}
	size := pageSize(limit)
	rows, err := s.queries.ListBoardColumn(ctx, dbgen.ListBoardColumnParams{
		ProjectID: access.Project.ID, StateID: state.ID,
		LabelName: filter.Label, PriorityName: filter.Priority, AssigneeID: filter.Assignee,
		CycleFilter: filter.Cycle,
		AfterRank:   rank, AfterNumber: number, PageSize: size,
	})
	if err != nil {
		return BoardColumn{}, fmt.Errorf("read column: %w", err)
	}

	column := BoardColumn{
		StateID: state.ID.String(), Name: state.Name, IsCompletion: state.IsCompletion,
		Cards: make([]CardJSON, 0, len(rows)),
	}
	if len(rows) == 0 {
		return column, nil
	}

	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	labels, err := s.queries.ListLabelsForIssues(ctx, ids)
	if err != nil {
		return BoardColumn{}, fmt.Errorf("read labels: %w", err)
	}
	assignees, err := s.queries.ListAssigneesForIssues(ctx, ids)
	if err != nil {
		return BoardColumn{}, fmt.Errorf("read assignees: %w", err)
	}
	priorities, err := s.queries.ListProjectPriorities(ctx, access.Project.ID)
	if err != nil {
		return BoardColumn{}, fmt.Errorf("read priorities: %w", err)
	}
	priorityByID := map[uuid.UUID]dbgen.ProjectPriority{}
	for _, priority := range priorities {
		priorityByID[priority.ID] = priority
	}
	labelsOf := map[uuid.UUID][]CardLabel{}
	for _, label := range labels {
		labelsOf[label.IssueID] = append(labelsOf[label.IssueID], CardLabel{Name: label.Name, Colour: label.Colour})
	}
	assigneesOf := map[uuid.UUID][]string{}
	for _, assignee := range assignees {
		assigneesOf[assignee.IssueID] = append(assigneesOf[assignee.IssueID], assignee.Username)
	}

	for _, row := range rows {
		card := CardJSON{
			Key:       identifier(access.Project.Key, row.Number),
			Title:     row.Title,
			Labels:    labelsOf[row.ID],
			Assignees: assigneesOf[row.ID],
		}
		if row.PriorityID != nil {
			if priority, ok := priorityByID[*row.PriorityID]; ok {
				card.Priority = &CardPriority{Name: priority.Name, Rank: priority.Rank}
			}
		}
		column.Cards = append(column.Cards, card)
	}
	if int32(len(rows)) == size {
		last := rows[len(rows)-1]
		column.Next = fmt.Sprintf("%d.%d", last.Rank, last.Number)
	}
	return column, nil
}

// parseCursor reads "rank.number"; empty is the start of the column.
func parseCursor(cursor string) (int32, int64, error) {
	if cursor == "" {
		return -1, 0, nil
	}
	rank, number, found := strings.Cut(cursor, ".")
	if !found {
		return 0, 0, ErrBadCursor
	}
	r, err := strconv.ParseInt(rank, 10, 32)
	if err != nil || r < 0 || r > unranked {
		return 0, 0, ErrBadCursor
	}
	n, err := strconv.ParseInt(number, 10, 64)
	if err != nil || n <= 0 {
		return 0, 0, ErrBadCursor
	}
	return int32(r), n, nil
}
