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
	"uuid"

	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/markup"
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
	/*
		The index holds the words without their formatting. A body is the
		Markdown somebody typed, and what search shows back — the excerpt with
		its matches marked — is read as text: "**deploy**" there would be two
		pairs of asterisks around the word somebody searched for. The message
		itself is unchanged; only what can be found and quoted is plain.
	*/
	body = markup.PlainText(body)
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

// RoomResult is a conversation that matched.
type RoomResult struct {
	ID         uuid.UUID
	Slug       string
	Name       string
	Topic      string
	Kind       string
	Visibility string
	Archived   bool
	// Member says whether the person is already in it, so a client can offer
	// "open" rather than "join".
	Member bool
}

// PersonResult is somebody who matched.
//
// Only what a member list already discloses. Search must not become the way
// around a privacy setting, so nothing withheld appears here.
type PersonResult struct {
	ID          uuid.UUID
	Username    string
	DisplayName string
}

/*
Rooms finds conversations by name or topic.

No index backs this, deliberately. A server has hundreds of channels where it
has millions of messages, so Postgres reads the small table and is finished
before an index lookup would have been planned — and an index would cost
storage and a write on every rename to save nothing measurable.

Visibility is the authorisation: a private channel is returned only to somebody
already in it, so searching cannot be used to discover that one exists.
*/
func (s *Service) Rooms(ctx context.Context, userID uuid.UUID, query string, limit int) ([]RoomResult, error) {
	text := strings.TrimSpace(query)
	if text == "" {
		return nil, nil
	}
	if limit <= 0 || limit > maxResults {
		limit = maxResults
	}

	rows, err := s.queries.SearchRooms(ctx, dbgen.SearchRoomsParams{
		UserID:      userID,
		Query:       text,
		ResultLimit: int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("search: rooms: %w", err)
	}

	results := make([]RoomResult, 0, len(rows))
	for _, row := range rows {
		result := RoomResult{
			ID:         row.ID,
			Name:       row.Name,
			Kind:       row.Kind,
			Visibility: row.Visibility,
			// sqlc types a computed boolean as interface{}, since it cannot
			// see through the expression. A failed assertion yields false,
			// which is the safe reading for both: an unknown room is treated
			// as not archived and not joined, so a client offers to join
			// rather than wrongly claiming membership.
			Archived: asBool(row.Archived),
			Member:   asBool(row.IsMember),
		}
		if row.Slug != nil {
			result.Slug = *row.Slug
		}
		result.Topic = row.Topic
		results = append(results, result)
	}

	return results, nil
}

// People finds somebody by name.
func (s *Service) People(ctx context.Context, query string, limit int) ([]PersonResult, error) {
	text := strings.TrimSpace(query)
	if text == "" {
		return nil, nil
	}
	if limit <= 0 || limit > maxResults {
		limit = maxResults
	}

	rows, err := s.queries.SearchPeople(ctx, dbgen.SearchPeopleParams{
		Query:       text,
		ResultLimit: int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("search: people: %w", err)
	}

	results := make([]PersonResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, PersonResult{
			ID:          row.ID,
			Username:    row.Username,
			DisplayName: row.DisplayName,
		})
	}

	return results, nil
}

// asBool reads a boolean sqlc could not type, defaulting to false.
//
// Used for computed columns, where sqlc sees an expression rather than a
// column and gives up on the type. False is the safe default for every use
// here: a room whose membership cannot be read is treated as not joined,
// which offers to join rather than claiming access somebody may not have.
func asBool(value any) bool {
	got, ok := value.(bool)
	return ok && got
}
