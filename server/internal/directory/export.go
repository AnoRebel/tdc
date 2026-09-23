package directory

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ProfileExportFormat names the shape of an exported profile, so a reader can
// tell which version of it they are holding. Documented in docs/directory.md.
const ProfileExportFormat = "tdc.directory.profile/1"

/*
ProfileExport is somebody's own profile in a form they can take elsewhere.

The Profile field is deliberately the same shape the profile is saved in, so
an export can be sent straight back to recreate what it describes — which is
what makes it portable rather than merely readable. Everything else is context
a reader needs and a save ignores.
*/
type ProfileExport struct {
	Format      string          `json:"format"`
	ExportedAt  string          `json:"exportedAt"`
	Server      string          `json:"server"`
	Username    string          `json:"username"`
	DisplayName string          `json:"displayName"`
	Profile     ProfileDocument `json:"profile"`
	Companies   []ExportCompany `json:"companies"`
	CreatedAt   string          `json:"createdAt"`
	UpdatedAt   string          `json:"updatedAt"`
}

/*
ProfileDocument is a profile as it is written and read over the API.

One type for both directions, so the export and the save cannot drift into two
descriptions of the same thing.
*/
type ProfileDocument struct {
	Headline          string              `json:"headline"`
	Bio               string              `json:"bio"`
	Location          string              `json:"location"`
	Experience        string              `json:"experience"`
	ContactEmail      string              `json:"contactEmail"`
	ContactPhone      string              `json:"contactPhone"`
	ContactURL        string              `json:"contactUrl"`
	Visibility        Visibility          `json:"visibility"`
	DetailsVisibility Visibility          `json:"detailsVisibility"`
	ContactVisibility Visibility          `json:"contactVisibility"`
	Portfolio         []PortfolioDocument `json:"portfolio"`
}

// PortfolioDocument is one portfolio link over the API. The identifier is
// absent for a link not yet saved.
type PortfolioDocument struct {
	ID          *uuid.UUID `json:"id,omitempty"`
	Title       string     `json:"title"`
	URL         string     `json:"url"`
	Description string     `json:"description"`
}

// ExportCompany is a company the person has agreed to be listed by.
type ExportCompany struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
	Role string `json:"role"`
}

// Input turns a document into what Save takes.
func (d ProfileDocument) Input() ProfileInput {
	input := ProfileInput{
		Headline: d.Headline, Bio: d.Bio, Location: d.Location, Experience: d.Experience,
		ContactEmail: d.ContactEmail, ContactPhone: d.ContactPhone, ContactURL: d.ContactURL,
		Visibility: d.Visibility, DetailsVisibility: d.DetailsVisibility,
		ContactVisibility: d.ContactVisibility,
	}
	for _, item := range d.Portfolio {
		input.Portfolio = append(input.Portfolio, PortfolioInput(item))
	}
	return input
}

// Document turns somebody's own profile into the shape it is saved in.
func (p Profile) Document() ProfileDocument {
	document := ProfileDocument{
		Headline: p.Headline, Bio: p.Bio, Location: p.Location, Experience: p.Experience,
		ContactEmail: p.ContactEmail, ContactPhone: p.ContactPhone, ContactURL: p.ContactURL,
		Visibility: p.Visibility, DetailsVisibility: p.DetailsVisibility,
		ContactVisibility: p.ContactVisibility,
		Portfolio:         []PortfolioDocument{},
	}
	for _, item := range p.Portfolio {
		id := item.ID
		document.Portfolio = append(document.Portfolio, PortfolioDocument{
			ID: &id, Title: item.Title, URL: item.URL, Description: item.Description,
		})
	}
	return document
}

// Export returns somebody's own profile, whole, for them to keep.
func (s *Service) Export(ctx context.Context, userID uuid.UUID, server string) (ProfileExport, error) {
	profile, err := s.Own(ctx, userID)
	if err != nil {
		return ProfileExport{}, err
	}

	export := ProfileExport{
		Format:      ProfileExportFormat,
		ExportedAt:  time.Now().UTC().Format(time.RFC3339),
		Server:      server,
		Username:    profile.Username,
		DisplayName: profile.DisplayName,
		Profile:     profile.Document(),
		Companies:   []ExportCompany{},
		CreatedAt:   profile.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   profile.UpdatedAt.UTC().Format(time.RFC3339),
	}
	for _, company := range profile.Companies {
		export.Companies = append(export.Companies, ExportCompany(company))
	}
	return export, nil
}
