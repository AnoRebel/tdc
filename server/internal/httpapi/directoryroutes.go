package httpapi

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/cache"
	"github.com/AnoRebel/tdc/server/internal/directory"
	"github.com/AnoRebel/tdc/server/internal/identity"
	"github.com/AnoRebel/tdc/server/internal/settings"
)

/*
The directory over HTTP.

Every route here sits behind one feature toggle, applied to the group rather
than per route, so a directory route cannot be added without it: an operator
who turns the directory off turns off all of it at once, and a server that
never turned it on answers every one of these as though it had never heard of
them.

Mounted from init, with its service built here rather than threaded through
Services, because nothing outside these files needs it.
*/
type directoryAPI struct {
	*Server
	directory *directory.Service
}

func init() {
	registerRoutes(func(s *Server) {
		api := &directoryAPI{Server: s, directory: directory.NewService(s.db, s.log)}
		api.mount()
	})

	documentRoutes(map[string]entry{
		"GET /api/directory/me":                         {Summary: "Your own directory profile", Authentication: "session or token"},
		"PUT /api/directory/me":                         {Summary: "Create or change your directory profile", Authentication: "session or token"},
		"DELETE /api/directory/me":                      {Summary: "Delete your directory profile, keeping your account", Authentication: "session or token"},
		"GET /api/directory/me/export":                  {Summary: "Export your directory profile", Authentication: "session or token"},
		"GET /api/directory/me/companies":               {Summary: "Companies you belong to or are invited by", Authentication: "session or token"},
		"POST /api/directory/me/companies/:slug/accept": {Summary: "Agree to be listed by a company", Authentication: "session or token"},
		"DELETE /api/directory/me/companies/:slug":      {Summary: "Leave a company or decline its invitation", Authentication: "session or token"},
		"GET /api/directory/profiles/:username":         {Summary: "A directory profile, as you may see it", Authentication: "optional"},
		"POST /api/directory/companies":                 {Summary: "Create a company profile", Authentication: "session or token"},
		"GET /api/directory/companies/:slug":            {Summary: "A company profile", Authentication: "optional"},
		"PUT /api/directory/companies/:slug":            {Summary: "Change a company profile", Authentication: "session or token"},
		"DELETE /api/directory/companies/:slug":         {Summary: "Delete a company profile", Authentication: "session or token"},
		"POST /api/directory/companies/:slug/members":   {Summary: "Ask somebody to be listed by a company", Authentication: "session or token"},
		"DELETE /api/directory/companies/:slug/members/:username": {
			Summary: "Remove somebody from a company", Authentication: "session or token",
		},
		"GET /api/directory/moderation/profiles/:username": {
			Summary: "Read a profile as its owner sees it", Authentication: "session or token",
			Permission: string(authz.PermDirectoryProfileManage),
		},
		"PUT /api/directory/moderation/profiles/:username": {
			Summary: "Change or clear somebody's profile", Authentication: "session or token",
			Permission: string(authz.PermDirectoryProfileManage),
		},
	})
}

func (api *directoryAPI) mount() {
	directoryGroup := api.echo.Group("/api/directory", api.requireFeature(settings.FeatureDirectory))

	// Somebody's own profile. Under /me because it belongs to the account,
	// and never reachable for anybody else's.
	me := directoryGroup.Group("/me", requireAuthentication)
	me.GET("", api.handleOwnProfile)
	me.PUT("", api.handleSaveProfile)
	me.DELETE("", api.handleDeleteProfile)
	me.GET("/export", api.handleExportProfile)
	me.GET("/companies", api.handleMyCompanies)
	me.POST("/companies/:slug/accept", api.handleAcceptCompany)
	me.DELETE("/companies/:slug", api.handleLeaveCompany)

	/*
		Reading the directory.

		No requireAuthentication: a public profile is visible to anybody the
		server lets discover it, which is the point of making it public. What
		somebody not signed in may see is decided in the query by their rank,
		and whether they may ask at all by the server's own discoverability.
	*/
	directoryGroup.GET("/profiles/:username", api.handleViewProfile)

	directoryGroup.POST("/companies", api.handleCreateCompany, requireAuthentication)
	directoryGroup.GET("/companies/:slug", api.handleViewCompany)
	directoryGroup.PUT("/companies/:slug", api.handleUpdateCompany, requireAuthentication)
	directoryGroup.DELETE("/companies/:slug", api.handleDeleteCompany, requireAuthentication)
	directoryGroup.POST("/companies/:slug/members", api.handleInviteToCompany, requireAuthentication)
	directoryGroup.DELETE("/companies/:slug/members/:username", api.handleRemoveFromCompany, requireAuthentication)

	/*
		Moderation, under the permission that governs it.

		Separate paths rather than a flag on the ordinary ones, so the
		ordinary read can never be widened by a parameter: seeing what an owner
		hid is only possible on a route that requires the permission and
		records its use.
	*/
	moderation := directoryGroup.Group("/moderation", requireAuthentication)
	moderation.GET("/profiles/:username", api.handleModeratorViewProfile,
		requirePermission(authz.PermDirectoryProfileManage))
	moderation.PUT("/profiles/:username", api.handleModerateProfile,
		requirePermission(authz.PermDirectoryProfileManage))

	// The rest of the directory, from files of their own.
	api.mountSkills(directoryGroup, moderation)
	api.mountSearch(directoryGroup)
	api.mountVerification(moderation)
	api.mountEnquiries(directoryGroup, me)
	api.mountEngagements(directoryGroup, me)
}

/*
withinReadLimit refuses a read of the directory past the operator's rate.

Per person when signed in, per address otherwise. Every read counts the same —
a search, a profile, a company — because walking profiles one by one is the
same enumeration as paging through results.
*/
func (api *directoryAPI) withinReadLimit(c *echo.Context) error {
	ctx := c.Request().Context()
	key := "directory:read:address:" + requestIP(c).String()
	setting := settings.DirectoryAnonymousReadsPerMinute
	if user, ok := currentUser(c); ok {
		key = "directory:read:user:" + user.ID.String()
		setting = settings.DirectoryReadsPerMinute
	}

	limit := cache.RateLimit{Attempts: int(api.limitFor(ctx, setting)), Window: time.Minute}
	if limit.Attempts < 1 {
		limit.Attempts = 1
	}
	decision, err := api.limiter.Allow(ctx, key, limit)
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return retryAfter(c, decision.RetryAfter)
	}
	return nil
}

/*
viewerRank is how much of the directory this caller may see, or false when
they may not see it at all.

Somebody signed in is a member. Somebody who is not may read public profiles
only on a server that lets strangers discover it — a private server's directory
is as undiscoverable as the server itself.
*/
func (api *directoryAPI) viewerRank(c *echo.Context) (directory.Rank, uuid.UUID, bool) {
	if user, ok := currentUser(c); ok {
		return directory.Member, user.ID, true
	}
	if api.visibility(c.Request().Context()) != VisibilityPublic {
		return directory.Anonymous, uuid.Nil, false
	}
	return directory.Anonymous, uuid.Nil, true
}

// noProfile is the one refusal for everything a viewer may not learn about a
// profile, worded identically whichever it was.
func noProfile() error {
	return echo.NewHTTPError(http.StatusNotFound, "no such profile")
}

// directoryError turns the directory's refusals into responses.
func directoryError(err error) error {
	var problem *directory.InvalidError
	switch {
	case errors.As(err, &problem):
		return echo.NewHTTPError(http.StatusBadRequest, problem.Error())
	case errors.Is(err, directory.ErrNoProfile):
		return noProfile()
	case errors.Is(err, directory.ErrNoCompany), errors.Is(err, directory.ErrNotAssociated):
		return echo.NewHTTPError(http.StatusNotFound, err.Error())
	case errors.Is(err, directory.ErrNotCompanyAdministrator):
		return echo.NewHTTPError(http.StatusForbidden, err.Error())
	case errors.Is(err, directory.ErrCompanyTaken), errors.Is(err, directory.ErrLastAdministrator):
		return echo.NewHTTPError(http.StatusConflict, err.Error())
	}
	return err
}

/*
profileView is a profile as somebody other than its owner sees it.

Every field past the name is omitted when empty, and a withheld field arrives
empty because the query never read it. So an absent bio means "nothing you may
see" without saying which: somebody who wrote nothing and somebody who hid what
they wrote look the same, which is what hiding it asked for.
*/
type profileView struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Address     string `json:"address"`

	Headline   string `json:"headline,omitempty"`
	Bio        string `json:"bio,omitempty"`
	Location   string `json:"location,omitempty"`
	Experience string `json:"experience,omitempty"`

	ContactEmail string `json:"contactEmail,omitempty"`
	ContactPhone string `json:"contactPhone,omitempty"`
	ContactURL   string `json:"contactUrl,omitempty"`

	Portfolio []portfolioView  `json:"portfolio,omitempty"`
	Companies []companyRefView `json:"companies,omitempty"`
	Skills    []skillClaimView `json:"skills,omitempty"`

	// IdentityVerified is present when a moderator has verified that the
	// person is who their profile says.
	IdentityVerified *verifiedView `json:"identityVerified,omitempty"`

	// Available is present only when true: whether somebody is not looking,
	// or has hidden whether they are, is not something to announce.
	Available        bool   `json:"available,omitempty"`
	AvailabilityNote string `json:"availabilityNote,omitempty"`
	AvailableUntil   string `json:"availableUntil,omitempty"`

	// AcceptsEnquiries is whether this viewer could send one now. False for
	// somebody the owner blocked, exactly as for a profile that takes none.
	AcceptsEnquiries bool `json:"acceptsEnquiries"`

	UpdatedAt string `json:"updatedAt"`
}

type skillClaimView struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Description string        `json:"description,omitempty"`
	Verified    *verifiedView `json:"verified,omitempty"`
}

type portfolioView struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	URL         string        `json:"url"`
	Description string        `json:"description,omitempty"`
	Verified    *verifiedView `json:"verified,omitempty"`
}

type companyRefView struct {
	Slug     string        `json:"slug"`
	Name     string        `json:"name"`
	Role     string        `json:"role,omitempty"`
	Verified *verifiedView `json:"verified,omitempty"`
}

/*
verifiedView says a claim was verified: by whom and when.

The note — what the moderator checked — is shown only to the profile's owner.
It is the moderator's own words, and may mention something the owner has
hidden from other viewers, such as the link that was checked.
*/
type verifiedView struct {
	By   string `json:"by"`
	At   string `json:"at"`
	Note string `json:"note,omitempty"`
}

// verifiedOf describes a standing verification of a claim, or nil.
func verifiedOf(verifications map[string]directory.Verification, kind directory.ClaimKind, id uuid.UUID, withNote bool) *verifiedView {
	verification, ok := verifications[directory.ClaimKey(kind, id)]
	if !ok {
		return nil
	}
	view := &verifiedView{By: verification.VerifiedBy, At: verification.VerifiedAt.UTC().Format(time.RFC3339)}
	if withNote {
		view.Note = verification.Note
	}
	return view
}

func (api *directoryAPI) viewOf(profile directory.Profile, verifications map[string]directory.Verification) profileView {
	view := profileView{
		Username:    profile.Username,
		DisplayName: profile.DisplayName,
		Address:     api.rooms.UserAddress(profile.Username),
		UpdatedAt:   profile.UpdatedAt.UTC().Format(time.RFC3339),

		AcceptsEnquiries: profile.AcceptsEnquiries,
	}

	// Copied as the query returned them. Whatever the viewer may not see
	// arrived empty, and empty fields are omitted, so nothing here decides
	// disclosure — that happened in SQL, where a mistake cannot be made
	// after the value was already read.
	view.Headline = profile.Headline
	view.Bio = profile.Bio
	view.Location = profile.Location
	view.Experience = profile.Experience
	view.ContactEmail = profile.ContactEmail
	view.ContactPhone = profile.ContactPhone
	view.ContactURL = profile.ContactURL
	for _, item := range profile.Portfolio {
		view.Portfolio = append(view.Portfolio, portfolioView{
			ID: item.ID.String(), Title: item.Title, URL: item.URL, Description: item.Description,
			Verified: verifiedOf(verifications, directory.ClaimPortfolio, item.ID, false),
		})
	}
	for _, company := range profile.Companies {
		view.Companies = append(view.Companies, companyRefView{
			Slug: company.Slug, Name: company.Name, Role: company.Role,
			Verified: verifiedOf(verifications, directory.ClaimCompany, company.ID, false),
		})
	}
	for _, skill := range profile.Skills {
		view.Skills = append(view.Skills, skillClaimView{
			ID: skill.ID.String(), Name: skill.Name, Description: skill.Description,
			Verified: verifiedOf(verifications, directory.ClaimSkill, skill.ID, false),
		})
	}
	// Only a verification of something shown is shown: the identity always is,
	// since a name is never withheld; the rest ride on the claims above, which
	// are present only where the viewer may see them.
	view.IdentityVerified = verifiedOf(verifications, directory.ClaimIdentity, profile.UserID, false)
	if profile.Available && profile.AvailableUntil != nil {
		view.Available = true
		view.AvailabilityNote = profile.AvailabilityNote
		view.AvailableUntil = profile.AvailableUntil.UTC().Format(time.RFC3339)
	}
	return view
}

// ownProfileResponse is somebody's own profile: everything, and the settings
// that decide who else sees it.
type ownProfileResponse struct {
	Username    string                    `json:"username"`
	DisplayName string                    `json:"displayName"`
	Address     string                    `json:"address"`
	Profile     directory.ProfileDocument `json:"profile"`
	Companies   []companyRefView          `json:"companies"`
	// Verifications are the owner's own, with what each moderator checked.
	Verifications []ownVerificationView `json:"verifications"`
	CreatedAt     string                `json:"createdAt"`
	UpdatedAt     string                `json:"updatedAt"`
}

// ownVerificationView is one of the owner's verifications, in full.
type ownVerificationView struct {
	Kind    string `json:"kind"`
	ClaimID string `json:"claimId"`
	verifiedView
}

func (api *directoryAPI) ownResponse(profile directory.Profile, verifications map[string]directory.Verification) ownProfileResponse {
	response := ownProfileResponse{
		Username:      profile.Username,
		DisplayName:   profile.DisplayName,
		Address:       api.rooms.UserAddress(profile.Username),
		Profile:       profile.Document(),
		Companies:     []companyRefView{},
		Verifications: []ownVerificationView{},
		CreatedAt:     profile.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:     profile.UpdatedAt.UTC().Format(time.RFC3339),
	}
	for _, company := range profile.Companies {
		response.Companies = append(response.Companies, companyRefView{
			Slug: company.Slug, Name: company.Name, Role: company.Role,
			Verified: verifiedOf(verifications, directory.ClaimCompany, company.ID, true),
		})
	}
	for _, verification := range verifications {
		response.Verifications = append(response.Verifications, ownVerificationView{
			Kind: string(verification.Kind), ClaimID: verification.ClaimID.String(),
			verifiedView: *verifiedOf(verifications, verification.Kind, verification.ClaimID, true),
		})
	}
	sort.Slice(response.Verifications, func(i, j int) bool {
		return response.Verifications[i].At < response.Verifications[j].At
	})
	return response
}

// ownWithVerifications reads somebody's own profile and its verifications.
func (api *directoryAPI) ownWithVerifications(ctx context.Context, userID uuid.UUID) (ownProfileResponse, error) {
	profile, err := api.directory.Own(ctx, userID)
	if err != nil {
		return ownProfileResponse{}, err
	}
	verifications, err := api.directory.Verifications(ctx, userID)
	if err != nil {
		return ownProfileResponse{}, err
	}
	return api.ownResponse(profile, verifications), nil
}

func (api *directoryAPI) handleOwnProfile(c *echo.Context) error {
	user, _ := currentUser(c)
	response, err := api.ownWithVerifications(c.Request().Context(), user.ID)
	if err != nil {
		return directoryError(err)
	}
	return c.JSON(http.StatusOK, response)
}

/*
handleSaveProfile creates somebody's profile or replaces what it says.

A PUT of the whole document rather than a PATCH of fields, because a profile is
edited as one form and saved as one: a partial update from a client that knew
about fewer fields would otherwise silently leave stale ones in place.
*/
func (api *directoryAPI) handleSaveProfile(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	var document directory.ProfileDocument
	if err := c.Bind(&document); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	created, cleared, err := api.directory.Save(ctx, user.ID, document.Input())
	if err != nil {
		return directoryError(err)
	}

	/*
		A verification the edit cleared is recorded, attributed to the owner
		whose edit cleared it. Otherwise a moderator reading the log would see
		a verification granted and never withdrawn, and could not tell why the
		profile no longer shows it.
	*/
	for _, verification := range cleared {
		if err := api.audit.Record(ctx, audit.Entry{
			ActorID:    &user.ID,
			ActorLabel: user.Username,
			Action:     audit.ActionDirectoryVerificationCleared,
			TargetKind: "directory_verification",
			TargetID:   verification.ID.String(),
			SourceIP:   requestIP(c),
			UserAgent:  c.Request().UserAgent(),
			Detail: map[string]any{
				"claim": string(verification.Kind), "claimId": verification.ClaimID.String(),
				"reason": "the claim was edited or removed after it was verified",
			},
		}); err != nil {
			return err
		}
	}

	response, err := api.ownWithVerifications(ctx, user.ID)
	if err != nil {
		return err
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	return c.JSON(status, response)
}

// handleDeleteProfile removes somebody's profile. Their account, messages and
// sessions are untouched: this is leaving the directory, not the server.
func (api *directoryAPI) handleDeleteProfile(c *echo.Context) error {
	user, _ := currentUser(c)
	if err := api.directory.Delete(c.Request().Context(), user.ID); err != nil {
		return directoryError(err)
	}
	return c.NoContent(http.StatusNoContent)
}

// handleExportProfile returns somebody's own profile in the documented
// format, as a download.
func (api *directoryAPI) handleExportProfile(c *echo.Context) error {
	user, _ := currentUser(c)
	export, err := api.directory.Export(c.Request().Context(), user.ID, api.config.CanonicalAddress)
	if err != nil {
		return directoryError(err)
	}
	c.Response().Header().Set("Content-Disposition",
		`attachment; filename="directory-profile-`+user.Username+`.json"`)
	return c.JSON(http.StatusOK, export)
}

func (api *directoryAPI) handleViewProfile(c *echo.Context) error {
	rank, viewer, allowed := api.viewerRank(c)
	if !allowed {
		return noProfile()
	}
	if err := api.withinReadLimit(c); err != nil {
		return err
	}

	ctx := c.Request().Context()
	profile, err := api.directory.View(ctx, c.Param("username"), rank, viewer)
	if err != nil {
		return directoryError(err)
	}
	verifications, err := api.directory.Verifications(ctx, profile.UserID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, api.viewOf(profile, verifications))
}

/*
handleModeratorViewProfile shows a moderator a profile as its owner sees it.

Recorded every time. Unlike the member list, where a moderator is audited only
when something was actually withheld, the point of this route is to see past
the owner's choices, so every use of it is the exception being exercised.
*/
func (api *directoryAPI) handleModeratorViewProfile(c *echo.Context) error {
	ctx := c.Request().Context()
	moderator, _ := currentUser(c)

	owner, err := api.directory.OwnerOf(ctx, c.Param("username"))
	if err != nil {
		return directoryError(err)
	}
	profile, err := api.directory.Own(ctx, owner)
	if err != nil {
		return directoryError(err)
	}

	if err := api.audit.Record(ctx, audit.Entry{
		ActorID:    &moderator.ID,
		ActorLabel: moderator.Username,
		Action:     audit.ActionDirectoryProfileViewed,
		TargetKind: "directory_profile",
		TargetID:   owner.String(),
		SourceIP:   requestIP(c),
		UserAgent:  c.Request().UserAgent(),
	}); err != nil {
		// Refused rather than shown unrecorded: an exception nobody can
		// review is not an exception.
		return err
	}

	verifications, err := api.directory.Verifications(ctx, owner)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, api.ownResponse(profile, verifications))
}

type moderateProfileRequest struct {
	Headline        string               `json:"headline"`
	Bio             string               `json:"bio"`
	Location        string               `json:"location"`
	Experience      string               `json:"experience"`
	Visibility      directory.Visibility `json:"visibility"`
	RemovePortfolio bool                 `json:"removePortfolio"`
	// Reason is required: the next moderator, and the person whose profile
	// it is, need to know why.
	Reason string `json:"reason"`
}

/*
handleModerateProfile replaces what somebody's profile says.

Audited with which fields changed and the reason, never with what they said:
the log records that something abusive was removed, not the abuse. Refused if
the record cannot be written — the change and its account of itself happen
together or not at all.
*/
func (api *directoryAPI) handleModerateProfile(c *echo.Context) error {
	ctx := c.Request().Context()
	moderator, _ := currentUser(c)

	var request moderateProfileRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	if request.Reason == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "say why: the reason is kept with the record")
	}

	owner, err := api.directory.OwnerOf(ctx, c.Param("username"))
	if err != nil {
		return directoryError(err)
	}
	before, err := api.directory.Own(ctx, owner)
	if err != nil {
		return directoryError(err)
	}

	if err := api.directory.Moderate(ctx, owner, directory.Moderation{
		Headline: request.Headline, Bio: request.Bio, Location: request.Location,
		Experience: request.Experience, Visibility: request.Visibility,
		RemovePortfolio: request.RemovePortfolio,
	}); err != nil {
		return directoryError(err)
	}

	changed := []string{}
	for field, differs := range map[string]bool{
		"headline":   before.Headline != request.Headline,
		"bio":        before.Bio != request.Bio,
		"location":   before.Location != request.Location,
		"experience": before.Experience != request.Experience,
		"visibility": before.Visibility != request.Visibility,
		"portfolio":  request.RemovePortfolio && len(before.Portfolio) > 0,
	} {
		if differs {
			changed = append(changed, field)
		}
	}
	sort.Strings(changed)

	if err := api.audit.Record(ctx, audit.Entry{
		ActorID:    &moderator.ID,
		ActorLabel: moderator.Username,
		Action:     audit.ActionDirectoryProfileModerated,
		TargetKind: "directory_profile",
		TargetID:   owner.String(),
		SourceIP:   requestIP(c),
		UserAgent:  c.Request().UserAgent(),
		Detail:     map[string]any{"changed": changed, "reason": request.Reason},
	}); err != nil {
		return err
	}

	profile, err := api.directory.Own(ctx, owner)
	if err != nil {
		return err
	}
	verifications, err := api.directory.Verifications(ctx, owner)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, api.ownResponse(profile, verifications))
}

// companyRequest is what a company's administrators send.
type companyRequest struct {
	Slug        string               `json:"slug"`
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Location    string               `json:"location"`
	Visibility  directory.Visibility `json:"visibility"`
	// Role is the creator's own, when creating.
	Role string `json:"role"`
}

type companyMemberView struct {
	Username      string `json:"username"`
	DisplayName   string `json:"displayName"`
	Address       string `json:"address"`
	Role          string `json:"role,omitempty"`
	Administrator bool   `json:"administrator,omitempty"`
	Since         string `json:"since"`
}

type companyView struct {
	Slug        string              `json:"slug"`
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	Location    string              `json:"location,omitempty"`
	Visibility  string              `json:"visibility"`
	Members     []companyMemberView `json:"members"`
	// Pending and Administered are only ever set for the company's own
	// administrators.
	Pending      []companyMemberView `json:"pending,omitempty"`
	Administered bool                `json:"administered,omitempty"`
}

func (api *directoryAPI) companyViewOf(company directory.Company) companyView {
	view := companyView{
		Slug: company.Slug, Name: company.Name, Description: company.Description,
		Location: company.Location, Visibility: string(company.Visibility),
		Members: []companyMemberView{}, Administered: company.Administered,
	}
	for _, member := range company.Members {
		view.Members = append(view.Members, api.memberView(member))
	}
	for _, member := range company.Pending {
		view.Pending = append(view.Pending, api.memberView(member))
	}
	return view
}

func (api *directoryAPI) memberView(member directory.CompanyMember) companyMemberView {
	return companyMemberView{
		Username: member.Username, DisplayName: member.DisplayName,
		Address: api.rooms.UserAddress(member.Username), Role: member.Role,
		Administrator: member.Administrator, Since: member.Since.UTC().Format(time.RFC3339),
	}
}

func (api *directoryAPI) handleCreateCompany(c *echo.Context) error {
	user, _ := currentUser(c)
	var request companyRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	company, err := api.directory.CreateCompany(c.Request().Context(), user.ID, request.Role, directory.CompanyInput{
		Slug: request.Slug, Name: request.Name, Description: request.Description,
		Location: request.Location, Visibility: request.Visibility,
	})
	if err != nil {
		return directoryError(err)
	}
	return c.JSON(http.StatusCreated, api.companyViewOf(company))
}

func (api *directoryAPI) handleViewCompany(c *echo.Context) error {
	rank, viewer, allowed := api.viewerRank(c)
	if !allowed {
		return echo.NewHTTPError(http.StatusNotFound, directory.ErrNoCompany.Error())
	}
	if err := api.withinReadLimit(c); err != nil {
		return err
	}
	company, err := api.directory.ViewCompany(c.Request().Context(), c.Param("slug"), rank, viewer)
	if err != nil {
		return directoryError(err)
	}
	return c.JSON(http.StatusOK, api.companyViewOf(company))
}

func (api *directoryAPI) handleUpdateCompany(c *echo.Context) error {
	user, _ := currentUser(c)
	var request companyRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	company, err := api.directory.UpdateCompany(c.Request().Context(), user.ID, c.Param("slug"), directory.CompanyInput{
		Name: request.Name, Description: request.Description,
		Location: request.Location, Visibility: request.Visibility,
	})
	if err != nil {
		return directoryError(err)
	}
	return c.JSON(http.StatusOK, api.companyViewOf(company))
}

func (api *directoryAPI) handleDeleteCompany(c *echo.Context) error {
	user, _ := currentUser(c)
	if err := api.directory.DeleteCompany(c.Request().Context(), user.ID, c.Param("slug")); err != nil {
		return directoryError(err)
	}
	return c.NoContent(http.StatusNoContent)
}

type inviteToCompanyRequest struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

// handleInviteToCompany asks somebody to be listed. It lists nobody: the
// person has to accept first.
func (api *directoryAPI) handleInviteToCompany(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)

	var request inviteToCompanyRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	invitee, err := api.queries.GetUserByUsername(ctx, identity.NormaliseUsername(request.Username))
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.NewHTTPError(http.StatusNotFound, "there is nobody by that name here")
	}
	if err != nil {
		return err
	}

	if err := api.directory.InviteToCompany(ctx, user.ID, c.Param("slug"), invitee.ID, request.Role); err != nil {
		return directoryError(err)
	}
	return c.NoContent(http.StatusAccepted)
}

// handleRemoveFromCompany is an administrator removing somebody, or somebody
// removing themselves by name.
func (api *directoryAPI) handleRemoveFromCompany(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)

	member, err := api.queries.GetUserByUsername(ctx, identity.NormaliseUsername(c.Param("username")))
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.NewHTTPError(http.StatusNotFound, directory.ErrNotAssociated.Error())
	}
	if err != nil {
		return err
	}
	if err := api.directory.LeaveCompany(ctx, user.ID, c.Param("slug"), member.ID); err != nil {
		return directoryError(err)
	}
	return c.NoContent(http.StatusNoContent)
}

type associationView struct {
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	Role          string `json:"role,omitempty"`
	Administrator bool   `json:"administrator,omitempty"`
	// Pending means the company asked and this person has not answered.
	Pending    bool   `json:"pending"`
	InvitedAt  string `json:"invitedAt"`
	AcceptedAt string `json:"acceptedAt,omitempty"`
}

func (api *directoryAPI) handleMyCompanies(c *echo.Context) error {
	user, _ := currentUser(c)
	associations, err := api.directory.Associations(c.Request().Context(), user.ID)
	if err != nil {
		return err
	}
	views := make([]associationView, 0, len(associations))
	for _, association := range associations {
		view := associationView{
			Slug: association.Slug, Name: association.Name, Role: association.Role,
			Administrator: association.Administrator, Pending: association.Pending,
			InvitedAt: association.InvitedAt.UTC().Format(time.RFC3339),
		}
		if association.AcceptedAt != nil {
			view.AcceptedAt = association.AcceptedAt.UTC().Format(time.RFC3339)
		}
		views = append(views, view)
	}
	return c.JSON(http.StatusOK, views)
}

func (api *directoryAPI) handleAcceptCompany(c *echo.Context) error {
	user, _ := currentUser(c)
	if err := api.directory.AcceptCompany(c.Request().Context(), user.ID, c.Param("slug")); err != nil {
		return directoryError(err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (api *directoryAPI) handleLeaveCompany(c *echo.Context) error {
	user, _ := currentUser(c)
	if err := api.directory.LeaveCompany(c.Request().Context(), user.ID, c.Param("slug"), user.ID); err != nil {
		return directoryError(err)
	}
	return c.NoContent(http.StatusNoContent)
}
