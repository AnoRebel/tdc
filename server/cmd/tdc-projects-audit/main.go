/*
Checks a project's history from outside the server that wrote it.

Reads a project's event stream straight from its database and holds it to three
things: every event verifies against the keys the server publishes — which is
all an auditor or a peer would have — the stream is one gapless order, and
replaying it from nothing reproduces every issue's current columns. A
demonstration of the issue history's guarantees that goes through the same
checks anybody else could make, rather than asking the server to vouch for
itself.
*/
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/federation"
	"github.com/AnoRebel/tdc/server/internal/projects"
	"github.com/AnoRebel/tdc/server/internal/rooms"
)

func main() {
	databaseURL := flag.String("database", "", "the server's database")
	published := flag.String("published", "", "the server's discovery document, for its public keys")
	key := flag.String("project", "", "the project's key")
	flag.Parse()

	if *databaseURL == "" || *published == "" || *key == "" {
		fmt.Fprintln(os.Stderr, "--database, --published and --project are required")
		os.Exit(2)
	}
	if err := audit(*databaseURL, *published, *key); err != nil {
		fmt.Fprintf(os.Stderr, "\nFAILED: %v\n", err)
		os.Exit(1)
	}
}

func audit(databaseURL, published, key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	keyring, err := publishedKeys(ctx, published)
	if err != nil {
		return err
	}

	pool, err := db.Open(ctx, db.Config{URL: databaseURL}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		return err
	}
	defer pool.Close()
	queries := dbgen.New(pool)

	project, err := queries.GetProjectByKey(ctx, strings.ToLower(key))
	if err != nil {
		return fmt.Errorf("find project %s: %w", key, err)
	}

	var stream []dbgen.Event
	var after int64
	for {
		page, err := queries.ListProjectStream(ctx, dbgen.ListProjectStreamParams{
			ProjectID: project.ID, AfterSequence: after, PageSize: 500,
		})
		if err != nil {
			return fmt.Errorf("read stream: %w", err)
		}
		stream = append(stream, page...)
		if len(page) < 500 {
			break
		}
		after = page[len(page)-1].Sequence
	}

	for i, event := range stream {
		if event.Sequence != int64(i+1) {
			return fmt.Errorf("the stream has a gap or a repeat at position %d (sequence %d)", i+1, event.Sequence)
		}
		if err := keyring.Verify(rooms.Verifiable(event)); err != nil {
			return fmt.Errorf("%s at %d does not verify: %w", event.Type, event.Sequence, err)
		}
	}
	fmt.Printf("  ok  %d events in %s's stream, one gapless order, each verified against the published keys\n",
		len(stream), project.Key)

	replayed, err := projects.Replay(stream)
	if err != nil {
		return fmt.Errorf("replay: %w", err)
	}
	for id, issue := range replayed {
		stored, err := queries.GetIssueByID(ctx, id)
		if err != nil {
			return fmt.Errorf("read issue %s-%d: %w", project.Key, issue.Number, err)
		}
		want := projects.Replayed{
			Number: stored.Number, Title: stored.Title, Description: stored.Description,
			StateID: stored.StateID, Deleted: stored.DeletedAt != nil,
		}
		if issue != want {
			return fmt.Errorf("%s-%d replays as %+v, but its columns say %+v", project.Key, stored.Number, issue, want)
		}
	}
	fmt.Printf("  ok  replaying the stream from nothing reproduces all %d issues exactly\n", len(replayed))
	return nil
}

// publishedKeys fetches a server's public keys from its discovery document.
func publishedKeys(ctx context.Context, url string) (*events.Keyring, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch published keys: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	var document federation.Document
	if err := json.NewDecoder(response.Body).Decode(&document); err != nil {
		return nil, fmt.Errorf("read published keys: %w", err)
	}
	if len(document.Keys) == 0 {
		return nil, errors.New("the server publishes no keys")
	}
	return events.KeyringFromPublished(document.Keys)
}
