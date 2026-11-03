package updater

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/AnoRebel/tdc/apps/desktop/internal/buildinfo"
	wails "github.com/wailsapp/wails/v3/pkg/updater"
)

/*
The updater, as the interface sees it.

Everything above this is written so nothing installs itself: checking says what
exists, downloading prepares it, and installing happens when somebody says so.
The three are separate methods for that reason rather than for tidiness.
*/

// State is what the interface shows about updates.
type State struct {
	// Enabled is false where this build cannot install updates, in which case
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
same release. Wails' updater does the downloading, matching, swapping and
restarting; this decides when, and says how it went.
*/
type Service struct {
	source wails.Provider
	store  *Store
	wails  *wails.Updater

	mu    sync.RWMutex
	state State
}

// NewService builds the updater, finding releases in a GitHub repository's.
func NewService(repository, settingsPath string) *Service {
	source, err := releasesOf(repository, &http.Client{Timeout: 5 * time.Minute})
	service := &Service{
		source: source,
		store:  OpenStore(settingsPath),
		state:  State{Current: buildinfo.Version},
	}
	if err != nil {
		service.state.Problem = err.Error()
	}
	return service
}

/*
Use hands over the application's updater, once the application exists.

No window of Wails' own: the Updates page is where this is shown, and nothing
happens that somebody there did not ask for.
*/
func (s *Service) Use(updater *wails.Updater) error {
	if s.source == nil {
		return errors.New(s.state.Problem)
	}
	if err := updater.Init(wails.Config{
		CurrentVersion: buildinfo.Version,
		Providers:      []wails.Provider{s.source},
		Window:         wails.WindowNone,
	}); err != nil {
		return err
	}
	s.mu.Lock()
	s.wails = updater
	s.state.Enabled = true
	s.mu.Unlock()
	return nil
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

// ready is the updater, or why there is none to use.
func (s *Service) ready() (*wails.Updater, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.wails == nil {
		return nil, "This build cannot install updates; download a new version from the releases page."
	}
	return s.wails, ""
}

/*
CheckForUpdate asks the feed what the newest release is, and whether its
signature proves it ours.

Returns the state rather than an error for an ordinary failure: a feed that
could not be reached is something to tell somebody quietly, not an exception
for the interface to handle.
*/
func (s *Service) CheckForUpdate(ctx context.Context) State {
	updater, problem := s.ready()
	s.mu.Lock()
	if updater == nil {
		defer s.mu.Unlock()
		s.state.Problem = problem
		return s.state
	}
	s.state.Checking = true
	s.state.Problem = ""
	s.mu.Unlock()

	release, err := updater.Check(ctx)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Checking = false
	if err != nil {
		s.state.Problem = err.Error()
		return s.state
	}
	s.store.MarkChecked(time.Now())
	if release == nil {
		s.state.Available, s.state.Notes, s.state.Ready = "", "", false
		return s.state
	}
	s.state.Available = release.Version
	s.state.Notes = release.Notes
	return s.state
}

/*
DownloadUpdate fetches the release found by the last check and holds it to
the digest its signature vouches for.

Separate from installing, so somebody can have it ready without having agreed
to replace what they are running.
*/
func (s *Service) DownloadUpdate(ctx context.Context) State {
	updater, problem := s.ready()
	if updater == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.state.Problem = problem
		return s.state
	}

	err := updater.DownloadAndInstall(ctx)

	s.mu.Lock()
	defer s.mu.Unlock()
	if errors.Is(err, wails.ErrNoPendingRelease) {
		s.state.Problem = "There is nothing to download; check for an update first."
		return s.state
	}
	if err != nil {
		s.state.Problem = err.Error()
		s.state.Ready = false
		return s.state
	}
	s.state.Ready = true
	s.state.Problem = ""
	return s.state
}

/*
InstallUpdate puts the downloaded update in place and restarts into it.

The application quits; Wails' helper swaps the files once it has, and starts
the new version — rolling back to this one if the new one will not start.
*/
func (s *Service) InstallUpdate(ctx context.Context) error {
	updater, problem := s.ready()
	if updater == nil {
		return errors.New(problem)
	}
	if err := updater.Restart(ctx); err != nil {
		s.mu.Lock()
		s.state.Problem = err.Error()
		s.mu.Unlock()
		return err
	}
	return nil
}

/*
DiscardUpdate declines a downloaded update for now. Wails' updater replaces
what it staged when the next one is downloaded.
*/
func (s *Service) DiscardUpdate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Ready = false
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
DefaultPath returns where update settings belong on this machine: under the
user's own configuration directory, where another account cannot change them.
*/
func DefaultPath() string {
	if config, err := os.UserConfigDir(); err == nil && config != "" {
		return filepath.Join(config, "tdc", "updates.json")
	}
	return ""
}
