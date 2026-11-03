package updater

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode"

	wails "github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

/*
signed puts sigstore in front of any of Wails' update sources.

Wails' own signatures need a key kept secret by whoever releases; ours are
keyless, bound to the release workflow instead. So the source finds the
release, the bundle beside its artifact is checked here, and the digest the
bundle vouches for is what Wails holds the download to. It refuses bytes that
do not match, swaps the application, and restarts it.
*/
type signed struct {
	wails.Provider
	client *http.Client
	// bundleURL is where the sigstore bundle for a release's artifact is.
	bundleURL func(*wails.Release) string
}

// Check returns the newer release for this machine, already proven ours.
func (s *signed) Check(ctx context.Context, request wails.CheckRequest) (*wails.Release, error) {
	release, err := s.Provider.Check(ctx, request)
	if err != nil || release == nil {
		return release, err
	}
	bundle, err := s.fetch(ctx, s.bundleURL(release))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnverified, err)
	}
	digest, err := VerifiedDigest(bundle, release.Version)
	if err != nil {
		return nil, err
	}
	// Replacing whatever the source said: only the signature's word counts.
	release.Verification = &wails.Verification{DigestAlgo: "sha256", Digest: digest}
	return release, nil
}

/*
bundleLimit bounds a signature file. A sigstore bundle is a few kilobytes;
without a limit, whatever answers decides how much memory this uses.
*/
const bundleLimit = 4 << 20

func (s *signed) fetch(ctx context.Context, url string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := s.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the signature answered %d", response.StatusCode)
	}
	return io.ReadAll(io.LimitReader(response.Body, bundleLimit))
}

/*
releasesOf is a repository's GitHub releases, signed.

The repository is passed in so a fork can update from its own releases, and is
refused unless signed by this project's release workflow — so pointing it
elsewhere yields refusals rather than somebody else's application.
*/
func releasesOf(repository string, client *http.Client) (wails.Provider, error) {
	source, err := github.New(github.Config{
		Repository:   repository,
		AssetMatcher: matchThisBuild,
		HTTPClient:   client,
	})
	if err != nil {
		return nil, err
	}
	return &signed{
		Provider: source,
		client:   client,
		bundleURL: func(release *wails.Release) string {
			return "https://github.com/" + repository + "/releases/download/v" +
				release.Version + "/" + release.Artifact.Filename + ".sigstore.json"
		},
	}, nil
}

/*
matchThisBuild picks this build's artifact: tdc-<version>-<os>-<arch>, with the
toolkit variant where the build has one, and .exe on Windows.

Matched whole, so the GTK3 build is never offered the ordinary one or the other
way round, and the server's archives, the checksums and the signatures — all in
the same release — are never taken for the application.
*/
func matchThisBuild(_ wails.CheckRequest, assets []github.ReleaseAsset) int {
	suffix := "-" + strings.ReplaceAll(PlatformKey(), "/", "-") + executableSuffix()
	for i, asset := range assets {
		version, ok := strings.CutPrefix(asset.Name, "tdc-")
		if !ok || version == "" || !unicode.IsDigit(rune(version[0])) {
			continue
		}
		if strings.HasSuffix(asset.Name, suffix) {
			return i
		}
	}
	return -1
}
