package directory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
Verification: a moderator saying one specific claim on a profile checks out.

What a verification means is fixed and small, and the interface says it
wherever one is shown: somebody with the permission to verify looked at the
evidence offered for this one claim and judged it credible. It is not the
server vouching for a person's character, the quality of their work, or
whether they will deliver — nobody checked any of that.

A verification belongs to the claim as it was when it was checked. Each one
holds a digest of the claim's content then, and is shown only while the claim
still has that content; editing the claim clears it, and it stays cleared even
if the claim is edited back, because what is on the profile now is not what
anybody looked at.
*/

// ClaimKind is what sort of thing a claim is.
type ClaimKind string

const (
	// ClaimSkill is a skill on a profile and what its owner says about it.
	ClaimSkill ClaimKind = "skill"
	// ClaimPortfolio is one portfolio link.
	ClaimPortfolio ClaimKind = "portfolio"
	// ClaimIdentity is that the person is who their profile says: their name,
	// and the link they give as who they are elsewhere.
	ClaimIdentity ClaimKind = "identity"
	// ClaimCompany is that they are part of a company, in the role it gave
	// them.
	ClaimCompany ClaimKind = "company"
)

var (
	// ErrNoClaim reports a claim that is not on the profile, or not one that
	// can be verified.
	ErrNoClaim = errors.New("that claim is not on the profile")
	// ErrAlreadyVerified reports a claim with a standing verification.
	ErrAlreadyVerified = errors.New("that claim is already verified")
	// ErrOwnClaim reports somebody verifying their own profile.
	ErrOwnClaim = errors.New("nobody verifies their own claims")
	// ErrNoVerification reports a verification that does not exist or no
	// longer stands.
	ErrNoVerification = errors.New("no such verification")
)

const maxVerificationNote = 500

// Claim is one thing on a profile that can be verified, as it stands now.
type Claim struct {
	Kind ClaimKind
	ID   uuid.UUID
	// Label is how the claim is named to a person: the skill's name, the
	// link's title, the company's name.
	Label   string
	Content map[string]string
	Digest  string
}

// Key identifies a claim across reads.
func (c Claim) Key() string { return ClaimKey(c.Kind, c.ID) }

// ClaimKey is how a claim is looked up.
func ClaimKey(kind ClaimKind, id uuid.UUID) string { return string(kind) + ":" + id.String() }

// Verification is a moderator's attestation about one claim.
type Verification struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	Kind       ClaimKind
	ClaimID    uuid.UUID
	Note       string
	VerifiedBy string
	VerifiedAt time.Time
	Snapshot   map[string]string
}

/*
digestOf is the fingerprint of a claim's content.

JSON with sorted keys — which encoding/json gives a map — so the same content
always produces the same digest regardless of the order it was assembled in.
*/
func digestOf(kind ClaimKind, content map[string]string) string {
	encoded, _ := json.Marshal(map[string]any{"kind": kind, "content": content})
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func claimOf(kind ClaimKind, id uuid.UUID, label string, content map[string]string) Claim {
	return Claim{Kind: kind, ID: id, Label: label, Content: content, Digest: digestOf(kind, content)}
}

/*
claimsWith lists every verifiable claim on a profile, as it stands.

Skills still waiting for review are not claims anybody can verify: they are not
in the vocabulary yet, and verifying one would attest to something the
directory does not recognise.
*/
func claimsWith(ctx context.Context, queries *dbgen.Queries, userID uuid.UUID) ([]Claim, error) {
	profile, err := queries.GetOwnDirectoryProfile(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoProfile
	}
	if err != nil {
		return nil, fmt.Errorf("read a profile: %w", err)
	}

	claims := []Claim{claimOf(ClaimIdentity, userID, profile.DisplayName, map[string]string{
		"username": profile.Username, "displayName": profile.DisplayName, "link": profile.ContactUrl,
	})}

	skills, err := queries.ListOwnProfileSkills(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("read a profile's skills: %w", err)
	}
	for _, skill := range skills {
		if skill.Status != string(SkillActive) {
			continue
		}
		claims = append(claims, claimOf(ClaimSkill, skill.SkillID, skill.Name, map[string]string{
			"skill": skill.Name, "description": skill.Description,
		}))
	}

	items, err := queries.ListPortfolioItems(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("read a portfolio: %w", err)
	}
	for _, item := range items {
		claims = append(claims, claimOf(ClaimPortfolio, item.ID, item.Title, map[string]string{
			"title": item.Title, "url": item.Url, "description": item.Description,
		}))
	}

	companies, err := queries.GetOwnCompanyClaims(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("read a profile's companies: %w", err)
	}
	for _, company := range companies {
		claims = append(claims, claimOf(ClaimCompany, company.ID, company.Name, map[string]string{
			"company": company.Slug, "role": company.Role,
		}))
	}

	return claims, nil
}

// Claims lists what on a profile can be verified.
func (s *Service) Claims(ctx context.Context, userID uuid.UUID) ([]Claim, error) {
	return claimsWith(ctx, s.queries, userID)
}

func verificationOf(row dbgen.ProfileVerification) Verification {
	snapshot := map[string]string{}
	_ = json.Unmarshal(row.ClaimSnapshot, &snapshot)
	return Verification{
		ID: row.ID, UserID: row.UserID, Kind: ClaimKind(row.ClaimKind), ClaimID: row.ClaimID,
		Note: row.Note, VerifiedBy: row.VerifiedByLabel, VerifiedAt: row.VerifiedAt, Snapshot: snapshot,
	}
}

/*
Verifications returns the verifications that apply to a profile now, by claim.

Only those neither revoked nor cleared, and only where the claim still says
exactly what it said when it was checked — the last is checked here against
the live claim, so a claim edited by some path that did not clear its
verification is still not shown as verified.
*/
func (s *Service) Verifications(ctx context.Context, userID uuid.UUID) (map[string]Verification, error) {
	claims, err := claimsWith(ctx, s.queries, userID)
	if err != nil {
		return nil, err
	}
	live := map[string]string{}
	for _, claim := range claims {
		live[claim.Key()] = claim.Digest
	}

	rows, err := s.queries.StandingVerifications(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("read verifications: %w", err)
	}
	standing := map[string]Verification{}
	for _, row := range rows {
		key := ClaimKey(ClaimKind(row.ClaimKind), row.ClaimID)
		if digest, present := live[key]; present && digest == row.ClaimDigest {
			standing[key] = verificationOf(row)
		}
	}
	return standing, nil
}

/*
Verify records a moderator's attestation about one claim.

The claim must be on the profile now, and the note — what was checked — is
required. The claim's content at this moment is what the verification covers.
*/
func (s *Service) Verify(
	ctx context.Context,
	verifier uuid.UUID,
	verifierLabel string,
	userID uuid.UUID,
	kind ClaimKind,
	claimID uuid.UUID,
	note string,
) (Verification, Claim, error) {
	note = strings.TrimSpace(note)
	switch {
	case verifier == userID:
		// A claim somebody verifies for themselves is the self-asserted claim
		// it already was.
		return Verification{}, Claim{}, ErrOwnClaim
	case note == "":
		return Verification{}, Claim{}, invalid("note", "say what you checked; it is kept with the verification")
	case utf8.RuneCountInString(note) > maxVerificationNote:
		return Verification{}, Claim{}, invalid("note", fmt.Sprintf("at most %d characters", maxVerificationNote))
	}

	claims, err := claimsWith(ctx, s.queries, userID)
	if err != nil {
		return Verification{}, Claim{}, err
	}
	var claim *Claim
	for index := range claims {
		if claims[index].Kind == kind && claims[index].ID == claimID {
			claim = &claims[index]
		}
	}
	if claim == nil {
		return Verification{}, Claim{}, ErrNoClaim
	}

	snapshot, err := json.Marshal(claim.Content)
	if err != nil {
		return Verification{}, Claim{}, fmt.Errorf("record a claim: %w", err)
	}
	row, err := s.queries.InsertVerification(ctx, dbgen.InsertVerificationParams{
		ID: uuid.New(), UserID: userID, ClaimKind: string(kind), ClaimID: claimID,
		ClaimDigest: claim.Digest, ClaimSnapshot: snapshot, Note: note,
		VerifiedBy: &verifier, VerifiedByLabel: verifierLabel,
	})
	if isUniqueViolation(err) {
		return Verification{}, Claim{}, ErrAlreadyVerified
	}
	if err != nil {
		return Verification{}, Claim{}, fmt.Errorf("record a verification: %w", err)
	}
	return verificationOf(row), *claim, nil
}

// Revoke withdraws a verification, with the reason required.
func (s *Service) Revoke(ctx context.Context, id, by uuid.UUID, reason string) (Verification, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return Verification{}, invalid("reason", "say why; it is kept with the record")
	}
	row, err := s.queries.GetVerification(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Verification{}, ErrNoVerification
	}
	if err != nil {
		return Verification{}, fmt.Errorf("read a verification: %w", err)
	}
	revoked, err := s.queries.RevokeVerification(ctx, dbgen.RevokeVerificationParams{
		ID: id, RevokedBy: &by, Reason: &reason,
	})
	if err != nil {
		return Verification{}, fmt.Errorf("revoke a verification: %w", err)
	}
	if revoked == 0 {
		return Verification{}, ErrNoVerification
	}
	return verificationOf(row), nil
}

/*
clearStale clears every verification whose claim is gone or has changed.

Run in the same transaction as the edit that changed it, so there is no moment
at which the edited claim is shown with the verification of the old one — and
recorded as cleared rather than left to be inferred, so it does not come back if
the claim is edited back to what was checked.
*/
func clearStale(ctx context.Context, queries *dbgen.Queries, userID uuid.UUID) ([]Verification, error) {
	claims, err := claimsWith(ctx, queries, userID)
	if err != nil {
		return nil, err
	}
	live := map[string]string{}
	for _, claim := range claims {
		live[claim.Key()] = claim.Digest
	}

	rows, err := queries.StandingVerifications(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("read verifications: %w", err)
	}
	cleared := []Verification{}
	for _, row := range rows {
		if digest, present := live[ClaimKey(ClaimKind(row.ClaimKind), row.ClaimID)]; present && digest == row.ClaimDigest {
			continue
		}
		if _, err := queries.ClearVerification(ctx, row.ID); err != nil {
			return nil, fmt.Errorf("clear a verification: %w", err)
		}
		cleared = append(cleared, verificationOf(row))
	}
	return cleared, nil
}
