/*
Package updater finds newer releases and refuses to install anything it cannot
prove came from us.

The trust model is one sentence: an artifact is installed only when a sigstore
bundle proves it was built by this project's release workflow, for that
version's tag. No bundle, no trusted root, no match — nothing is installed, and somebody is told why.

Fail closed is the whole point. An updater that falls back to "well, it
downloaded" when verification is unavailable is worse than no updater: it is a
mechanism for replacing a running application with whatever answered the
request, wearing the appearance of a security feature.
*/
package updater

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	sigbundle "github.com/sigstore/sigstore-go/pkg/bundle"
	sigroot "github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
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
		releaseWorkflow is the signing certificate's subject, less the
		version: the release workflow, run for a version's tag. Other
		workflows in this repository can sign too, and none of them makes
		releases.
	*/
	releaseWorkflow = "https://github.com/AnoRebel/tdc/.github/workflows/release.yml@refs/tags/v"
)

/*
signerFor is the one subject a release of this version may be signed by,
matched whole, so AnoRebel/tdc-not-really does not pass. Tied to the version
so a feed cannot offer an old signed release as a newer one: its signature
names the old tag.
*/
func signerFor(version string) string {
	return "^" + regexp.QuoteMeta(releaseWorkflow+version) + "$"
}

var (
	// ErrNoTrustRoot is returned when sigstore's trusted root could not be
	// had, so nothing can be checked and nothing is installed.
	ErrNoTrustRoot = errors.New("updates cannot be checked: sigstore's trusted root could not be fetched")

	// ErrUnverified is returned when a bundle is missing, malformed, or does
	// not prove what it claims. Always terminal.
	ErrUnverified = errors.New("this download could not be shown to come from TDC")
)

var (
	trustMu sync.Mutex
	trust   sigroot.TrustedMaterial
)

/*
trustedMaterial is sigstore's public trusted root, fetched over TUF.

Fetched rather than embedded: sigstore's keys rotate, and a copy built into the
application would refuse every release signed after the next rotation. TUF
checks what it fetches against the root sigstore-go carries, so whoever answers
the request cannot substitute their own. Cached under the user's cache
directory, and held for the life of the process once had.
*/
func trustedMaterial() (sigroot.TrustedMaterial, error) {
	trustMu.Lock()
	defer trustMu.Unlock()
	if trust != nil {
		return trust, nil
	}
	options := tuf.DefaultOptions()
	if cache, err := os.UserCacheDir(); err == nil && cache != "" {
		options.CachePath = filepath.Join(cache, "tdc", "sigstore")
	}
	fetched, err := sigroot.FetchTrustedRootWithOptions(options)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoTrustRoot, err)
	}
	trust = fetched
	return trust, nil
}

// setTrustedMaterial points verification at material a test supplies.
func setTrustedMaterial(material sigroot.TrustedMaterial) {
	trustMu.Lock()
	defer trustMu.Unlock()
	trust = material
}

/*
VerifiedDigest checks a release's bundle and returns the SHA-256 digest of the
artifact it vouches for.

The bundle is checked before anything is downloaded; the digest is what the
download must then match, which Wails' updater checks as the bytes arrive.
Beyond fetching the trusted root, offline: the bundle carries the signing
certificate and the transparency-log inclusion proof.
*/
func VerifiedDigest(bundleJSON []byte, version string) ([]byte, error) {
	trust, err := trustedMaterial()
	if err != nil {
		return nil, err
	}

	var bundle sigbundle.Bundle
	if err := bundle.UnmarshalJSON(bundleJSON); err != nil {
		return nil, fmt.Errorf("%w: the signature file could not be read: %v", ErrUnverified, err)
	}
	return digestOf(trust, &bundle, version)
}

// digestOf verifies an entity's signature over the digest it carries.
func digestOf(trust sigroot.TrustedMaterial, entity sigverify.SignedEntity, version string) ([]byte, error) {
	content, err := entity.SignatureContent()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnverified, err)
	}
	message := content.MessageSignatureContent()
	if message == nil || (message.DigestAlgorithm() != "SHA2_256" && message.DigestAlgorithm() != "sha256") {
		return nil, fmt.Errorf("%w: the signature is not over a SHA-256 digest", ErrUnverified)
	}
	digest := message.Digest()
	if err := verifyEntity(trust, entity, sigverify.WithArtifactDigest("sha256", digest), version); err != nil {
		return nil, err
	}
	return digest, nil
}

/*
verifyEntity runs the policy against a parsed bundle or a test entity.

Both an observer timestamp and a transparency-log entry are required. Either
alone leaves a gap: a signature with no log entry cannot be shown to have
existed before somebody noticed a key was stolen, and a log entry with no
timestamp cannot be placed in time at all.
*/
func verifyEntity(trust sigroot.TrustedMaterial, entity sigverify.SignedEntity, artifact sigverify.ArtifactPolicyOption, version string) error {
	identity, err := sigverify.NewShortCertificateIdentity(
		certIdentityIssuer, "", "", signerFor(version))
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
		artifact,
		sigverify.WithCertificateIdentity(identity),
	)

	if _, err := verifier.Verify(entity, policy); err != nil {
		return fmt.Errorf("%w: %v", ErrUnverified, err)
	}

	return nil
}
