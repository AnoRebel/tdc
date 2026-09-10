package updater

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/sigstore/sigstore-go/pkg/testing/ca"
)

/*
What may be installed, and what must not.

An updater that installs something it cannot verify is worse than no updater at
all: it is a mechanism for replacing a running application with whatever
answered the request, wearing the appearance of a security feature. So the
cases that matter here are the refusals.

Verification runs against an in-process signing authority rather than a real
release, because a test that could only run after a release is a test that
never runs before one.
*/

// The identity a real release would be signed with.
const (
	testIdentity = "https://github.com/AnoRebel/tdc/.github/workflows/release.yml@refs/heads/main"
	testIssuer   = certIdentityIssuer
)

/** Points verification at a signing authority that exists only in this test. */
func trustThis(t *testing.T, authority *ca.VirtualSigstore) {
	t.Helper()
	setTrustedMaterial(authority)
	t.Cleanup(func() { SetTrustedRoot(nil) })
}

/*
With no trust root, nothing is installed and nothing is pretended.

This is the state every build ships in until a release pipeline exists, and it
has to be a refusal rather than a silent pass — otherwise the first real
release would be the first time anybody discovered verification never ran.
*/
func TestWithoutATrustRootNothingIsVerified(t *testing.T) {
	SetTrustedRoot(nil)

	if Enabled() {
		t.Error("updates report themselves enabled with no trust root")
	}

	err := Verify([]byte("{}"), strings.NewReader("an artifact"))
	if !errors.Is(err, ErrNoTrustRoot) {
		t.Errorf("verifying without a trust root gave %v", err)
	}
}

// A trust root that cannot be parsed is no trust root.
func TestAnUnparseableTrustRootDisablesUpdates(t *testing.T) {
	SetTrustedRoot([]byte("this is not a sigstore trusted root"))
	t.Cleanup(func() { SetTrustedRoot(nil) })

	if Enabled() {
		t.Error("a trust root that cannot be read was accepted")
	}
}

/*
The ordinary case: an artifact signed by the release workflow verifies.

Worth having so the refusals below mean something — a verifier that refused
everything would pass every other test in this file.
*/
func TestAnArtifactSignedByTheReleaseWorkflowVerifies(t *testing.T) {
	authority, err := ca.NewVirtualSigstore()
	if err != nil {
		t.Fatalf("building a signing authority: %v", err)
	}
	trustThis(t, authority)

	artifact := []byte("the bytes of a release")
	entity, err := authority.Sign(testIdentity, testIssuer, artifact)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	trust, ok := trustedMaterial()
	if !ok {
		t.Fatal("the trusted material went missing")
	}

	if err := verifyEntity(trust, entity, bytes.NewReader(artifact)); err != nil {
		t.Errorf("a properly signed artifact was refused: %v", err)
	}
}

/*
The artifact that was signed is the artifact that was checked.

Otherwise a valid signature over one file would admit any other — which is the
whole attack this exists to stop.
*/
func TestADifferentArtifactIsRefused(t *testing.T) {
	authority, err := ca.NewVirtualSigstore()
	if err != nil {
		t.Fatalf("building a signing authority: %v", err)
	}
	trustThis(t, authority)

	entity, err := authority.Sign(testIdentity, testIssuer, []byte("the real release"))
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	trust, _ := trustedMaterial()

	err = verifyEntity(trust, entity, strings.NewReader("something else entirely"))
	if err == nil {
		t.Fatal("a signature over one file admitted another")
	}
	if !errors.Is(err, ErrUnverified) {
		t.Errorf("the refusal was %v", err)
	}
}

/*
A signature from somebody else's repository is not a TDC release.

Keyless signing binds a signature to who produced it. Without the identity
check any valid sigstore signature would pass — and anybody can make one.
*/
func TestASignatureFromAnotherProjectIsRefused(t *testing.T) {
	authority, err := ca.NewVirtualSigstore()
	if err != nil {
		t.Fatalf("building a signing authority: %v", err)
	}
	trustThis(t, authority)

	artifact := []byte("a release")
	entity, err := authority.Sign(
		"https://github.com/somebody-else/their-app/.github/workflows/release.yml@refs/heads/main",
		testIssuer, artifact)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	trust, _ := trustedMaterial()

	if err := verifyEntity(trust, entity, bytes.NewReader(artifact)); err == nil {
		t.Error("a signature from another project was accepted as a TDC release")
	}
}

/*
And a repository whose name merely contains ours is not ours.

The reason the pattern is anchored: AnoRebel/tdc-not-really contains
"AnoRebel/tdc", and an unanchored match would have taken it.
*/
func TestARepositoryThatMerelyLooksLikeOursIsRefused(t *testing.T) {
	authority, err := ca.NewVirtualSigstore()
	if err != nil {
		t.Fatalf("building a signing authority: %v", err)
	}
	trustThis(t, authority)

	artifact := []byte("a release")
	entity, err := authority.Sign(
		"https://github.com/AnoRebel/tdc-not-really/.github/workflows/release.yml@refs/heads/main",
		testIssuer, artifact)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	trust, _ := trustedMaterial()

	if err := verifyEntity(trust, entity, bytes.NewReader(artifact)); err == nil {
		t.Error("a repository merely containing our name was accepted")
	}
}

// A bundle that is not JSON is a refusal, not a crash.
func TestAMalformedBundleIsRefused(t *testing.T) {
	authority, err := ca.NewVirtualSigstore()
	if err != nil {
		t.Fatalf("building a signing authority: %v", err)
	}
	trustThis(t, authority)

	for _, bundle := range [][]byte{nil, []byte(""), []byte("not json"), []byte("{}")} {
		if err := Verify(bundle, strings.NewReader("a release")); !errors.Is(err, ErrUnverified) {
			t.Errorf("a bundle of %q gave %v", string(bundle), err)
		}
	}
}
