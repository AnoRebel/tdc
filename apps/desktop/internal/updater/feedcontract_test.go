package updater

import (
	"encoding/json"
	"errors"
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
		// The legacy toolkit build, under a key of its own. See
		// TestTheLegacyBuildIsNeverOfferedTheOrdinaryArtifact below for why
		// that key must not be "linux/amd64".
		"linux/amd64-gtk3": true,
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

/*
The legacy build is never offered the ordinary Linux artifact.

Both run on linux/amd64 as far as the Go runtime is concerned, so keying on
GOOS/GOARCH alone hands the GTK4 binary to the machine that needed the legacy
one — precisely the machine without GTK4, where it will not start. The whole
reason the variant is built is that the two are not interchangeable.

PlatformKey is what keeps them apart, and it is compiled per build tag, so this
checks the mapping rather than the constant: given the key a gtk3 build asks
for, the feed must answer with the gtk3 artifact and nothing else.
*/
func TestTheLegacyBuildIsNeverOfferedTheOrdinaryArtifact(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "workflow-feed.json"))
	if err != nil {
		t.Fatalf("reading the sample feed: %v", err)
	}

	var release Release
	if err := json.Unmarshal(raw, &release); err != nil {
		t.Fatalf("parsing: %v", err)
	}

	ordinary, ok := release.Artifacts["linux/amd64"]
	if !ok {
		t.Fatal("no ordinary linux/amd64 artifact, so this proves nothing")
	}
	legacy, ok := release.Artifacts["linux/amd64-gtk3"]
	if !ok {
		t.Fatal("the feed carries no legacy artifact, so a machine without GTK4 has nothing to update to")
	}

	if legacy.URL == ordinary.URL {
		t.Errorf("both keys point at one binary (%s), so one of the two machines gets a build it cannot run", legacy.URL)
	}
	if !strings.HasSuffix(legacy.URL, "-gtk3") {
		t.Errorf("the legacy key points at %q, which is not the legacy build", legacy.URL)
	}

	/*
		And a feed that stopped publishing the legacy build offers that machine
		nothing, rather than quietly falling back to the ordinary one. Absent is
		the honest answer: there is no newer version it can run.

		ForThisPlatform asks for whichever key this test binary was built for, so
		the way to check the refusal on either build is to hand it a feed missing
		that key and require an error. A fallback to the bare platform would make
		this pass on the ordinary build and fail here.
	*/
	withoutMine := Release{Version: release.Version, Artifacts: map[string]Artifact{}}
	for key, artifact := range release.Artifacts {
		if key != PlatformKey() {
			withoutMine.Artifacts[key] = artifact
		}
	}
	if _, err := withoutMine.ForThisPlatform(); !errors.Is(err, ErrNoRelease) {
		t.Errorf("a feed carrying every platform but %s offered an update anyway: %v", PlatformKey(), err)
	}
}
