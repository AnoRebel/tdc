package updater

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

/*
Which version supersedes which.

The failure this guards against is quiet: somebody stays on an old release
because a comparison said the new one was older, and nothing ever tells them.
*/
func TestWhichVersionIsNewer(t *testing.T) {
	for _, test := range []struct {
		candidate, current string
		want               bool
		why                string
	}{
		{"0.2.0", "0.1.0", true, "a later minor"},
		{"0.1.1", "0.1.0", true, "a later patch"},
		{"1.0.0", "0.9.9", true, "a later major"},
		{"0.1.0", "0.1.0", false, "the same version"},
		{"0.1.0", "0.2.0", false, "an earlier version"},

		/*
			The one string comparison gets backwards, and the one somebody
			would actually be stuck on: "0.10.0" < "0.9.0" as text.
		*/
		{"0.10.0", "0.9.0", true, "ten is after nine"},
		{"1.0.0", "0.10.0", true, "a major after a two-digit minor"},

		// A release supersedes its own pre-releases.
		{"1.0.0", "1.0.0-beta.1", true, "a release after its beta"},
		{"1.0.0-beta.1", "1.0.0", false, "a beta does not supersede its release"},
		{"1.0.0-beta.2", "1.0.0-beta.1", true, "a later beta"},

		// A leading v is how a tag is written and means the same thing.
		{"v0.2.0", "0.1.0", true, "a tag with its v"},

		/*
			Anything unreadable is not newer. Offering an update to a version
			this cannot understand is worse than offering none.
		*/
		{"", "0.1.0", false, "nothing at all"},
		{"latest", "0.1.0", false, "a word rather than a version"},
		{"0.1", "0.1.0", false, "too few parts"},
		{"0.1.0.1", "0.1.0", false, "too many parts"},
		{"0.1.x", "0.1.0", false, "a part that is not a number"},
	} {
		if got := Newer(test.candidate, test.current); got != test.want {
			t.Errorf("%s: Newer(%q, %q) = %v, want %v",
				test.why, test.candidate, test.current, got, test.want)
		}
	}
}

/*
A feed that cannot be reached is an error, not "you are up to date".

Somebody who has been told nothing should not be left believing they are
current — which is how a machine sits on a version with a known problem.
*/
func TestAnUnreachableFeedIsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	address := server.URL
	server.Close()

	_, newer, err := Check(context.Background(), server.Client(), address, "0.1.0")

	if err == nil {
		t.Error("an unreachable feed reported success")
	}
	if newer {
		t.Error("an unreachable feed reported an update")
	}
}

func TestAFeedThatAnswersBadlyIsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	if _, _, err := Check(context.Background(), server.Client(), server.URL, "0.1.0"); err == nil {
		t.Error("a feed answering 500 reported success")
	}
}

func TestAFeedThatIsNotJSONIsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("<html>not a feed</html>"))
	}))
	defer server.Close()

	if _, _, err := Check(context.Background(), server.Client(), server.URL, "0.1.0"); err == nil {
		t.Error("a feed of HTML was read as a release")
	}
}

/*
A feed longer than a feed could reasonably be is cut off.

Without a limit, whatever answers the request decides how much memory this
uses.

The document is *valid* JSON, and complete — a truncated one would fail to
decode whether or not a limit existed, which is a test that passes for the
wrong reason. This one decodes cleanly if it is read in full, so the only way
it fails is the limit doing its job.
*/
func TestAnEnormousFeedIsRefused(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Well past the limit, and syntactically complete.
		json.NewEncoder(w).Encode(Release{
			Version: "0.2.0",
			Notes:   strings.Repeat("a", feedLimit+(1<<20)),
		})
	}))
	defer server.Close()

	if _, _, err := Check(context.Background(), server.Client(), server.URL, "0.1.0"); err == nil {
		t.Error("a feed larger than the limit was read in full")
	}
}

func TestANewerReleaseIsReported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(Release{
			Version: "0.2.0",
			Notes:   "Something changed.",
			Artifacts: map[string]Artifact{
				"linux/amd64": {URL: "https://example.test/tdc", BundleURL: "https://example.test/tdc.sigstore.json"},
			},
		})
	}))
	defer server.Close()

	release, newer, err := Check(context.Background(), server.Client(), server.URL, "0.1.0")
	if err != nil {
		t.Fatalf("checking: %v", err)
	}
	if !newer {
		t.Error("0.2.0 was not reported as newer than 0.1.0")
	}
	if release.Notes == "" {
		t.Error("what changed was lost")
	}
}

/*
A platform with no build is told so, rather than shown a download that will
not run.
*/
func TestAPlatformWithNoBuildSaysSo(t *testing.T) {
	release := Release{
		Version:   "0.2.0",
		Artifacts: map[string]Artifact{"plan9/386": {URL: "https://example.test/tdc"}},
	}

	if _, err := release.ForThisPlatform(); err == nil {
		t.Error("a release with no build for this platform offered one anyway")
	}
}
