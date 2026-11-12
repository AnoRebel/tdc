package updater

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/AnoRebel/tdc/apps/desktop/internal/buildinfo"
	wails "github.com/wailsapp/wails/v3/pkg/updater"
)

/*
Who may replace themselves: an AppImage, a .app bundle, or a Windows
executable, each somewhere writable. Everybody else — a distribution's
package, a plain binary — is told of updates and given the release page.
*/
func TestOnlyCopiesThatCanBeReplacedUpdateThemselves(t *testing.T) {
	writable := t.TempDir()
	readOnly := t.TempDir()
	if err := os.Chmod(readOnly, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0o700) })
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}

	for _, c := range []struct {
		name, goos, exe, appImage string
		want                      bool
		why                       string
	}{
		{"an AppImage", "linux", "/tmp/.mount_x/usr/bin/tdc", filepath.Join(writable, "TDC.AppImage"), true, ""},
		{"a package or plain binary", "linux", filepath.Join(writable, "tdc"), "", false, "AppImage"},
		{"an AppImage nobody may replace", "linux", "/tmp/.mount_x/usr/bin/tdc", filepath.Join(readOnly, "TDC.AppImage"), false, "not writable"},
		{"a .app bundle", "darwin", filepath.Join(writable, "TDC.app/Contents/MacOS/tdc"), "", true, ""},
		{"a .app nobody may replace", "darwin", filepath.Join(readOnly, "TDC.app/Contents/MacOS/tdc"), "", false, "not writable"},
		{"a binary outside a bundle", "darwin", filepath.Join(writable, "tdc"), "", false, ".app bundle"},
		{"a Windows executable", "windows", filepath.Join(writable, "tdc.exe"), "", true, ""},
		{"one under Program Files", "windows", filepath.Join(readOnly, "tdc.exe"), "", false, "not writable"},
	} {
		t.Run(c.name, func(t *testing.T) {
			ok, why := Eligibility(c.goos, c.exe, c.appImage)
			if ok != c.want || (!ok && !strings.Contains(why, c.why)) {
				t.Errorf("Eligibility = %v %q, want %v mentioning %q", ok, why, c.want, c.why)
			}
		})
	}
	if entries, _ := os.ReadDir(writable); len(entries) != 0 {
		t.Errorf("the probe left %d files behind", len(entries))
	}
}

type stubHost struct{}

func (stubHost) Emit(string, ...any) bool                          { return true }
func (stubHost) OnEvent(string, func(any)) func()                  { return func() {} }
func (stubHost) OpenWindow(wails.WindowOptions) wails.WindowHandle { return nil }
func (stubHost) Quit()                                             {}

func newKey(t *testing.T) (ed25519.PrivateKey, []byte) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKIXPublicKey(public)
	return private, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

/*
release serves a manifest offering version for this platform, as
"wails3 updater manifest" writes one: signed (or not) over what was signed,
serving what is served, counting downloads.
*/
func release(t *testing.T, key ed25519.PrivateKey, version string, signed, served []byte, sign bool) (string, *atomic.Int32) {
	t.Helper()
	sum := sha512.Sum512(signed)
	artifact := map[string]any{
		"url": "/artifact", "platform": runtime.GOOS, "arch": runtime.GOARCH,
		"digestAlgo": "sha512", "digest": base64.StdEncoding.EncodeToString(sum[:]),
	}
	if sign {
		signature, err := key.Sign(nil, sum[:], &ed25519.Options{Hash: crypto.SHA512})
		if err != nil {
			t.Fatal(err)
		}
		artifact["signatureAlgo"] = "ed25519ph"
		artifact["signature"] = base64.StdEncoding.EncodeToString(signature)
	}
	manifest, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "version": version, "notes": "what changed", "artifacts": []any{artifact},
	})
	downloads := &atomic.Int32{}
	mux := http.NewServeMux()
	mux.HandleFunc("/update-manifest.json", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(manifest) })
	mux.HandleFunc("/artifact", func(w http.ResponseWriter, _ *http.Request) {
		downloads.Add(1)
		_, _ = w.Write(served)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server.URL + "/update-manifest.json", downloads
}

func service(t *testing.T, manifest string, publicKey []byte, self bool) *Service {
	t.Helper()
	s := NewService(manifest, publicKey, filepath.Join(t.TempDir(), "updates.json"))
	s.Eligibility = func() (bool, string) { return self, "not running from an AppImage" }
	if err := s.Use(wails.New(stubHost{})); err != nil {
		t.Fatalf("Use: %v", err)
	}
	return s
}

func TestANewerReleaseIsReportedAndTheSameIsNot(t *testing.T) {
	key, public := newKey(t)
	newer, _ := release(t, key, "2.0.0", []byte("x"), []byte("x"), true)
	if state := service(t, newer, public, true).CheckForUpdate(context.Background()); state.Available != "2.0.0" || state.Notes != "what changed" {
		t.Errorf("a newer release gave %+v", state)
	}
	same, _ := release(t, key, buildinfo.Version, []byte("x"), []byte("x"), true)
	if state := service(t, same, public, true).CheckForUpdate(context.Background()); state.Available != "" || state.Problem != "" {
		t.Errorf("the running version gave %+v", state)
	}
}

// A release without a manifest is a failed check, not "up to date".
func TestAMissingManifestIsAProblem(t *testing.T) {
	_, public := newKey(t)
	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)
	if state := service(t, server.URL+"/update-manifest.json", public, true).CheckForUpdate(context.Background()); state.Problem == "" {
		t.Errorf("a missing manifest gave %+v", state)
	}
}

/*
Nothing is installed that this project's key did not sign: not an unsigned
entry, not bytes other than those signed, not something signed with another
key.
*/
func TestOnlyWhatOurKeySignedIsInstalled(t *testing.T) {
	key, public := newKey(t)
	other, _ := newKey(t)
	payload := []byte("the new version")
	for name, manifest := range map[string]string{
		"unsigned":         first(release(t, key, "2.0.0", payload, payload, false)),
		"tampered with":    first(release(t, key, "2.0.0", payload, []byte("something else"), true)),
		"signed elsewhere": first(release(t, other, "2.0.0", payload, payload, true)),
	} {
		t.Run(name, func(t *testing.T) {
			s := service(t, manifest, public, true)
			s.CheckForUpdate(context.Background())
			if state := s.DownloadUpdate(context.Background()); state.Ready || state.Problem == "" {
				t.Errorf("an update %s was accepted: %+v", name, state)
			}
		})
	}

	s := service(t, first(release(t, key, "2.0.0", payload, payload, true)), public, true)
	s.CheckForUpdate(context.Background())
	if state := s.DownloadUpdate(context.Background()); !state.Ready {
		t.Errorf("a properly signed update was refused: %+v", state)
	}
}

// A copy that cannot replace itself is told, and downloads nothing.
func TestACopyThatCannotUpdateItselfDownloadsNothing(t *testing.T) {
	key, public := newKey(t)
	payload := []byte("the new version")
	manifest, downloads := release(t, key, "2.0.0", payload, payload, true)
	s := service(t, manifest, public, false)

	state := s.CheckForUpdate(context.Background())
	if state.Available != "2.0.0" || state.SelfUpdate || state.NotSelf == "" {
		t.Fatalf("checking gave %+v", state)
	}
	if state := s.DownloadUpdate(context.Background()); state.Ready || !strings.Contains(state.Problem, "releases page") {
		t.Errorf("downloading gave %+v", state)
	}
	if err := s.InstallUpdate(context.Background()); err == nil {
		t.Error("installing was not refused")
	}
	if n := downloads.Load(); n != 0 {
		t.Errorf("downloaded %d times", n)
	}
}

func first[T any](value T, _ ...any) T { return value }
