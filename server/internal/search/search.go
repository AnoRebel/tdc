// Package search finds messages a person is allowed to read.
//
// Postgres full-text search rather than a separate index. Zulip and Synapse
// both run plain Postgres at millions of messages, and embedding a search
// engine means a second write path, a second backup, and index/database
// divergence — all inside the process serving conversations. Mattermost ran
// embedded Bleve for years and removed it in Server v11.
package search

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

// maxResults bounds one search.
//
// Enough to be useful, small enough that a query matching most of a busy
// channel does not become a way to read it all at once.
const maxResults = 50

// Result is one message that matched.
type Result struct {
	EventID string
	RoomID  uuid.UUID
	Sender  string
	Body    string
	// Highlighted is the body with the matched terms marked, so a client can
	// show why this result matched without guessing.
	Highlighted string
	SentAt      int64
}

// Searcher finds messages.
//
// An interface because the design commits to Postgres now while leaving room
// for an operator with an enormous archive to point this elsewhere. What it
// does not do is embed a second engine in this process.
type Searcher interface {
	// Search finds messages matching a query, restricted to what the person
	// searching may read.
	Search(ctx context.Context, userID uuid.UUID, query Query) ([]Result, error)
}

// Query is what to look for.
type Query struct {
	Text string
	// Room narrows the search to one conversation. Nil searches everything the
	// person can read.
	Room *uuid.UUID
	// Prefix asks for search-as-you-type behaviour, which needs trigrams:
	// websearch_to_tsquery cannot match a prefix, so "mkut" finds nothing
	// there however many messages contain "mkutano".
	Prefix bool
	Limit  int
}

// Service searches messages in Postgres.
type Service struct {
	queries *dbgen.Queries
}

func NewService(pool *db.Pool) *Service {
	return &Service{queries: dbgen.New(pool)}
}

/*
Search finds messages the person is allowed to read.

Authorisation is a join in the query rather than a filter afterwards. Fetching
a result and then discarding it has still read it from disk, and a mistake in
the discarding is a leak — where a mistake in a join is an empty result. The
safer failure is the one that shows nothing.
*/
func (s *Service) Search(ctx context.Context, userID uuid.UUID, query Query) ([]Result, error) {
	text := strings.TrimSpace(query.Text)
	if text == "" {
		// An empty search is not an error; it simply matches nothing. Running
		// it would return the whole archive.
		return nil, nil
	}

	limit := query.Limit
	if limit <= 0 || limit > maxResults {
		limit = maxResults
	}

	// A plain UUID cannot express "no room", so the two are passed separately
	// rather than relying on a zero value to mean something.
	var room uuid.UUID
	if query.Room != nil {
		room = *query.Room
	}
	allRooms := query.Room == nil

	if query.Prefix {
		return s.searchFuzzy(ctx, userID, text, room, allRooms, int32(limit))
	}
	return s.searchFullText(ctx, userID, text, room, allRooms, int32(limit))
}

func (s *Service) searchFullText(
	ctx context.Context,
	userID uuid.UUID,
	text string,
	room uuid.UUID,
	allRooms bool,
	limit int32,
) ([]Result, error) {
	rows, err := s.queries.SearchMessages(ctx, dbgen.SearchMessagesParams{
		Query:       text,
		UserID:      userID,
		RoomID:      room,
		AllRooms:    allRooms,
		ResultLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	results := make([]Result, 0, len(rows))
	for _, row := range rows {
		results = append(results, Result{
			EventID:     row.EventID,
			RoomID:      row.RoomID,
			Sender:      row.SenderAddress,
			Body:        row.Body,
			Highlighted: string(row.Highlighted),
			SentAt:      row.SentAt,
		})
	}

	return results, nil
}

func (s *Service) searchFuzzy(
	ctx context.Context,
	userID uuid.UUID,
	text string,
	room uuid.UUID,
	allRooms bool,
	limit int32,
) ([]Result, error) {
	rows, err := s.queries.SearchMessagesFuzzy(ctx, dbgen.SearchMessagesFuzzyParams{
		Query:       text,
		UserID:      userID,
		RoomID:      room,
		AllRooms:    allRooms,
		ResultLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	results := make([]Result, 0, len(rows))
	for _, row := range rows {
		results = append(results, Result{
			EventID: row.EventID,
			RoomID:  row.RoomID,
			Sender:  row.SenderAddress,
			Body:    row.Body,
			// No highlighting on the fuzzy path: ts_headline marks the terms a
			// tsquery matched, and a trigram match has no such terms. Marking
			// something arbitrary would be worse than marking nothing.
			Highlighted: row.Body,
			SentAt:      row.SentAt,
		})
	}

	return results, nil
}

/*
Index records a message so it can be found.

Called after the message is stored, and deliberately not inside the same
transaction: a search index that fails must not fail the message. Somebody
whose message did not send because an index was unavailable has lost something
real; somebody whose message is briefly unsearchable has not.
*/
func (s *Service) Index(ctx context.Context, event dbgen.Event, body string) error {
	if strings.TrimSpace(body) == "" {
		return nil
	}

	err := s.queries.IndexMessage(ctx, dbgen.IndexMessageParams{
		EventID:       event.ID,
		RoomID:        event.RoomID,
		SenderID:      event.SenderID,
		SenderAddress: event.SenderAddress,
		Body:          body,
		SentAt:        event.CreatedAt,
	})
	if err != nil {
		return fmt.Errorf("search: indexing %s: %w", event.ID, err)
	}
	return nil
}

// Unindex removes a message from the index.
//
// Called when a message is deleted or expires. A deleted message that remained
// searchable would be a deletion that did not delete: the placeholder says the
// content is gone, and search must agree.
func (s *Service) Unindex(ctx context.Context, eventID string) error {
	if err := s.queries.UnindexMessage(ctx, eventID); err != nil {
		return fmt.Errorf("search: removing %s: %w", eventID, err)
	}
	return nil
}

// ErrUnavailable is returned when search cannot run at all.
var ErrUnavailable = errors.New("search: unavailable")
