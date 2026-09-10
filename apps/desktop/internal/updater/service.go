package updater

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/AnoRebel/tdc/apps/desktop/internal/buildinfo"
)

/*
The updater, as the interface sees it.

Everything above this is written so nothing installs itself: checking says what
exists, downloading prepares it, and installing happens when somebody says so.
The three are separate methods for that reason rather than for tidiness.
*/

// State is what the interface shows about updates.
type State struct {
	// Enabled is false where this build cannot verify anything, in which case
	// nothing else here will happen.
	Enabled bool `json:"enabled"`
	// Current is the version running now.
	Current string `json:"current"`
	// Available is the newest version found, or empty.
	Available string `json:"available,omitempty"`
	// Notes is what changed in it.
	Notes string `json:"notes,omitempty"`
	// Ready is true once an update has been downloaded and verified.
	Ready bool `json:"ready"`
	// Checking is true while a check is in flight.
	Checking bool `json:"checking"`
	// Problem is what went wrong with the last attempt, in words.
	Problem string `json:"problem,omitempty"`
}

/*
Service holds the updater's state for the life of the application.

One per application: two would check twice and could both be downloading the
same release.
*/
type Service struct {
	feedURL    string
	store      *Store
	downloader *Downloader
	client     *http.Client

	mu        sync.RWMutex
	state     State
	release   Release
	readyPath string
}

/*
NewService builds the updater.

The feed URL is passed in rather than fixed here so a fork can point at its own
releases without editing code — verification still binds to this project's
signing identity, so pointing it elsewhere yields refusals rather than somebody
else's application.
*/
func NewService(feedURL, settingsPath, downloadDir string) *Service {
	client := &http.Client{Timeout: 5 * time.Minute}

	return &Service{
		feedURL:    feedURL,
		store:      OpenStore(settingsPath),
		downloader: NewDownloader(client, downloadDir),
		client:     client,
		state:      State{Enabled: Enabled(), Current: buildinfo.Version},
	}
}

// UpdateState reports what the interface should show.
func (s *Service) UpdateState() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// UpdateSettings reports when this looks for updates.
func (s *Service) UpdateSettings() Settings {
	return s.store.Settings()
}

// SetUpdateSettings records a change, refusing what it cannot use.
func (s *Service) SetUpdateSettings(settings Settings) error {
	return s.store.Save(settings)
}

/*
CheckForUpdate asks the feed what the newest release is.

Returns the state rather than an error for an ordinary failure: a feed that
could not be reached is something to tell somebody quietly, not an exception
for the interface to handle.
*/
func (s *Service) CheckForUpdate(ctx context.Context) State {
	s.mu.Lock()
	if !s.state.Enabled {
		defer s.mu.Unlock()
		s.state.Problem = "This build cannot verify updates, so it will not install one."
		return s.state
	}
	s.state.Checking = true
	s.state.Problem = ""
	s.mu.Unlock()

	release, newer, err := Check(ctx, s.client, s.feedURL, buildinfo.Version)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Checking = false

	if err != nil {
		s.state.Problem = err.Error()
		return s.state
	}

	s.store.MarkChecked(time.Now())

	if !newer {
		s.state.Available = ""
		s.state.Notes = ""
		return s.state
	}

	s.release = release
	s.state.Available = release.Version
	s.state.Notes = release.Notes

	return s.state
}

/*
DownloadUpdate fetches the release found by the last check and verifies it.

Separate from installing, so somebody can have it ready without having agreed
to replace what they are running.
*/
func (s *Service) DownloadUpdate(ctx context.Context) State {
	s.mu.RLock()
	release := s.release
	s.mu.RUnlock()

	if release.Version == "" {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.state.Problem = "There is nothing to download; check for an update first."
		return s.state
	}

	path, err := s.downloader.Fetch(ctx, release)

	s.mu.Lock()
	defer s.mu.Unlock()

	if err != nil {
		s.state.Problem = err.Error()
		s.state.Ready = false
		return s.state
	}

	s.readyPath = path
	s.state.Ready = true
	s.state.Problem = ""

	return s.state
}

/*
InstallUpdate puts a downloaded update in place.

The application has to be restarted afterwards, which the caller does: this
package should not decide when somebody's application closes.
*/
func (s *Service) InstallUpdate() error {
	s.mu.RLock()
	path := s.readyPath
	s.mu.RUnlock()

	if path == "" {
		return ErrNotVerified
	}

	if err := s.downloader.Install(path); err != nil {
		s.mu.Lock()
		s.state.Problem = err.Error()
		s.mu.Unlock()
		return err
	}

	s.mu.Lock()
	s.state.Ready = false
	s.readyPath = ""
	s.mu.Unlock()

	return nil
}

/*
DiscardUpdate forgets a downloaded update and removes it.

Called when somebody declines. A verified binary left in a temporary directory
is a thing somebody may later run without knowing what it is.
*/
func (s *Service) DiscardUpdate() {
	s.mu.Lock()
	path := s.readyPath
	s.readyPath = ""
	s.state.Ready = false
	s.mu.Unlock()

	if path != "" {
		s.downloader.Discard(path)
	}
}

/*
Start begins whatever checking the settings ask for.

Runs until the context ends. A check on launch is delayed rather than immediate
— an application's first seconds belong to showing somebody their messages, not
to a request about itself.
*/
func (s *Service) Start(ctx context.Context) {
	settings := s.store.Settings()

	if settings.OnLaunch {
		go func() {
			select {
			case <-time.After(30 * time.Second):
			case <-ctx.Done():
				return
			}
			s.checkAndMaybeDownload(ctx)
		}()
	}

	// Nothing scheduled means nothing to wake for.
	if _, wanted := settings.Every.Every(); !wanted {
		return
	}

	go func() {
		/*
			Woken often, acting rarely.

			The interval is a day or a week, and a timer that long does not
			survive a laptop being closed — so this wakes hourly and asks the
			settings whether a check is actually due.
		*/
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if s.store.Settings().Due(time.Now()) {
					s.checkAndMaybeDownload(ctx)
				}
			}
		}
	}()
}

// checkAndMaybeDownload checks, and fetches too where that was asked for.
func (s *Service) checkAndMaybeDownload(ctx context.Context) {
	state := s.CheckForUpdate(ctx)

	if state.Available != "" && s.store.Settings().Download {
		s.DownloadUpdate(ctx)
	}
}

/*
DefaultPaths returns where settings and downloads belong on this machine.

Under the user's own configuration and cache directories, so nothing is written
where another account could read or replace it.
*/
func DefaultPaths() (settings string, downloads string) {
	if config, err := os.UserConfigDir(); err == nil && config != "" {
		settings = filepath.Join(config, "tdc", "updates.json")
	}

	if cache, err := os.UserCacheDir(); err == nil && cache != "" {
		downloads = filepath.Join(cache, "tdc", "updates")
	}

	return settings, downloads
}
