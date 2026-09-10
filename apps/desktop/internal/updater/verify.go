/*
Package updater finds newer releases and refuses to install anything it cannot
prove came from us.

The trust model is one sentence: an artifact is installed only when a sigstore
bundle proves it was built by this project's release workflow. No bundle, no
trusted root, no match — nothing is installed, and somebody is told why.

Fail closed is the whole point. An updater that falls back to "well, it
downloaded" when verification is unavailable is worse than no updater: it is a
mechanism for replacing a running application with whatever answered the
request, wearing the appearance of a security feature.
*/
package updater

import (
	"errors"
	"fmt"
	"io"
	"sync"

	sigbundle "github.com/sigstore/sigstore-go/pkg/bundle"
	sigroot "github.com/sigstore/sigstore-go/pkg/root"
	sigverify "github.com/sigstore/sigstore-go/pkg/verify"
)

/*
Who is allowed to have signed a release.

Keyless signing binds a signature to the identity that produced it rather than
to a key somebody has to keep secret. These two constants are what make that
binding mean something: a signature from any other issuer, or from a workflow
in any other repository, is not a TDC release however valid the signature is.
*/
const (
	// certIdentityIssuer is where the release workflow authenticates.
	certIdentityIssuer = "https://token.actions.githubusercontent.com"

	/*
		certIdentitySANRegex matches the signing certificate's subject.

		Anchored at both ends and escaping the dots, so it matches this
		repository and nothing that merely contains its name — a repository
		called AnoRebel/tdc-not-really would otherwise pass.
	*/
	certIdentitySANRegex = `^https://github\.com/AnoRebel/tdc/`
)

var (
	// ErrNoTrustRoot is returned when nothing can be verified, so nothing is
	// installed. Not an error in the update — an error in this build.
	ErrNoTrustRoot = errors.New("updates are disabled: no trust root is configured")

	// ErrUnverified is returned when a bundle is missing, malformed, or does
	// not prove what it claims. Always terminal.
	ErrUnverified = errors.New("this download could not be shown to come from TDC")
)

var (
	trustMu sync.RWMutex
	// trustJSON is the pinned sigstore trusted root, as embedded at build time.
	trustJSON []byte
	/*
		trustOverride lets tests point verification at an in-process signing
		authority instead of the real one.

		Necessary rather than convenient: verifying a real bundle needs a real
		release, and a test that could only run after a release is a test that
		never runs before one.
	*/
	trustOverride sigroot.TrustedMaterial
)

/*
SetTrustedRoot installs the sigstore trusted root for this process.

Called once at startup with the embedded bytes. A placeholder — which is what
ships until a release pipeline exists — leaves updates disabled rather than
unverified.
*/
func SetTrustedRoot(trustedRootJSON []byte) {
	trustMu.Lock()
	defer trustMu.Unlock()
	trustJSON = trustedRootJSON
	trustOverride = nil
}

// setTrustedMaterial points verification at material a test supplies.
func setTrustedMaterial(material sigroot.TrustedMaterial) {
	trustMu.Lock()
	defer trustMu.Unlock()
	trustOverride = material
	trustJSON = nil
}

/*
trustedMaterial returns what to verify against, and whether there is any.

Unparseable and absent are the same answer: without a trust root there is
nothing to check a download against, so there is nothing safe to do with one.
*/
func trustedMaterial() (sigroot.TrustedMaterial, bool) {
	trustMu.RLock()
	override, pinned := trustOverride, trustJSON
	trustMu.RUnlock()

	if override != nil {
		return override, true
	}
	if len(pinned) == 0 {
		return nil, false
	}

	root, err := sigroot.NewTrustedRootFromJSON(pinned)
	if err != nil {
		return nil, false
	}
	return root, true
}

// Enabled reports whether this build can verify an update at all.
func Enabled() bool {
	_, ok := trustedMaterial()
	return ok
}

/*
Verify checks that an artifact is what its bundle says it is.

Offline: the bundle carries the signing certificate and the transparency-log
inclusion proof, so this needs no network and cannot be defeated by controlling
what the machine can reach.
*/
func Verify(bundleJSON []byte, artifact io.Reader) error {
	trust, ok := trustedMaterial()
	if !ok {
		return ErrNoTrustRoot
	}

	var bundle sigbundle.Bundle
	if err := bundle.UnmarshalJSON(bundleJSON); err != nil {
		return fmt.Errorf("%w: the signature file could not be read: %v", ErrUnverified, err)
	}

	return verifyEntity(trust, &bundle, artifact)
}

/*
verifyEntity runs the policy against a parsed bundle or a test entity.

Both an observer timestamp and a transparency-log entry are required. Either
alone leaves a gap: a signature with no log entry cannot be shown to have
existed before somebody noticed a key was stolen, and a log entry with no
timestamp cannot be placed in time at all.
*/
func verifyEntity(trust sigroot.TrustedMaterial, entity sigverify.SignedEntity, artifact io.Reader) error {
	identity, err := sigverify.NewShortCertificateIdentity(
		certIdentityIssuer, "", "", certIdentitySANRegex)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnverified, err)
	}

	verifier, err := sigverify.NewVerifier(trust,
		sigverify.WithObserverTimestamps(1),
		sigverify.WithTransparencyLog(1),
	)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnverified, err)
	}

	policy := sigverify.NewPolicy(
		sigverify.WithArtifact(artifact),
		sigverify.WithCertificateIdentity(identity),
	)

	if _, err := verifier.Verify(entity, policy); err != nil {
		return fmt.Errorf("%w: %v", ErrUnverified, err)
	}

	return nil
}
