package updater

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

/*
When to look for a newer version, and where to keep that decision.

Checking is a network request to a third party that says something about this
machine — that TDC is installed and which version. Somebody may reasonably not
want that on a schedule, so it is a setting rather than an assumption, and the
default is modest.
*/

// How often to look, as somebody would choose it.
type Interval string

const (
	// NeverCheck means only when somebody asks.
	NeverCheck Interval = "never"
	// Daily is the default: often enough to hear about a security fix, rarely
	// enough not to be a heartbeat.
	Daily Interval = "daily"
	// Weekly, for somebody who would rather it were quieter still.
	Weekly Interval = "weekly"
)

// Every reports how much time an interval means, and whether it means any.
func (i Interval) Every() (time.Duration, bool) {
	switch i {
	case Daily:
		return 24 * time.Hour, true
	case Weekly:
		return 7 * 24 * time.Hour, true
	default:
		return 0, false
	}
}

/*
Settings is what somebody chose about updates.

Defaults are deliberately quiet: check on launch and daily, and never install
anything without being asked. An updater that installed on its own would be
making a decision about somebody's machine that only they can make.
*/
type Settings struct {
	// OnLaunch checks once shortly after starting.
	OnLaunch bool `json:"onLaunch"`
	// Every is how often to check while running.
	Every Interval `json:"every"`
	/*
		Download fetches and verifies an update as soon as one is found,
		leaving it ready to install.

		Still never installs by itself: what this saves is the wait, not the
		decision.
	*/
	Download bool `json:"download"`
	// LastChecked is when the last check happened, so a schedule survives a
	// restart rather than checking afresh on every launch.
	LastChecked time.Time `json:"lastChecked,omitempty"`
}

// Defaults are what somebody gets who has never opened this.
func Defaults() Settings {
	return Settings{OnLaunch: true, Every: Daily, Download: false}
}

/*
Valid reports why these settings cannot be used, or nil.

Checked because they are written by an API a program may call: an interval
this does not understand would silently become "never", which is a setting
somebody did not choose.
*/
func (s Settings) Valid() error {
	switch s.Every {
	case NeverCheck, Daily, Weekly:
		return nil
	default:
		return fmt.Errorf("%q is not how often this can check; use never, daily or weekly", s.Every)
	}
}

/*
Due reports whether it is time to check again.

A check on launch counts as a check, so starting an application several times
in a morning does not mean several checks.
*/
func (s Settings) Due(now time.Time) bool {
	every, wanted := s.Every.Every()
	if !wanted {
		return false
	}

	if s.LastChecked.IsZero() {
		return true
	}
	return now.Sub(s.LastChecked) >= every
}

/*
Store keeps settings across launches.

A file rather than the keychain: none of this is secret, and a keychain prompt
for "how often to check for updates" would be absurd.
*/
type Store struct {
	path string

	mu       sync.RWMutex
	settings Settings
}

/*
OpenStore reads what was saved, or starts from the defaults.

An unreadable file is not an error: somebody's preferences being lost is worth
a quiet reset, where refusing to start the updater over a malformed JSON file
would lose the updater as well.
*/
func OpenStore(path string) *Store {
	store := &Store{path: path, settings: Defaults()}

	contents, err := os.ReadFile(path)
	if err != nil {
		return store
	}

	var saved Settings
	if err := json.Unmarshal(contents, &saved); err != nil {
		return store
	}
	if saved.Valid() != nil {
		return store
	}

	store.settings = saved
	return store
}

// Settings returns what is currently chosen.
func (s *Store) Settings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings
}

/*
Save records a change.

Refuses settings it does not understand rather than storing them, so a bad
value cannot survive a restart and become the state everything reads.
*/
func (s *Store) Save(settings Settings) error {
	if err := settings.Valid(); err != nil {
		return err
	}

	s.mu.Lock()
	// LastChecked is this package's to keep, not the interface's to send.
	settings.LastChecked = s.settings.LastChecked
	s.settings = settings
	path := s.path
	toWrite := s.settings
	s.mu.Unlock()

	return write(path, toWrite)
}

// MarkChecked records that a check has just happened.
func (s *Store) MarkChecked(when time.Time) {
	s.mu.Lock()
	s.settings.LastChecked = when
	path, toWrite := s.path, s.settings
	s.mu.Unlock()

	// A failure to record the time means checking again sooner than intended,
	// which is not worth reporting to somebody.
	_ = write(path, toWrite)
}

/*
write saves settings to disk, creating the directory if it is missing.

Written to a temporary file and renamed, so an interrupted write leaves the
previous settings rather than a half-written file that reads as none.
*/
func write(path string, settings Settings) error {
	if path == "" {
		return errors.New("no path was given for the update settings")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("making room for the update settings: %w", err)
	}

	encoded, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding the update settings: %w", err)
	}

	temporary := path + ".writing"
	if err := os.WriteFile(temporary, encoded, 0o644); err != nil {
		return fmt.Errorf("writing the update settings: %w", err)
	}

	return os.Rename(temporary, path)
}
