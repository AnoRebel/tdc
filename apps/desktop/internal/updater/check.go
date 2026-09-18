package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

/*
Finding out whether there is something newer.

Checking is separate from installing, and deliberately so: knowing a newer
version exists is useful even on a machine where this cannot verify one, and
telling somebody is honest where silently doing nothing is not.
*/

// ErrNoRelease is returned when the feed holds nothing for this platform.
var ErrNoRelease = errors.New("no release for this platform")

/*
Release is what the feed says about one version.

The bundle is named rather than embedded: it is fetched alongside the artifact
and verified against it, and a feed that carried its own proof of authenticity
would be proving something about itself.
*/
type Release struct {
	// Version, as a plain semantic version without a leading v.
	Version string `json:"version"`
	// Notes is what changed, in the release's own words.
	Notes string `json:"notes,omitempty"`
	// Published is when, so an interface can say how old it is.
	Published time.Time `json:"published"`
	// Artifacts, keyed by "os/arch" — "linux/amd64", "darwin/arm64".
	Artifacts map[string]Artifact `json:"artifacts"`
}

// Artifact is one platform's download.
type Artifact struct {
	// URL of the file itself.
	URL string `json:"url"`
	// BundleURL of the sigstore bundle proving where it came from.
	BundleURL string `json:"bundleUrl"`
	// Size in bytes, so a download can be bounded and reported.
	Size int64 `json:"size"`
}

/*
ForThisPlatform returns the artifact for the machine this is running on.

An absent one is not an error in the feed: a release may not build for every
platform on the day it is cut, and somebody on that platform should be told
there is nothing for them rather than shown a download that will not run.

A build with a variant asks only for its own key and never falls back to the
bare platform. The two are not interchangeable — that is the whole reason the
variant exists — so a missing variant artifact means there is nothing this
machine can run, not that it should take the other one.
*/
func (r Release) ForThisPlatform() (Artifact, error) {
	key := PlatformKey()
	artifact, ok := r.Artifacts[key]
	if !ok {
		return Artifact{}, fmt.Errorf("%w: %s", ErrNoRelease, key)
	}
	return artifact, nil
}

/*
PlatformKey is how this build names itself to the feed.

Ordinarily the Go platform, as "linux/amd64". A build against a different
toolkit appends its variant, so it is offered its own artifact and the ordinary
build is offered the ordinary one.
*/
func PlatformKey() string {
	key := runtime.GOOS + "/" + runtime.GOARCH
	if platformVariant != "" {
		key += "-" + platformVariant
	}
	return key
}

/*
Newer reports whether one version supersedes another.

Compares the numeric parts in order, so 0.10.0 is newer than 0.9.0 — which
string comparison gets backwards, and which is exactly the version somebody
would be stuck on.

A pre-release is older than the release it leads to: 1.0.0-beta.1 precedes
1.0.0. Anything unparseable is treated as not newer, because offering an update
to a version this cannot understand is worse than offering none.
*/
func Newer(candidate, current string) bool {
	candidateParts, candidatePre, candidateOK := splitVersion(candidate)
	currentParts, currentPre, currentOK := splitVersion(current)

	if !candidateOK || !currentOK {
		return false
	}

	for i := 0; i < 3; i++ {
		if candidateParts[i] != currentParts[i] {
			return candidateParts[i] > currentParts[i]
		}
	}

	/*
		Same numbers. A release beats a pre-release of itself; between two
		pre-releases, the later string wins — which is right for the
		beta.1/beta.2 form this project uses and is not general semver
		ordering, so anything more exotic simply does not update.
	*/
	switch {
	case candidatePre == "" && currentPre != "":
		return true
	case candidatePre != "" && currentPre == "":
		return false
	default:
		return candidatePre > currentPre
	}
}

/*
splitVersion reads "1.2.3" or "1.2.3-beta.4" into numbers and a pre-release.

Reports false for anything else, so an unreadable version is never newer than
what is running.
*/
func splitVersion(version string) ([3]int, string, bool) {
	text := strings.TrimPrefix(strings.TrimSpace(version), "v")

	numbers, pre, _ := strings.Cut(text, "-")
	parts := strings.Split(numbers, ".")
	if len(parts) != 3 {
		return [3]int{}, "", false
	}

	var out [3]int
	for i, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 {
			return [3]int{}, "", false
		}
		out[i] = value
	}

	return out, pre, true
}

/*
feedLimit bounds what a feed may be.

A release feed is a short document. Without a limit, whatever answers the
request decides how much memory this uses.
*/
const feedLimit = 1 << 20

/*
Check asks the feed what the newest release is.

Returns the release and whether it is newer than what is running. A feed that
cannot be reached is an error rather than "no update": somebody who has not
been told anything should not be left believing they are up to date.
*/
func Check(ctx context.Context, client *http.Client, feedURL, currentVersion string) (Release, bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return Release{}, false, fmt.Errorf("asking about updates: %w", err)
	}
	request.Header.Set("Accept", "application/json")

	response, err := client.Do(request)
	if err != nil {
		return Release{}, false, fmt.Errorf("could not reach the update feed: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return Release{}, false, fmt.Errorf("the update feed answered %d", response.StatusCode)
	}

	var release Release
	if err := json.NewDecoder(io.LimitReader(response.Body, feedLimit)).Decode(&release); err != nil {
		return Release{}, false, fmt.Errorf("the update feed could not be read: %w", err)
	}

	return release, Newer(release.Version, currentVersion), nil
}
