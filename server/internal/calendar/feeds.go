package calendar

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
Personal subscription addresses.

The secret is in the address, because a calendar application subscribing to a
feed has nowhere else to put a credential. It is drawn from the system's own
randomness and stored only as its SHA-256 hash, as call links are: the defence
is its entropy, not a slow hash. It is shown once, and replaced or revoked by
its owner.
*/

// FeedSecretPrefix starts every feed secret, so one is recognisable in a log
// or a leak report for what it is.
const FeedSecretPrefix = "tdcf_"

// feedHistory is how far back a feed reaches: recent events stay, so an
// application does not drop last week's meetup the moment it ends.
const feedHistory = 60 * 24 * time.Hour

func feedHash(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

// MakeFeed makes, or replaces, somebody's subscription address and returns
// its secret, which is not kept.
func (s *Service) MakeFeed(ctx context.Context, userID uuid.UUID) (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate feed secret: %w", err)
	}
	secret := FeedSecretPrefix + base64.RawURLEncoding.EncodeToString(raw)
	if err := s.queries.SetCalendarFeed(ctx, dbgen.SetCalendarFeedParams{UserID: userID, TokenHash: feedHash(secret)}); err != nil {
		return "", fmt.Errorf("store feed: %w", err)
	}
	return secret, nil
}

// RevokeFeed withdraws somebody's address; reports whether there was one.
func (s *Service) RevokeFeed(ctx context.Context, userID uuid.UUID) (bool, error) {
	removed, err := s.queries.DeleteCalendarFeed(ctx, userID)
	return removed > 0, err
}

// Feed is somebody's address as they may be told about it: whether it exists
// and when it was last fetched.
func (s *Service) Feed(ctx context.Context, userID uuid.UUID) (dbgen.CalendarFeed, bool) {
	feed, err := s.queries.GetCalendarFeed(ctx, userID)
	return feed, err == nil
}

// FeedOwner is whose address a secret is, noting that it was used.
func (s *Service) FeedOwner(ctx context.Context, secret string) (uuid.UUID, error) {
	if !strings.HasPrefix(secret, FeedSecretPrefix) {
		return uuid.Nil(), ErrNotFound
	}
	owner, err := s.queries.UseCalendarFeed(ctx, feedHash(secret))
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil(), ErrNotFound
	}
	return owner, err
}

/*
FeedEvents are what somebody's feed holds, decided for them now: the events
they host or speak at, the ones they are going or maybe going to or waiting
for, and the events of their channels that they may see.
*/
func (s *Service) FeedEvents(ctx context.Context, viewer Viewer) ([]Listed, error) {
	since := time.Now().Add(-feedHistory)
	answers, err := s.queries.ListMyCalendarAnswers(ctx, viewer.UserID)
	if err != nil {
		return nil, fmt.Errorf("read answers: %w", err)
	}
	ids := []uuid.UUID{}
	for id := range viewer.Hosting {
		ids = append(ids, id)
	}
	for _, answer := range answers {
		if answer.Status != Declined {
			ids = append(ids, answer.EventID)
		}
	}
	chosen, err := s.queries.ListCalendarEventsByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("read events: %w", err)
	}
	channels := []uuid.UUID{}
	for id := range viewer.Channels {
		channels = append(channels, id)
	}
	inChannels, err := s.queries.ListCalendarEventsInChannels(ctx, dbgen.ListCalendarEventsInChannelsParams{
		Channels: channels, Since: since,
	})
	if err != nil {
		return nil, fmt.Errorf("read channel events: %w", err)
	}

	seen := map[uuid.UUID]bool{}
	var events []Listed
	add := func(event Listed) {
		if seen[event.ID] || !viewer.Sees(event) {
			return
		}
		if event.LastEndsAt != nil && event.LastEndsAt.Before(since) {
			return
		}
		seen[event.ID] = true
		events = append(events, event)
	}
	for _, row := range chosen {
		add(listedOf(row.CalendarEvent, row.ChannelName, row.ChannelVisibility))
	}
	for _, row := range inChannels {
		add(listedOf(row.CalendarEvent, row.ChannelName, row.ChannelVisibility))
	}
	return events, nil
}

// PublicEvents are the server's public events, for the public feed.
func (s *Service) PublicEvents(ctx context.Context) ([]Listed, error) {
	rows, err := s.queries.ListPublicCalendarEvents(ctx, time.Now().Add(-feedHistory))
	if err != nil {
		return nil, fmt.Errorf("read public events: %w", err)
	}
	events := make([]Listed, 0, len(rows))
	for _, row := range rows {
		events = append(events, listedOf(row.CalendarEvent, row.ChannelName, row.ChannelVisibility))
	}
	return events, nil
}
