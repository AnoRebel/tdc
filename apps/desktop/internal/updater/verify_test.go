package updater

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/sigstore/sigstore-go/pkg/testing/ca"
	sigverify "github.com/sigstore/sigstore-go/pkg/verify"
)

/*
What may be installed, and what must not.

An updater that installs something it cannot verify is worse than no updater at
all, so the cases that matter here are the refusals. Verification runs against
an in-process signing authority rather than a real release, because a test that
could only run after a release is a test that never runs before one.
*/

const testVersion = "1.2.3"

// The identity a real release of testVersion is signed with.
const testIdentity = releaseWorkflow + testVersion

// signedBy signs an artifact as identity, under an authority this test trusts.
func signedBy(t *testing.T, identity string, artifact []byte) sigverify.SignedEntity {
	t.Helper()
	authority, err := ca.NewVirtualSigstore()
	if err != nil {
		t.Fatalf("building a signing authority: %v", err)
	}
	setTrustedMaterial(authority)
	t.Cleanup(func() { setTrustedMaterial(nil) })
	entity, err := authority.Sign(identity, certIdentityIssuer, artifact)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	return entity
}

/*
The ordinary case: a release signed by the release workflow at its tag gives
the digest Wails then holds the download to. Worth having so the refusals below
mean something — a verifier that refused everything would pass all of them.
*/
func TestAReleaseSignedAtItsTagGivesItsDigest(t *testing.T) {
	artifact := []byte("the bytes of a release")
	entity := signedBy(t, testIdentity, artifact)
	trust, _ := trustedMaterial()

	digest, err := digestOf(trust, entity, testVersion)
	if err != nil {
		t.Fatalf("a properly signed release was refused: %v", err)
	}
	if want := sha256.Sum256(artifact); !bytes.Equal(digest, want[:]) {
		t.Error("the digest given is not the signed artifact's")
	}
}

// The artifact that was signed is the artifact that was checked.
func TestADifferentArtifactIsRefused(t *testing.T) {
	entity := signedBy(t, testIdentity, []byte("the real release"))
	trust, _ := trustedMaterial()
	err := verifyEntity(trust, entity, sigverify.WithArtifact(bytes.NewReader([]byte("something else"))), testVersion)
	if !errors.Is(err, ErrUnverified) {
		t.Errorf("a signature over one file admitted another: %v", err)
	}
}

/*
Only the release workflow, run for this version's tag, signs this version.
Anybody can make a valid sigstore signature; other workflows in this repository
can too; and an old release's signature names the old tag, so a feed offering
it as newer is refused.
*/
func TestOnlyTheReleaseWorkflowAtThisTagIsAccepted(t *testing.T) {
	for name, identity := range map[string]string{
		"another project":                  "https://github.com/somebody-else/their-app/.github/workflows/release.yml@refs/tags/v" + testVersion,
		"a name merely containing ours":    "https://github.com/AnoRebel/tdc-not-really/.github/workflows/release.yml@refs/tags/v" + testVersion,
		"another workflow here":            "https://github.com/AnoRebel/tdc/.github/workflows/ci.yml@refs/tags/v" + testVersion,
		"the release workflow on a branch": "https://github.com/AnoRebel/tdc/.github/workflows/release.yml@refs/heads/main",
		"an older release":                 releaseWorkflow + "1.2.2",
		"a version sharing a prefix":       releaseWorkflow + testVersion + "9",
	} {
		t.Run(name, func(t *testing.T) {
			entity := signedBy(t, identity, []byte("a release"))
			trust, _ := trustedMaterial()
			if _, err := digestOf(trust, entity, testVersion); !errors.Is(err, ErrUnverified) {
				t.Errorf("signed by %s, it was accepted as %s: %v", identity, testVersion, err)
			}
		})
	}
}

// A bundle that is not one is a refusal, not a crash.
func TestAMalformedBundleIsRefused(t *testing.T) {
	signedBy(t, testIdentity, []byte("a release"))
	for _, bundle := range [][]byte{nil, []byte(""), []byte("not json"), []byte("{}")} {
		if _, err := VerifiedDigest(bundle, testVersion); !errors.Is(err, ErrUnverified) {
			t.Errorf("a bundle of %q gave %v", string(bundle), err)
		}
	}
}
