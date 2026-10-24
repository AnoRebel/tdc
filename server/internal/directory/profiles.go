package directory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

// Service keeps directory profiles.
type Service struct {
	pool    *db.Pool
	queries *dbgen.Queries
	log     *slog.Logger
}

// NewService builds a directory over a database.
func NewService(pool *db.Pool, log *slog.Logger) *Service {
	return &Service{pool: pool, queries: dbgen.New(pool), log: log}
}

/*
Limits on what a profile holds.

Generous for a person describing themselves and small for a page read on a
phone over metered data, which is where a developer answering a client is most
likely to be reading it.
*/
const (
	maxHeadline = 120
	maxBio      = 2000
	// maxLocation is the account location's limit too, enforced there; a
	// moderator replacing it through a profile is held to the same.
	maxLocation     = 80
	maxExperience   = 500
	maxContactField = 200
	maxPortfolio    = 20
	maxItemTitle    = 120
	maxItemURL      = 500
	maxItemText     = 500
	maxNote         = 200

	/*
		maxAvailability is how far ahead somebody may say they are available.

		Six months: long enough that nobody has to keep renewing it, short
		enough that a profile somebody stopped looking after stops advertising
		them within a season rather than indefinitely.
	*/
	maxAvailability = 183 * 24 * time.Hour
)

// ProfileInput is everything a person may say in their own profile.
//
// No location: that belongs to the account, set in its own settings, and a
// profile shows it rather than holding a second copy.
type ProfileInput struct {
	Headline   string
	Bio        string
	Experience string

	ContactEmail string
	ContactPhone string
	ContactURL   string

	Visibility        Visibility
	DetailsVisibility Visibility
	// ContactVisibility left empty means hidden: the most restrictive setting
	// unless the owner chose otherwise.
	ContactVisibility Visibility

	Portfolio []PortfolioInput
	Skills    []SkillInput

	// OpenToWork is whether they are looking, until AvailableUntil. Being open
	// requires an expiry: a statement about availability that never lapses is
	// the stale profile the expiry exists to prevent.
	OpenToWork       bool
	AvailabilityNote string
	AvailableUntil   *time.Time

	// AcceptsEnquiries is the owner's choice about being approached through
	// the directory, or nil to follow the profile's visibility: a public
	// profile takes enquiries, anything else does not.
	AcceptsEnquiries *bool
}

// PortfolioInput is one link. An item with an ID is one already on the
// profile, changed or kept; one without is new.
type PortfolioInput struct {
	ID          *uuid.UUID
	Title       string
	URL         string
	Description string
}

// PortfolioItem is one link, as stored.
type PortfolioItem struct {
	ID          uuid.UUID
	Title       string
	URL         string
	Description string
	UpdatedAt   time.Time
}

// CompanyReference is a company a profile's owner has agreed to be listed by.
type CompanyReference struct {
	ID   uuid.UUID
	Slug string
	Name string
	Role string
}

/*
Profile is a profile as one viewer may see it.

DetailsShown and ContactShown say whether the viewer may see those sections at
all; when false, the fields in them are empty because the query withheld them,
and a response must omit them rather than send the empty value — an empty
contact field would tell a viewer there is nothing, where the truth is that it
is not theirs to know.
*/
type Profile struct {
	UserID      uuid.UUID
	Username    string
	DisplayName string

	Visibility        Visibility
	DetailsVisibility Visibility
	ContactVisibility Visibility

	DetailsShown bool
	ContactShown bool

	Headline   string
	Bio        string
	Location   string
	Experience string

	ContactEmail string
	ContactPhone string
	ContactURL   string

	Portfolio []PortfolioItem
	Companies []CompanyReference
	Skills    []SkillClaim

	// OpenToWork and AvailableUntil are what the owner stated, and are read
	// only for the owner. Available is whether that statement is current —
	// open, and not yet expired — which is all a viewer is told.
	OpenToWork       bool
	AvailableUntil   *time.Time
	Available        bool
	AvailabilityNote string

	// AcceptsEnquiriesChoice is the owner's stated choice, read only for the
	// owner; AcceptsEnquiries is whether an enquiry would be taken now — for a
	// viewer, from them in particular.
	AcceptsEnquiriesChoice *bool
	AcceptsEnquiries       bool

	CreatedAt time.Time
	UpdatedAt time.Time
}

/*
Save creates somebody's profile or replaces what it says.

The portfolio is replaced as a list: items sent back with their identifier are
kept and updated, new ones are added, and anything not sent back is removed.
Identifiers survive an edit because a verification names one item, and editing
an item must be distinguishable from deleting it and adding another.

Returns whether the profile was created by this call, and any verifications
the save cleared because the claim they covered changed.
*/
func (s *Service) Save(ctx context.Context, userID uuid.UUID, input ProfileInput) (bool, []Verification, error) {
	if err := normalise(&input); err != nil {
		return false, nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, nil, fmt.Errorf("begin saving a profile: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)

	created, err := queries.SaveDirectoryProfile(ctx, dbgen.SaveDirectoryProfileParams{
		UserID:                userID,
		Headline:              input.Headline,
		Bio:                   input.Bio,
		Experience:            input.Experience,
		ContactEmail:          input.ContactEmail,
		ContactPhone:          input.ContactPhone,
		ContactUrl:            input.ContactURL,
		Visibility:            string(input.Visibility),
		DetailsVisibility:     string(input.DetailsVisibility),
		ContactVisibility:     string(input.ContactVisibility),
		OpenToWork:            input.OpenToWork,
		AvailabilityNote:      input.AvailabilityNote,
		AvailabilityExpiresAt: input.AvailableUntil,
		AcceptsEnquiries:      input.AcceptsEnquiries,
	})
	if err != nil {
		return false, nil, fmt.Errorf("save a profile: %w", err)
	}

	if err := savePortfolio(ctx, queries, userID, input.Portfolio); err != nil {
		return false, nil, err
	}

	skills, err := s.resolveSkills(ctx, queries, userID, input.Skills)
	if err != nil {
		return false, nil, err
	}
	if err := saveSkills(ctx, queries, userID, skills); err != nil {
		return false, nil, err
	}

	cleared, err := clearStale(ctx, queries, userID)
	if err != nil {
		return false, nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return false, nil, fmt.Errorf("commit a profile: %w", err)
	}
	return created, cleared, nil
}

// savePortfolio reconciles the stored portfolio with the list sent.
func savePortfolio(ctx context.Context, queries *dbgen.Queries, userID uuid.UUID, items []PortfolioInput) error {
	keep := make([]uuid.UUID, 0, len(items))

	for position, item := range items {
		if item.ID != nil {
			changed, err := queries.UpdatePortfolioItem(ctx, dbgen.UpdatePortfolioItemParams{
				ID:          *item.ID,
				UserID:      userID,
				Title:       item.Title,
				Url:         item.URL,
				Description: item.Description,
				Position:    int32(position),
			})
			if err != nil {
				return fmt.Errorf("update a portfolio item: %w", err)
			}
			if changed == 1 {
				keep = append(keep, *item.ID)
				continue
			}
			/*
				An identifier that is not one of this person's items.

				Treated as new rather than refused: a client resending a list
				after the item was removed elsewhere should not fail the whole
				save, and the identifier it named is never reused — the new
				item gets its own, so nobody's item can be claimed by naming
				it.
			*/
		}

		id := uuid.New()
		if err := queries.InsertPortfolioItem(ctx, dbgen.InsertPortfolioItemParams{
			ID:          id,
			UserID:      userID,
			Title:       item.Title,
			Url:         item.URL,
			Description: item.Description,
			Position:    int32(position),
		}); err != nil {
			return fmt.Errorf("add a portfolio item: %w", err)
		}
		keep = append(keep, id)
	}

	if err := queries.DeletePortfolioItemsExcept(ctx, dbgen.DeletePortfolioItemsExceptParams{
		UserID: userID,
		Keep:   keep,
	}); err != nil {
		return fmt.Errorf("remove portfolio items: %w", err)
	}
	return nil
}

// Own returns somebody's own profile, every section of it.
func (s *Service) Own(ctx context.Context, userID uuid.UUID) (Profile, error) {
	row, err := s.queries.GetOwnDirectoryProfile(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, ErrNoProfile
	}
	if err != nil {
		return Profile{}, fmt.Errorf("read a profile: %w", err)
	}

	profile := Profile{
		UserID:                 row.UserID,
		Username:               row.Username,
		DisplayName:            row.DisplayName,
		Visibility:             Visibility(row.Visibility),
		DetailsVisibility:      Visibility(row.DetailsVisibility),
		ContactVisibility:      Visibility(row.ContactVisibility),
		DetailsShown:           true,
		ContactShown:           true,
		Headline:               row.Headline,
		Bio:                    row.Bio,
		Location:               row.Location,
		Experience:             row.Experience,
		ContactEmail:           row.ContactEmail,
		ContactPhone:           row.ContactPhone,
		ContactURL:             row.ContactUrl,
		OpenToWork:             row.OpenToWork,
		AvailableUntil:         row.AvailabilityExpiresAt,
		AcceptsEnquiriesChoice: row.AcceptsEnquiries,
		AcceptsEnquiries:       acceptsByDefault(row.AcceptsEnquiries, Visibility(row.Visibility)),
		AvailabilityNote:       row.AvailabilityNote,
		Available: row.OpenToWork && row.AvailabilityExpiresAt != nil &&
			row.AvailabilityExpiresAt.After(time.Now()),
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}

	skills, err := s.queries.ListOwnProfileSkills(ctx, userID)
	if err != nil {
		return Profile{}, fmt.Errorf("read a profile's skills: %w", err)
	}
	for _, skill := range skills {
		profile.Skills = append(profile.Skills, SkillClaim{
			ID: skill.SkillID, Name: skill.Name, Status: SkillStatus(skill.Status),
			Description: skill.Description, UpdatedAt: skill.UpdatedAt,
		})
	}

	items, err := s.queries.ListPortfolioItems(ctx, userID)
	if err != nil {
		return Profile{}, fmt.Errorf("read a portfolio: %w", err)
	}
	profile.Portfolio = portfolioOf(items)

	companies, err := s.queries.ViewProfileCompanies(ctx, dbgen.ViewProfileCompaniesParams{
		UserID: userID, ViewerRank: int16(Owner),
	})
	if err != nil {
		return Profile{}, fmt.Errorf("read a profile's companies: %w", err)
	}
	profile.Companies = companiesOf(companies)

	return profile, nil
}

/*
View returns a profile as a viewer of the given rank may see it.

ErrNoProfile covers everything the viewer may not learn: no such username, no
profile, and a profile hidden from them all answer the same way.
*/
func (s *Service) View(ctx context.Context, username string, rank Rank, viewer uuid.UUID) (Profile, error) {
	row, err := s.queries.ViewDirectoryProfile(ctx, dbgen.ViewDirectoryProfileParams{
		ViewerRank: int16(rank),
		Username:   username,
		Viewer:     viewer,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, ErrNoProfile
	}
	if err != nil {
		return Profile{}, fmt.Errorf("read a profile: %w", err)
	}

	profile := Profile{
		UserID:       row.UserID,
		Username:     row.Username,
		DisplayName:  row.DisplayName,
		Visibility:   Visibility(row.Visibility),
		DetailsShown: row.DetailsShown,
		ContactShown: row.ContactShown,
		Headline:     row.Headline,
		Bio:          row.Bio,
		Location:     row.Location,
		Experience:   row.Experience,
		ContactEmail: row.ContactEmail,
		ContactPhone: row.ContactPhone,
		ContactURL:   row.ContactUrl,
		Available:    row.Available,
		// Somebody not signed in cannot send an enquiry, so is not told a
		// profile takes them.
		AcceptsEnquiries: row.AcceptsEnquiries && viewer != uuid.Nil,
		UpdatedAt:        row.UpdatedAt,
	}
	if row.Available {
		profile.AvailabilityNote = row.AvailabilityNote
		until := row.AvailableUntil
		profile.AvailableUntil = &until
	}

	skills, err := s.queries.ViewProfileSkills(ctx, dbgen.ViewProfileSkillsParams{
		UserID: row.UserID, ViewerRank: int16(rank),
	})
	if err != nil {
		return Profile{}, fmt.Errorf("read a profile's skills: %w", err)
	}
	for _, skill := range skills {
		profile.Skills = append(profile.Skills, SkillClaim{
			ID: skill.SkillID, Name: skill.Name, Status: SkillActive,
			Description: skill.Description, UpdatedAt: skill.UpdatedAt,
		})
	}

	// The portfolio and companies belong to the details section, and each
	// query withholds them by the viewer's rank itself rather than being
	// skipped on the flag above: a read that depended on the caller
	// remembering to skip it would be a read that eventually does not.
	items, err := s.queries.ViewPortfolioItems(ctx, dbgen.ViewPortfolioItemsParams{
		UserID: row.UserID, ViewerRank: int16(rank),
	})
	if err != nil {
		return Profile{}, fmt.Errorf("read a portfolio: %w", err)
	}
	profile.Portfolio = portfolioOf(items)

	companies, err := s.queries.ViewProfileCompanies(ctx, dbgen.ViewProfileCompaniesParams{
		UserID: row.UserID, ViewerRank: int16(rank),
	})
	if err != nil {
		return Profile{}, fmt.Errorf("read a profile's companies: %w", err)
	}
	profile.Companies = companiesOf(companies)

	return profile, nil
}

// acceptsByDefault is whether a profile takes enquiries: the owner's choice
// where there is one, and otherwise whether the profile is public.
func acceptsByDefault(choice *bool, visibility Visibility) bool {
	if choice != nil {
		return *choice
	}
	return visibility == Public
}

// Delete removes somebody's profile, leaving their account as it was.
func (s *Service) Delete(ctx context.Context, userID uuid.UUID) error {
	removed, err := s.queries.DeleteDirectoryProfile(ctx, userID)
	if err != nil {
		return fmt.Errorf("delete a profile: %w", err)
	}
	if removed == 0 {
		return ErrNoProfile
	}
	return nil
}

// Moderation is what a moderator may change about somebody else's profile.
type Moderation struct {
	Headline   string
	Bio        string
	Location   string
	Experience string
	Visibility Visibility
	// RemovePortfolio removes every portfolio link, which is the one change to
	// the portfolio moderation needs: a link cannot be partly abusive.
	RemovePortfolio bool
}

// Moderate replaces what somebody's profile says. The caller has checked the
// permission and records the action.
func (s *Service) Moderate(ctx context.Context, userID uuid.UUID, change Moderation) error {
	if !change.Visibility.Valid() {
		return invalid("visibility", `must be "public", "members" or "hidden"`)
	}
	input := ProfileInput{
		Headline: change.Headline, Bio: change.Bio, Experience: change.Experience,
		Visibility: change.Visibility, DetailsVisibility: Members, ContactVisibility: Hidden,
	}
	if err := normalise(&input); err != nil {
		return err
	}
	place := strings.TrimSpace(change.Location)
	if utf8.RuneCountInString(place) > maxLocation {
		return invalid("location", fmt.Sprintf("at most %d characters", maxLocation))
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin moderating a profile: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)

	if err := queries.ModerateDirectoryProfile(ctx, dbgen.ModerateDirectoryProfileParams{
		UserID:     userID,
		Headline:   input.Headline,
		Bio:        input.Bio,
		Experience: input.Experience,
		Visibility: string(input.Visibility),
	}); err != nil {
		return fmt.Errorf("moderate a profile: %w", err)
	}
	// The location the profile shows is the account's, so replacing what the
	// profile says replaces that too.
	if err := queries.ModerateLocation(ctx, dbgen.ModerateLocationParams{
		UserID: userID, Place: place,
	}); err != nil {
		return fmt.Errorf("moderate a location: %w", err)
	}
	if change.RemovePortfolio {
		if err := queries.DeletePortfolioItemsExcept(ctx, dbgen.DeletePortfolioItemsExceptParams{
			UserID: userID, Keep: []uuid.UUID{},
		}); err != nil {
			return fmt.Errorf("remove a portfolio: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit moderating a profile: %w", err)
	}
	return nil
}

// OwnerOf returns whose profile a username names, for moderation.
func (s *Service) OwnerOf(ctx context.Context, username string) (uuid.UUID, error) {
	row, err := s.queries.GetDirectoryProfileOwnerByUsername(ctx, username)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNoProfile
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("find a profile: %w", err)
	}
	return row.ID, nil
}

func portfolioOf(rows []dbgen.PortfolioItem) []PortfolioItem {
	items := make([]PortfolioItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, PortfolioItem{
			ID: row.ID, Title: row.Title, URL: row.Url,
			Description: row.Description, UpdatedAt: row.UpdatedAt,
		})
	}
	return items
}

func companiesOf(rows []dbgen.ViewProfileCompaniesRow) []CompanyReference {
	companies := make([]CompanyReference, 0, len(rows))
	for _, row := range rows {
		companies = append(companies, CompanyReference{ID: row.ID, Slug: row.Slug, Name: row.Name, Role: row.Role})
	}
	return companies
}

/*
normalise trims what was sent and refuses what cannot be stored.

The contact visibility defaults to hidden when nothing is chosen, which is the
spec's requirement stated in the one place every save passes through: a client
that forgets the field publishes nothing.
*/
func normalise(input *ProfileInput) error {
	input.Headline = strings.TrimSpace(input.Headline)
	input.Bio = strings.TrimSpace(input.Bio)
	input.Experience = strings.TrimSpace(input.Experience)
	input.ContactEmail = strings.TrimSpace(input.ContactEmail)
	input.ContactPhone = strings.TrimSpace(input.ContactPhone)
	input.ContactURL = strings.TrimSpace(input.ContactURL)

	if input.Visibility == "" {
		input.Visibility = Members
	}
	if input.DetailsVisibility == "" {
		input.DetailsVisibility = Members
	}
	if input.ContactVisibility == "" {
		input.ContactVisibility = Hidden
	}
	for field, value := range map[string]Visibility{
		"visibility":        input.Visibility,
		"detailsVisibility": input.DetailsVisibility,
		"contactVisibility": input.ContactVisibility,
	} {
		if !value.Valid() {
			return invalid(field, `must be "public", "members" or "hidden"`)
		}
	}

	for _, check := range []struct {
		field, value string
		limit        int
	}{
		{"headline", input.Headline, maxHeadline},
		{"bio", input.Bio, maxBio},
		{"experience", input.Experience, maxExperience},
		{"contactEmail", input.ContactEmail, maxContactField},
		{"contactPhone", input.ContactPhone, maxContactField},
		{"contactUrl", input.ContactURL, maxContactField},
	} {
		if utf8.RuneCountInString(check.value) > check.limit {
			return invalid(check.field, fmt.Sprintf("at most %d characters", check.limit))
		}
	}

	if input.ContactEmail != "" {
		if address, err := mail.ParseAddress(input.ContactEmail); err != nil || address.Address != input.ContactEmail {
			return invalid("contactEmail", "is not an email address")
		}
	}
	if input.ContactURL != "" && !isWebLink(input.ContactURL) {
		return invalid("contactUrl", "must be an http or https link")
	}

	input.AvailabilityNote = strings.TrimSpace(input.AvailabilityNote)
	if utf8.RuneCountInString(input.AvailabilityNote) > maxNote {
		return invalid("availabilityNote", fmt.Sprintf("at most %d characters", maxNote))
	}
	if input.OpenToWork {
		now := time.Now()
		switch {
		case input.AvailableUntil == nil:
			return invalid("availableUntil", "say until when; availability has to lapse")
		case !input.AvailableUntil.After(now):
			return invalid("availableUntil", "must be in the future")
		case input.AvailableUntil.After(now.Add(maxAvailability)):
			return invalid("availableUntil", "at most six months ahead")
		}
	} else {
		input.AvailableUntil = nil
	}

	if len(input.Portfolio) > maxPortfolio {
		return invalid("portfolio", fmt.Sprintf("at most %d links", maxPortfolio))
	}
	for index := range input.Portfolio {
		item := &input.Portfolio[index]
		item.Title = strings.TrimSpace(item.Title)
		item.URL = strings.TrimSpace(item.URL)
		item.Description = strings.TrimSpace(item.Description)

		field := fmt.Sprintf("portfolio[%d]", index)
		switch {
		case item.Title == "":
			return invalid(field+".title", "is required")
		case utf8.RuneCountInString(item.Title) > maxItemTitle:
			return invalid(field+".title", fmt.Sprintf("at most %d characters", maxItemTitle))
		case len(item.URL) > maxItemURL || !isWebLink(item.URL):
			return invalid(field+".url", "must be an http or https link")
		case utf8.RuneCountInString(item.Description) > maxItemText:
			return invalid(field+".description", fmt.Sprintf("at most %d characters", maxItemText))
		}
	}

	return nil
}

/*
isWebLink accepts a link a browser will open as a page.

Restricted to http and https, so a portfolio link cannot be a javascript: URL
that runs in whoever clicks it, or a data: URL carrying whatever its author
liked into a stranger's browser.
*/
func isWebLink(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}
