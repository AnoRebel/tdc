package calendar

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
	"uuid"

	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/delivery"
	"github.com/AnoRebel/tdc/server/internal/unfurl"
)

/*
Following calendars published elsewhere.

The address is somebody else's and is fetched on a schedule, which makes this
an outbound request a person steers, like a link preview or a webhook — and it
is guarded the same way. The address is checked when the subscription is made,
so somebody is told at once that it can never work; and every connection,
including after a redirect, is judged after DNS has resolved the name and
immediately before it is made, so a name that resolves somewhere public for the
check and somewhere private a moment later gains nothing.
*/

const (
	// MaxFetchBytes is the most of a calendar read.
	MaxFetchBytes = 2 << 20
	// FetchTimeout bounds one fetch, every redirect included.
	FetchTimeout = 20 * time.Second
	// maxRedirects is how many times a fetch follows somebody elsewhere.
	maxRedirects = 3
	// PauseAfter is how many failures in a row pause a subscription.
	PauseAfter = 10
)

// managePermission is what following an outside calendar takes.
const managePermission = authz.PermEventCalendarsManage

// ErrAddress is an address that cannot be subscribed to.
var ErrAddress = errors.New("that is not a calendar address this server will fetch")

// FetchAddress turns what somebody pasted into what is fetched: webcal is
// https by another name.
func FetchAddress(raw string) string {
	raw = strings.TrimSpace(raw)
	if rest, ok := strings.CutPrefix(raw, "webcal://"); ok {
		return "https://" + rest
	}
	if rest, ok := strings.CutPrefix(raw, "webcals://"); ok {
		return "https://" + rest
	}
	return raw
}

/*
Fetcher fetches calendars through the private-address guard. allowPrivate is
asked on every connection, so an operator's change takes effect at once.
*/
type Fetcher struct {
	client *http.Client
}

// NewFetcher builds a fetcher.
func NewFetcher(allowPrivate func() bool) *Fetcher {
	plain := &net.Dialer{Timeout: FetchTimeout}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if allowPrivate != nil && allowPrivate() {
				return plain.DialContext(ctx, network, address)
			}
			return unfurl.DialContext(ctx, network, address)
		},
		DisableKeepAlives:     true,
		TLSHandshakeTimeout:   FetchTimeout,
		ResponseHeaderTimeout: FetchTimeout,
		// A proxy from the environment would carry the request past the guard.
		Proxy: nil,
	}
	return &Fetcher{client: &http.Client{
		Transport: transport,
		Timeout:   FetchTimeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errors.New("the calendar sent the request elsewhere too many times")
			}
			if request.URL.Scheme != "https" && request.URL.Scheme != "http" {
				return errors.New("the calendar sent the request somewhere that is not a web address")
			}
			return nil
		},
	}}
}

// Fetched is what one fetch brought back.
type Fetched struct {
	Body         []byte
	ETag         string
	LastModified string
	// Unchanged is a 304: the calendar is as it was.
	Unchanged bool
}

// Fetch reads a calendar, asking only for a change since the last time.
func (f *Fetcher) Fetch(ctx context.Context, address, etag, lastModified string) (Fetched, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, FetchAddress(address), nil)
	if err != nil {
		return Fetched{}, ErrAddress
	}
	request.Header.Set("Accept", "text/calendar")
	request.Header.Set("User-Agent", "TDC-Calendar/1")
	if etag != "" {
		request.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		request.Header.Set("If-Modified-Since", lastModified)
	}
	response, err := f.client.Do(request)
	if err != nil {
		if strings.Contains(err.Error(), "not a public address") {
			return Fetched{}, fmt.Errorf("%w: it leads to a private or reserved address", ErrAddress)
		}
		return Fetched{}, fmt.Errorf("the calendar could not be fetched: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotModified {
		return Fetched{Unchanged: true, ETag: etag, LastModified: lastModified}, nil
	}
	if response.StatusCode != http.StatusOK {
		return Fetched{}, fmt.Errorf("the calendar answered %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, MaxFetchBytes+1))
	if err != nil {
		return Fetched{}, fmt.Errorf("the calendar could not be read: %w", err)
	}
	if len(body) > MaxFetchBytes {
		return Fetched{}, fmt.Errorf("the calendar is larger than %d bytes", MaxFetchBytes)
	}
	return Fetched{Body: body, ETag: response.Header.Get("ETag"), LastModified: response.Header.Get("Last-Modified")}, nil
}

// NewSubscription is what somebody asks to follow.
type NewSubscription struct {
	URL        string
	Label      string
	ChannelID  *uuid.UUID
	Visibility string
	Kind       string
}

/*
Subscribe makes a subscription, for somebody allowed to: event.calendars.manage
across the server, or in the channel its events will belong to — which they
must be able to read. The address is checked before anything is stored.
*/
func (s *Service) Subscribe(ctx context.Context, actor dbgen.User, viewer Viewer, request NewSubscription, allowPrivate bool) (dbgen.CalendarSubscription, error) {
	request.Label = strings.TrimSpace(request.Label)
	if request.Label == "" || utf8.RuneCountInString(request.Label) > 80 {
		return dbgen.CalendarSubscription{}, invalid("a subscription needs a label of up to 80 characters")
	}
	if request.Kind == "" {
		request.Kind = KindOther
	}
	if !slices.Contains(Kinds, request.Kind) {
		return dbgen.CalendarSubscription{}, invalid("a kind is one of %s", strings.Join(Kinds, ", "))
	}
	if request.ChannelID != nil {
		channel, err := s.channelFor(ctx, *request.ChannelID, viewer)
		if err != nil {
			return dbgen.CalendarSubscription{}, err
		}
		if !viewer.Permissions.AllowsChannel(managePermission, channel.ID).Allowed {
			return dbgen.CalendarSubscription{}, ErrNotAllowed
		}
		if request.Visibility == "" {
			request.Visibility = VisibilityChannel
		}
		if request.Visibility == VisibilityPublic && channel.Visibility != "public" {
			return dbgen.CalendarSubscription{}, invalid("an event in a private channel cannot be public")
		}
	} else {
		if !viewer.Permissions.AllowsServer(managePermission).Allowed {
			return dbgen.CalendarSubscription{}, ErrNotAllowed
		}
		request.Visibility = VisibilityPublic
	}
	if request.Visibility != VisibilityPublic && request.Visibility != VisibilityChannel {
		return dbgen.CalendarSubscription{}, invalid("a subscription's events are public or for the channel")
	}
	address := FetchAddress(request.URL)
	if len(address) > MaxURL {
		return dbgen.CalendarSubscription{}, ErrAddress
	}
	if err := delivery.CheckDestination(ctx, address, allowPrivate); err != nil {
		return dbgen.CalendarSubscription{}, fmt.Errorf("%w: %w", ErrAddress, err)
	}
	return s.queries.CreateCalendarSubscription(ctx, dbgen.CreateCalendarSubscriptionParams{
		ID: uuid.New(), Url: address, Label: request.Label, ChannelID: request.ChannelID,
		Visibility: request.Visibility, Kind: request.Kind, CreatedBy: &actor.ID,
	})
}

// MayManageSubscription is whether somebody may refresh or remove one.
func (v Viewer) MayManageSubscription(subscription dbgen.CalendarSubscription) bool {
	if subscription.ChannelID != nil {
		return v.Permissions.AllowsChannel(managePermission, *subscription.ChannelID).Allowed
	}
	return v.Permissions.AllowsServer(managePermission).Allowed
}

/*
Refresh fetches a subscription now and brings its events into line, recording
how it went. Failing PauseAfter times in a row pauses it, so a calendar that
has gone away is not fetched forever.
*/
func (s *Service) Refresh(ctx context.Context, fetcher *Fetcher, subscription dbgen.CalendarSubscription, every time.Duration) (Synced, error) {
	fetched, err := fetcher.Fetch(ctx, subscription.Url, subscription.Etag, subscription.LastModified)
	var synced Synced
	if err == nil && !fetched.Unchanged {
		var incoming []Incoming
		if incoming, err = ParseICS(fetched.Body); err == nil {
			synced, err = s.Sync(ctx, subscription, incoming)
		}
	}
	record := dbgen.RecordCalendarFetchParams{
		ID: subscription.ID, NextFetchAt: time.Now().Add(every),
		Etag: subscription.Etag, LastModified: subscription.LastModified,
	}
	if err != nil {
		record.LastError = err.Error()
		record.Failures = subscription.Failures + 1
		if record.Failures >= PauseAfter {
			now := time.Now()
			record.PausedAt = &now
		}
	} else {
		record.Etag, record.LastModified = fetched.ETag, fetched.LastModified
	}
	if recordErr := s.queries.RecordCalendarFetch(ctx, record); recordErr != nil {
		s.log.Warn("could not record a calendar fetch", "subscription", subscription.ID, "error", recordErr)
	}
	return synced, err
}
