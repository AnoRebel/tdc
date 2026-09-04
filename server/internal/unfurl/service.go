package unfurl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/AnoRebel/tdc/server/internal/cache"
)

// Cached previews live this long.
//
// A page's title and description change rarely, and re-fetching on every render
// would mean one outbound request per viewer per message — which is both slow
// for the reader and rude to the site being linked. A day is long enough to be
// worth having and short enough that a corrected headline reaches people.
const (
	previewTTL = 24 * time.Hour

	// A link that produced nothing is remembered too, and for much less time.
	// Without this, a message linking to a site that is down would send an
	// outbound request every time anybody scrolled past it.
	failureTTL = 15 * time.Minute
)

// Settings say whether a link may be fetched at all.
type Settings struct {
	// Enabled is the operator's switch. When off, no link is ever fetched,
	// whatever an individual has chosen.
	Enabled bool
}

// UserPreference is a person's own choice about previews.
//
// Separate from the operator's setting because the two answer different
// questions: the operator decides whether this server makes outbound requests
// at all, and a person decides whether their own messages cause them. Someone
// on metered data may reasonably want neither the fetch nor the picture.
type UserPreference struct {
	Enabled bool
}

// Service fetches previews, subject to both switches, and remembers what it
// found.
type Service struct {
	fetcher  *Fetcher
	cache    cache.Cache
	settings Settings
	log      *slog.Logger
}

// NewService wires a fetcher to a cache.
func NewService(store cache.Cache, settings Settings, log *slog.Logger) *Service {
	return &Service{
		fetcher:  NewFetcher(),
		cache:    store,
		settings: settings,
		log:      log,
	}
}

// Enabled reports whether the operator permits previews at all.
func (s *Service) Enabled() bool { return s.settings.Enabled }

/*
Preview returns what a link is, or nothing.

Both switches are checked before anything else happens, and the check is on
whether to make the request rather than on whether to show the result. That
distinction is the whole point: someone who turns previews off is asking this
server not to tell a third party that they were sent a link, and fetching it
anyway and then hiding the answer would leak exactly what they were avoiding.
*/
func (s *Service) Preview(ctx context.Context, rawURL string, preference UserPreference) (*Preview, error) {
	if !s.settings.Enabled || !preference.Enabled {
		return nil, ErrNotPreviewable
	}

	key := cacheKey(rawURL)

	if cached, found, err := s.cache.Get(ctx, key); err == nil && found {
		var entry cacheEntry
		if json.Unmarshal(cached, &entry) == nil {
			if entry.Failed {
				return nil, ErrNotPreviewable
			}
			if entry.Preview != nil {
				return entry.Preview, nil
			}
		}
		// A malformed entry is treated as a miss rather than an error: the
		// worst case is one extra fetch.
	}

	preview, err := s.fetcher.Fetch(ctx, rawURL)
	if err != nil {
		// Remembered as a failure, so a dead link does not cost an outbound
		// request every time somebody scrolls past the message.
		s.remember(ctx, key, cacheEntry{Failed: true}, failureTTL)

		if !errors.Is(err, ErrNotPreviewable) {
			// Logged rather than returned to the caller: a preview that could
			// not be made is not a failure of the message it belongs to, and
			// an operator debugging outbound requests needs to see the reason.
			s.log.Debug("cannot preview a link", "error", err)
		}
		return nil, ErrNotPreviewable
	}

	s.remember(ctx, key, cacheEntry{Preview: preview}, previewTTL)
	return preview, nil
}

// cacheEntry is what is stored, including the fact that a link produced
// nothing.
type cacheEntry struct {
	Preview *Preview `json:"preview,omitempty"`
	Failed  bool     `json:"failed,omitempty"`
}

func (s *Service) remember(ctx context.Context, key string, entry cacheEntry, ttl time.Duration) {
	encoded, err := json.Marshal(entry)
	if err != nil {
		return
	}

	// A cache that will not store is not worth failing a request over; the
	// next reader simply fetches again.
	if err := s.cache.Set(ctx, key, encoded, ttl); err != nil {
		s.log.Debug("cannot cache a link preview", "error", err)
	}
}

// cacheKey namespaces previews away from everything else in the cache.
func cacheKey(rawURL string) string {
	return fmt.Sprintf("unfurl:v1:%s", rawURL)
}
