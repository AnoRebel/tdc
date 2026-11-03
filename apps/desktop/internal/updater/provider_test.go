package updater

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	wails "github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

// A release's assets as the release workflow publishes them.
func releaseAssets(version string) []github.ReleaseAsset {
	var assets []github.ReleaseAsset
	for _, name := range []string{
		"SHA256SUMS",
		"tdc-server-" + version + "-linux-amd64.tar.gz",
		"tdc-" + version + "-linux-amd64-gtk3",
		"tdc-" + version + "-linux-amd64-gtk3.sigstore.json",
		"tdc-" + version + "-linux-amd64",
		"tdc-" + version + "-linux-amd64.sigstore.json",
		"tdc-" + version + "-linux-arm64",
		"tdc-" + version + "-darwin-arm64",
		"tdc-" + version + "-windows-amd64.exe",
	} {
		assets = append(assets, github.ReleaseAsset{Name: name})
	}
	return assets
}

/*
This build is offered its own artifact, and only that: not its signature, not
the server's archive, and not the other toolkit's build for the same machine,
which would not run.
*/
func TestThisBuildIsOfferedItsOwnArtifact(t *testing.T) {
	assets := releaseAssets("1.2.3-dev")
	index := matchThisBuild(wails.CheckRequest{}, assets)
	if index < 0 {
		t.Fatalf("nothing matched %s", PlatformKey())
	}
	want := "tdc-1.2.3-dev-" + strings.ReplaceAll(PlatformKey(), "/", "-") + executableSuffix()
	if got := assets[index].Name; got != want {
		t.Errorf("offered %s, want %s", got, want)
	}
}

// A release without this build's artifact offers nothing, not a neighbour's.
func TestABuildWithNoArtifactIsOfferedNothing(t *testing.T) {
	var others []github.ReleaseAsset
	own := "-" + strings.ReplaceAll(PlatformKey(), "/", "-") + executableSuffix()
	for _, asset := range releaseAssets("1.2.3") {
		if !strings.HasSuffix(asset.Name, own) {
			others = append(others, asset)
		}
	}
	if index := matchThisBuild(wails.CheckRequest{}, others); index >= 0 {
		t.Errorf("offered %s to %s", others[index].Name, PlatformKey())
	}
}

type stubSource struct{ release *wails.Release }

func (s stubSource) Name() string { return "stub" }
func (s stubSource) Check(context.Context, wails.CheckRequest) (*wails.Release, error) {
	return s.release, nil
}
func (s stubSource) Download(context.Context, *wails.Release, io.Writer, func(int64, int64)) error {
	return nil
}

/*
A release whose signature cannot be had, or does not hold, is not offered —
whatever digest its source claimed for it.
*/
func TestAReleaseWithoutAGoodSignatureIsNotOffered(t *testing.T) {
	signedBy(t, testIdentity, []byte("a release"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/garbled.sigstore.json") {
			_, _ = w.Write([]byte("not a bundle"))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	for _, name := range []string{"missing", "garbled"} {
		source := &signed{
			Provider: stubSource{release: &wails.Release{
				Version:      testVersion,
				Artifact:     wails.Artifact{Filename: name},
				Verification: &wails.Verification{DigestAlgo: "sha256", Digest: make([]byte, 32)},
			}},
			client:    server.Client(),
			bundleURL: func(r *wails.Release) string { return server.URL + "/" + r.Artifact.Filename + ".sigstore.json" },
		}
		if release, err := source.Check(context.Background(), wails.CheckRequest{}); release != nil || !errors.Is(err, ErrUnverified) {
			t.Errorf("a %s signature gave %v, %v", name, release, err)
		}
	}
}

// Up to date is up to date: nothing is fetched or verified.
func TestNothingNewerFetchesNothing(t *testing.T) {
	source := &signed{
		Provider:  stubSource{},
		client:    http.DefaultClient,
		bundleURL: func(*wails.Release) string { t.Fatal("fetched a signature for nothing"); return "" },
	}
	if release, err := source.Check(context.Background(), wails.CheckRequest{}); release != nil || err != nil {
		t.Errorf("nothing newer gave %v, %v", release, err)
	}
}
