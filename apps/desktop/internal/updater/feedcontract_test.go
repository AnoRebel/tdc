package updater

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

/*
The feed the release workflow writes is the feed this code reads.

Two things describe one document, in two languages: the workflow generates it
in Python, and this parses it into Go structs. Nothing makes them agree, and a
mismatch is invisible until a release is cut and every client decides there is
nothing newer — or worse, cannot tell.

testdata/workflow-feed.json is the workflow's own output, produced by running
its generator against a directory of artifacts. Regenerate it if the workflow's
shape changes, and this will say whether the client can still read it.
*/
func TestTheReleaseWorkflowsFeedIsReadable(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "workflow-feed.json"))
	if err != nil {
		t.Fatalf("reading the sample feed: %v", err)
	}

	var release Release
	if err := json.Unmarshal(raw, &release); err != nil {
		t.Fatalf("the workflow writes a feed this cannot parse: %v", err)
	}

	if release.Version == "" {
		t.Error("no version, so nothing can be compared against what is installed")
	}
	if release.Published.IsZero() {
		t.Error("published did not parse as a time, so an interface cannot say how old this is")
	}

	/*
		Every platform the workflow builds for. A release that silently stopped
		producing one of these would leave those people on an old version with
		nothing saying why.
	*/
	expected := map[string]bool{
		"linux/amd64": true, "linux/arm64": true,
		"darwin/amd64": true, "darwin/arm64": true,
		"windows/amd64": true, "windows/arm64": true,
	}

	for platform := range expected {
		artifact, ok := release.Artifacts[platform]
		if !ok {
			t.Errorf("no artifact for %s", platform)
			continue
		}
		if artifact.URL == "" {
			t.Errorf("%s: nothing to download", platform)
		}
		// No bundle means no proof, and this client installs nothing it cannot
		// prove came from the release workflow.
		if artifact.BundleURL == "" {
			t.Errorf("%s: no sigstore bundle, so it could never be installed", platform)
		}
		if artifact.Size == 0 {
			t.Errorf("%s: no size, so a download cannot be bounded", platform)
		}

	}

	/*
		And nothing else is in it.

		The release carries the server's tarballs in the same directory, named
		alike enough that a generator matching too loosely picks them up. They do
		not land on the six keys above — a tarball becomes "linux/amd64.tar.gz",
		which no client asks for — so a loop over expected platforms cannot see
		them. What catches it is looking at everything the feed contains.

		Harmless-looking, and not: ForThisPlatform keys on GOOS/GOARCH, so the
		day one of these names happens to match is the day a desktop client
		downloads a server, verifies it, and tries to run it as itself.
	*/
	for platform, artifact := range release.Artifacts {
		if strings.Contains(artifact.URL, "tdc-server-") {
			t.Errorf("the feed carries a server artifact under %q: %s", platform, artifact.URL)
		}
		if !expected[platform] {
			t.Errorf("the feed carries %q, which is not a platform this client runs on", platform)
		}
	}
}

/*
And the artifact chosen is the one for the machine asking.

ForThisPlatform keys on runtime.GOOS and GOARCH, so a feed whose keys are
shaped differently — "linux_amd64", "Linux/AMD64" — would parse perfectly and
then find nothing for anybody.
*/
func TestThePlatformKeysMatchWhatIsAskedFor(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "workflow-feed.json"))
	if err != nil {
		t.Fatalf("reading the sample feed: %v", err)
	}

	var release Release
	if err := json.Unmarshal(raw, &release); err != nil {
		t.Fatalf("parsing: %v", err)
	}

	// Whatever this test is running on must be found, which on CI and on a
	// development machine is one of the platforms the workflow builds.
	if _, err := release.ForThisPlatform(); err != nil {
		t.Errorf("the feed has nothing for the machine running this: %v", err)
	}
}
