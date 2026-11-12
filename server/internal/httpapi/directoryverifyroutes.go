package httpapi

import (
	"errors"
	"net/http"
	"time"
	"uuid"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/directory"
)

/*
Verification over HTTP: a moderator reading the claims on a profile, attesting
to one, and withdrawing an attestation.

All of it under directory.verify, and every grant and revocation recorded with
who, what and why. A grant the log could not record is refused rather than
made silently, since an attestation nobody can trace is worth less than none.
*/

func init() {
	permission := string(authz.PermDirectoryVerify)
	documentRoutes(map[string]entry{
		"GET /api/directory/moderation/profiles/:username/claims": {
			Summary: "The verifiable claims on a profile", Authentication: "session or token", Permission: permission,
		},
		"POST /api/directory/moderation/profiles/:username/verifications": {
			Summary: "Verify one claim on a profile", Authentication: "session or token", Permission: permission,
		},
		"POST /api/directory/moderation/verifications/:id/revoke": {
			Summary: "Revoke a verification", Authentication: "session or token", Permission: permission,
		},
	})
}

func (api *directoryAPI) mountVerification(moderation *echo.Group) {
	verify := requirePermission(authz.PermDirectoryVerify)
	moderation.GET("/profiles/:username/claims", api.handleListClaims, verify)
	moderation.POST("/profiles/:username/verifications", api.handleVerify, verify)
	moderation.POST("/verifications/:id/revoke", api.handleRevokeVerification, verify)
}

type claimView struct {
	Kind    string            `json:"kind"`
	ID      string            `json:"id"`
	Label   string            `json:"label"`
	Content map[string]string `json:"content"`
	// Verification is the standing one, when there is one: its identifier is
	// what revoking takes.
	Verification *claimVerificationView `json:"verification,omitempty"`
}

type claimVerificationView struct {
	ID string `json:"id"`
	verifiedView
}

type claimsView struct {
	Username    string      `json:"username"`
	DisplayName string      `json:"displayName"`
	Claims      []claimView `json:"claims"`
}

/*
verifiableOwner finds whose profile a moderator is verifying.

A hidden profile is refused as though it did not exist: its owner has not asked
to be found, and a verifier's permission is to check claims people are making,
not to read profiles nobody is meant to see. A moderator who needs to look at a
hidden profile has directory.profile.manage, whose reads are recorded.
*/
func (api *directoryAPI) verifiableOwner(c *echo.Context) (directory.Profile, error) {
	ctx := c.Request().Context()
	owner, err := api.directory.OwnerOf(ctx, c.Param("username"))
	if err != nil {
		return directory.Profile{}, directoryError(err)
	}
	profile, err := api.directory.Own(ctx, owner)
	if err != nil {
		return directory.Profile{}, directoryError(err)
	}
	if profile.Visibility == directory.Hidden {
		return directory.Profile{}, noProfile()
	}
	return profile, nil
}

func (api *directoryAPI) handleListClaims(c *echo.Context) error {
	ctx := c.Request().Context()
	profile, err := api.verifiableOwner(c)
	if err != nil {
		return err
	}

	claims, err := api.directory.Claims(ctx, profile.UserID)
	if err != nil {
		return directoryError(err)
	}
	verifications, err := api.directory.Verifications(ctx, profile.UserID)
	if err != nil {
		return err
	}

	view := claimsView{Username: profile.Username, DisplayName: profile.DisplayName, Claims: []claimView{}}
	for _, claim := range claims {
		entry := claimView{Kind: string(claim.Kind), ID: claim.ID.String(), Label: claim.Label, Content: claim.Content}
		if verification, ok := verifications[claim.Key()]; ok {
			entry.Verification = &claimVerificationView{
				ID:           verification.ID.String(),
				verifiedView: *verifiedOf(verifications, claim.Kind, claim.ID, true),
			}
		}
		view.Claims = append(view.Claims, entry)
	}
	return c.JSON(http.StatusOK, view)
}

type verifyRequest struct {
	Kind    string `json:"kind"`
	ClaimID string `json:"claimId"`
	// Note is what was checked. Required, and kept with the verification.
	Note string `json:"note"`
}

func (api *directoryAPI) handleVerify(c *echo.Context) error {
	ctx := c.Request().Context()
	verifier, _ := currentUser(c)

	var request verifyRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	claimID, err := uuid.Parse(request.ClaimID)
	if err != nil || request.Kind == "" {
		return echo.NewHTTPError(http.StatusBadRequest,
			"name the claim: kind and claimId are both required")
	}

	profile, err := api.verifiableOwner(c)
	if err != nil {
		return err
	}

	verification, claim, err := api.directory.Verify(ctx, verifier.ID, verifier.Username,
		profile.UserID, directory.ClaimKind(request.Kind), claimID, request.Note)
	if err != nil {
		return verificationError(err)
	}

	if err := api.audit.Record(ctx, audit.Entry{
		ActorID:    &verifier.ID,
		ActorLabel: verifier.Username,
		Action:     audit.ActionDirectoryVerificationGranted,
		TargetKind: "directory_verification",
		TargetID:   verification.ID.String(),
		SourceIP:   requestIP(c),
		UserAgent:  c.Request().UserAgent(),
		Detail: map[string]any{
			"profile": profile.Username, "claim": string(claim.Kind),
			"claimId": claim.ID.String(), "label": claim.Label, "note": verification.Note,
		},
	}); err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, claimVerificationView{
		ID: verification.ID.String(),
		verifiedView: verifiedView{
			By: verification.VerifiedBy, At: verification.VerifiedAt.UTC().Format(time.RFC3339),
			Note: verification.Note,
		},
	})
}

type revokeRequest struct {
	Reason string `json:"reason"`
}

func (api *directoryAPI) handleRevokeVerification(c *echo.Context) error {
	ctx := c.Request().Context()
	moderator, _ := currentUser(c)

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, directory.ErrNoVerification.Error())
	}
	var request revokeRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	verification, err := api.directory.Revoke(ctx, id, moderator.ID, request.Reason)
	if err != nil {
		return verificationError(err)
	}

	if err := api.audit.Record(ctx, audit.Entry{
		ActorID:    &moderator.ID,
		ActorLabel: moderator.Username,
		Action:     audit.ActionDirectoryVerificationRevoked,
		TargetKind: "directory_verification",
		TargetID:   verification.ID.String(),
		SourceIP:   requestIP(c),
		UserAgent:  c.Request().UserAgent(),
		Detail: map[string]any{
			"claim": string(verification.Kind), "claimId": verification.ClaimID.String(),
			"reason": request.Reason,
		},
	}); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func verificationError(err error) error {
	switch {
	case errors.Is(err, directory.ErrNoClaim), errors.Is(err, directory.ErrNoVerification):
		return echo.NewHTTPError(http.StatusNotFound, err.Error())
	case errors.Is(err, directory.ErrOwnClaim):
		return echo.NewHTTPError(http.StatusForbidden, err.Error())
	case errors.Is(err, directory.ErrAlreadyVerified):
		return echo.NewHTTPError(http.StatusConflict, err.Error())
	}
	return directoryError(err)
}
