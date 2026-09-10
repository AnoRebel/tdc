package updater

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	sigroot "github.com/sigstore/sigstore-go/pkg/root"
)

/*
What may be installed, and what is left on disk afterwards.

The rules under test: nothing is installed that this process did not verify,
nothing unverified survives on disk, and a failed install leaves a working
application rather than none.
*/

/*
A path is not a credential.

The gate is what this process verified, not what exists where a verified
download was put. Otherwise anything able to write to the download directory
could have its file installed as an update.
*/
func TestAnUnverifiedFileIsNotInstalled(t *testing.T) {
	downloader := NewDownloader(http.DefaultClient, t.TempDir())

	planted := filepath.Join(downloader.dir, "update-something")
	if err := os.WriteFile(planted, []byte("not really an update"), 0o700); err != nil {
		t.Fatalf("planting a file: %v", err)
	}

	if err := downloader.Install(planted); !errors.Is(err, ErrNotVerified) {
		t.Errorf("installing a file nobody verified gave %v", err)
	}
}

/*
And nothing is fetched at all when nothing could be verified.

Downloading something that could never be checked wastes bandwidth and leaves
a file that looks like an update on somebody's disk.
*/
func TestNothingIsFetchedWithoutATrustRoot(t *testing.T) {
	SetTrustedRoot(nil)

	var asked bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked = true
		w.Write([]byte("an update"))
	}))
	defer server.Close()

	downloader := NewDownloader(server.Client(), t.TempDir())
	release := Release{
		Version: "0.2.0",
		Artifacts: map[string]Artifact{
			platform(): {URL: server.URL + "/tdc", BundleURL: server.URL + "/tdc.sigstore.json"},
		},
	}

	if _, err := downloader.Fetch(context.Background(), release); !errors.Is(err, ErrNoTrustRoot) {
		t.Errorf("fetching without a trust root gave %v", err)
	}
	if asked {
		t.Error("something was downloaded that could never have been verified")
	}
}

/*
A download that fails verification is deleted.

Left where it is, it is a file in a directory named after this application
that somebody may later run. That is the worst outcome of the three.
*/
func TestAnUnverifiableDownloadIsDeleted(t *testing.T) {
	// A trust root that is real enough to get past the enabled check, so the
	// download happens and then fails to verify.
	SetTrustedRoot(nil)
	setTrustedMaterial(unusableTrust{})
	t.Cleanup(func() { SetTrustedRoot(nil) })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sigstore.json") {
			w.Write([]byte(`{"not":"a bundle"}`))
			return
		}
		w.Write([]byte("bytes pretending to be an update"))
	}))
	defer server.Close()

	dir := t.TempDir()
	downloader := NewDownloader(server.Client(), dir)

	release := Release{
		Version: "0.2.0",
		Artifacts: map[string]Artifact{
			platform(): {URL: server.URL + "/tdc", BundleURL: server.URL + "/tdc.sigstore.json"},
		},
	}

	if _, err := downloader.Fetch(context.Background(), release); err == nil {
		t.Fatal("an unverifiable download was accepted")
	}

	left, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading the download directory: %v", err)
	}
	if len(left) != 0 {
		t.Errorf("%d file(s) were left behind after verification failed", len(left))
	}
}

/*
A release naming no signature is refused before anything is downloaded twice.

A feed that offers an artifact and no bundle is either misconfigured or
malicious, and there is nothing to check either way.
*/
func TestAReleaseWithNoSignatureIsRefused(t *testing.T) {
	setTrustedMaterial(unusableTrust{})
	t.Cleanup(func() { SetTrustedRoot(nil) })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("an update"))
	}))
	defer server.Close()

	downloader := NewDownloader(server.Client(), t.TempDir())
	release := Release{
		Version:   "0.2.0",
		Artifacts: map[string]Artifact{platform(): {URL: server.URL + "/tdc"}},
	}

	if _, err := downloader.Fetch(context.Background(), release); !errors.Is(err, ErrUnverified) {
		t.Errorf("a release with no signature gave %v", err)
	}
}

/*
A server sending more than it declared is stopped.

Either broken or hostile, and either way this should not fill somebody's disk
finding out which.
*/
func TestADownloadLargerThanDeclaredIsRefused(t *testing.T) {
	setTrustedMaterial(unusableTrust{})
	t.Cleanup(func() { SetTrustedRoot(nil) })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sigstore.json") {
			w.Write([]byte("{}"))
			return
		}
		w.Write([]byte(strings.Repeat("a", 4096)))
	}))
	defer server.Close()

	dir := t.TempDir()
	downloader := NewDownloader(server.Client(), dir)

	release := Release{
		Version: "0.2.0",
		Artifacts: map[string]Artifact{
			// Declared as tiny, sent as four kilobytes.
			platform(): {URL: server.URL + "/tdc", BundleURL: server.URL + "/tdc.sigstore.json", Size: 16},
		},
	}

	if _, err := downloader.Fetch(context.Background(), release); !errors.Is(err, ErrTooLarge) {
		t.Errorf("a download larger than declared gave %v", err)
	}

	left, _ := os.ReadDir(dir)
	if len(left) != 0 {
		t.Errorf("%d file(s) were left behind after an oversized download", len(left))
	}
}

/*
Replacing the running application keeps the old one.

An install that goes wrong should leave something to go back to, and on some
systems a running process holds its own executable open.
*/
func TestReplacingKeepsThePreviousApplication(t *testing.T) {
	dir := t.TempDir()
	running := filepath.Join(dir, "tdc")
	downloaded := filepath.Join(dir, "update")

	if err := os.WriteFile(running, []byte("the old application"), 0o755); err != nil {
		t.Fatalf("writing: %v", err)
	}
	if err := os.WriteFile(downloaded, []byte("the new application"), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}

	if err := replace(running, downloaded); err != nil {
		t.Fatalf("replacing: %v", err)
	}

	now, err := os.ReadFile(running)
	if err != nil || string(now) != "the new application" {
		t.Errorf("the running path holds %q", string(now))
	}

	previous, err := os.ReadFile(running + ".previous")
	if err != nil || string(previous) != "the old application" {
		t.Errorf("the previous application was not kept: %v", err)
	}
}

/*
And what replaces it is executable.

A rename carries the downloaded file's mode, which is not necessarily one that
can be run — an update that installs and then will not start is worse than one
that refused.
*/
func TestWhatIsInstalledCanBeRun(t *testing.T) {
	dir := t.TempDir()
	running := filepath.Join(dir, "tdc")
	downloaded := filepath.Join(dir, "update")

	_ = os.WriteFile(running, []byte("old"), 0o755)
	// Deliberately not executable, as a downloaded file may be.
	_ = os.WriteFile(downloaded, []byte("new"), 0o600)

	if err := replace(running, downloaded); err != nil {
		t.Fatalf("replacing: %v", err)
	}

	info, err := os.Stat(running)
	if err != nil {
		t.Fatalf("looking at what was installed: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("what was installed has mode %v and cannot be run", info.Mode().Perm())
	}
}

/*
Declining an update leaves nothing behind.

A verified binary in a temporary directory is a thing somebody may later run
without knowing what it is.
*/
func TestDecliningAnUpdateRemovesIt(t *testing.T) {
	dir := t.TempDir()
	downloader := NewDownloader(http.DefaultClient, dir)

	path := filepath.Join(dir, "update-abc")
	_ = os.WriteFile(path, []byte("an update"), 0o700)
	downloader.mu.Lock()
	downloader.verified[path] = "0.2.0"
	downloader.mu.Unlock()

	downloader.Discard(path)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("a declined update was left on disk")
	}
	if _, ok := downloader.VerifiedVersion(path); ok {
		t.Error("a declined update is still remembered as verified")
	}
}

// The platform this test is running on, as a feed would key it.
func platform() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

/*
Trusted material that exists but can verify nothing.

Enough to get past the "are updates enabled" check, so the tests that need a
download to happen and then fail verification can happen at all.
*/
type unusableTrust struct{}

func (unusableTrust) TimestampingAuthorities() []sigroot.TimestampingAuthority     { return nil }
func (unusableTrust) FulcioCertificateAuthorities() []sigroot.CertificateAuthority { return nil }
func (unusableTrust) RekorLogs() map[string]*sigroot.TransparencyLog               { return nil }
func (unusableTrust) CTLogs() map[string]*sigroot.TransparencyLog                  { return nil }
func (unusableTrust) PublicKeyVerifier(string) (sigroot.TimeConstrainedVerifier, error) {
	return nil, errors.New("no keys")
}
