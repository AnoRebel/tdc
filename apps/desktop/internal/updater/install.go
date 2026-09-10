package updater

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

/*
Fetching an update and putting it in place.

Two rules run through all of it. Nothing is installed that this process did not
itself verify — not merely something that exists on disk where a verified
download was put. And the running binary is replaced by a rename rather than a
write, so an interrupted install leaves the old application working instead of
half of a new one.
*/

var (
	// ErrNotVerified is returned when something is asked to be installed that
	// was never checked. Always terminal.
	ErrNotVerified = errors.New("this update was not verified, so it will not be installed")

	// ErrTooLarge is returned when a download exceeds what the feed declared.
	ErrTooLarge = errors.New("the download was larger than the release said")
)

/*
Downloader fetches and verifies updates.

Keeps a record of what it verified, so installing can insist on it. The record
is in memory and dies with the process, which is the point: a download verified
by a previous run is a file on disk with no proof attached.
*/
type Downloader struct {
	client *http.Client
	// Where downloads are put, removed when they are installed or abandoned.
	dir string

	mu sync.Mutex
	// Paths this process verified, and the version each carries.
	verified map[string]string
}

// NewDownloader builds one that works under the given directory.
func NewDownloader(client *http.Client, dir string) *Downloader {
	return &Downloader{client: client, dir: dir, verified: map[string]string{}}
}

/*
Fetch downloads a release's artifact and proves where it came from.

Returns the path only when both happened. A download that cannot be verified is
deleted rather than left for somebody to find and run — a file in a temporary
directory named after this application is a thing people trust.
*/
func (d *Downloader) Fetch(ctx context.Context, release Release) (string, error) {
	artifact, err := release.ForThisPlatform()
	if err != nil {
		return "", err
	}

	if !Enabled() {
		/*
			Checked before anything is fetched. Downloading something that
			could never be verified wastes somebody's bandwidth and leaves a
			file that looks like an update on their disk.
		*/
		return "", ErrNoTrustRoot
	}

	if err := os.MkdirAll(d.dir, 0o700); err != nil {
		return "", fmt.Errorf("making room for the download: %w", err)
	}

	path, err := d.fetchArtifact(ctx, artifact)
	if err != nil {
		return "", err
	}

	bundle, err := d.fetchBundle(ctx, artifact.BundleURL)
	if err != nil {
		_ = os.Remove(path)
		return "", err
	}

	file, err := os.Open(path)
	if err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("reading the download back: %w", err)
	}
	defer file.Close()

	if err := Verify(bundle, file); err != nil {
		/*
			Deleted, not kept. An unverified download is either a mistake or an
			attack, and leaving it where somebody might run it is the worst of
			both.
		*/
		_ = os.Remove(path)
		return "", err
	}

	d.mu.Lock()
	d.verified[path] = release.Version
	d.mu.Unlock()

	return path, nil
}

/*
bundleLimit bounds a signature file.

A sigstore bundle is a few kilobytes. Without a limit, whatever answers the
request decides how much memory this uses.
*/
const bundleLimit = 4 << 20

// fetchBundle downloads the proof of where an artifact came from.
func (d *Downloader) fetchBundle(ctx context.Context, url string) ([]byte, error) {
	if url == "" {
		return nil, fmt.Errorf("%w: the release names no signature", ErrUnverified)
	}

	response, err := d.get(ctx, url)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	bundle, err := io.ReadAll(io.LimitReader(response.Body, bundleLimit))
	if err != nil {
		return nil, fmt.Errorf("reading the signature: %w", err)
	}

	return bundle, nil
}

/*
fetchArtifact downloads the file itself to a fresh path.

Bounded by the size the feed declared, plus nothing. A server that sends more
than it said is either broken or hostile, and either way this stops rather than
filling a disk.
*/
func (d *Downloader) fetchArtifact(ctx context.Context, artifact Artifact) (string, error) {
	response, err := d.get(ctx, artifact.URL)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	path := filepath.Join(d.dir, "update-"+randomName())

	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		return "", fmt.Errorf("creating the download: %w", err)
	}
	defer file.Close()

	limit := artifact.Size
	if limit <= 0 {
		// A feed that declared no size still gets a ceiling, so this cannot be
		// asked to write until the disk is full.
		limit = 512 << 20
	}

	written, err := io.Copy(file, io.LimitReader(response.Body, limit+1))
	if err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("downloading: %w", err)
	}
	if written > limit {
		_ = os.Remove(path)
		return "", ErrTooLarge
	}

	return path, nil
}

// get makes one request and refuses anything but a plain success.
func (d *Downloader) get(ctx context.Context, url string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("asking for %s: %w", url, err)
	}

	response, err := d.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}

	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("%s answered %d", url, response.StatusCode)
	}

	return response, nil
}

/*
Install puts a verified download in place of the running application.

Refuses anything this process did not verify. That is the gate: a path is not a
credential, and a file that appeared where a verified download was put is not
thereby verified.
*/
func (d *Downloader) Install(path string) error {
	d.mu.Lock()
	_, verified := d.verified[path]
	d.mu.Unlock()

	if !verified {
		return ErrNotVerified
	}

	running, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding the running application: %w", err)
	}
	running, err = filepath.EvalSymlinks(running)
	if err != nil {
		return fmt.Errorf("following the running application: %w", err)
	}

	return replace(running, path)
}

/*
replace swaps a new binary in for the running one.

By rename, which is atomic on the same filesystem: at no point is there a
partly-written application. The old one is moved aside first rather than
deleted, because a running process holds its executable open on some systems
and because an install that goes wrong should leave something to go back to.
*/
func replace(running, downloaded string) error {
	previous := running + ".previous"

	// Anything left from an earlier install. Removing it is safe: the current
	// application is running from `running`, not from this.
	_ = os.Remove(previous)

	if err := os.Rename(running, previous); err != nil {
		return fmt.Errorf("moving the current application aside: %w", err)
	}

	if err := os.Rename(downloaded, running); err != nil {
		// Put back what was there. An install that fails must not leave
		// somebody without an application at all.
		if back := os.Rename(previous, running); back != nil {
			return fmt.Errorf("the update failed and the previous application "+
				"could not be restored; it is at %s: %w", previous, err)
		}
		return fmt.Errorf("putting the update in place: %w", err)
	}

	// Executable, since a rename carries the mode of the downloaded file.
	if err := os.Chmod(running, 0o755); err != nil {
		return fmt.Errorf("making the update executable: %w", err)
	}

	return nil
}

/*
Discard removes a download and forgets it was verified.

Called when somebody declines an update. Leaving a verified binary in a
temporary directory is a thing somebody may later run without knowing what it
is.
*/
func (d *Downloader) Discard(path string) {
	d.mu.Lock()
	delete(d.verified, path)
	d.mu.Unlock()

	_ = os.Remove(path)
}

// VerifiedVersion reports which version a verified download carries.
func (d *Downloader) VerifiedVersion(path string) (string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	version, ok := d.verified[path]
	return version, ok
}

/*
randomName keeps two downloads from colliding.

Random rather than the version, because a predictable path in a shared
temporary directory is one somebody else can create first.
*/
func randomName() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		// Cannot happen on any platform this runs on; if it did, a fixed name
		// in a directory this created with 0700 is still not reachable.
		return "download"
	}
	return hex.EncodeToString(bytes[:]) + suffix()
}

// suffix is what the platform expects an executable to be called.
func suffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}
