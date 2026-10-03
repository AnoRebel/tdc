package directory

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

var (
	// ErrNoCompany reports a company the caller may not see, or that does
	// not exist — one answer for both.
	ErrNoCompany = errors.New("no such company")
	// ErrNotCompanyAdministrator reports somebody changing a company they do
	// not administer.
	ErrNotCompanyAdministrator = errors.New("only a company's administrators can do that")
	// ErrCompanyTaken reports a name another company already uses.
	ErrCompanyTaken = errors.New("that company address is already taken")
	// ErrNotAssociated reports an association that does not exist.
	ErrNotAssociated = errors.New("there is no such association")
	// ErrLastAdministrator reports a change that would leave a company nobody
	// can administer.
	ErrLastAdministrator = errors.New("a company must keep at least one administrator; delete it instead")
)

// companySlug is what a company's address may look like.
var companySlug = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,38}[a-z0-9])?$`)

const (
	maxCompanyName        = 80
	maxCompanyDescription = 2000
	maxCompanyRole        = 60
)

// CompanyInput is what a company's administrators may say about it.
type CompanyInput struct {
	Slug        string
	Name        string
	Description string
	Location    string
	Visibility  Visibility
}

// CompanyMember is somebody listed by a company.
type CompanyMember struct {
	UserID        uuid.UUID
	Username      string
	DisplayName   string
	Role          string
	Administrator bool
	// Since is when they accepted, or when they were asked while pending.
	Since time.Time
}

// Company is a company as one viewer may see it.
type Company struct {
	ID          uuid.UUID
	Slug        string
	Name        string
	Description string
	Location    string
	Visibility  Visibility
	Members     []CompanyMember
	// Pending is filled only for the company's administrators.
	Pending []CompanyMember
	// Administered says the viewer may change the company.
	Administered bool
}

// Association is one company somebody is, or has been asked to be, part of.
type Association struct {
	Slug          string
	Name          string
	Role          string
	Administrator bool
	Pending       bool
	InvitedAt     time.Time
	AcceptedAt    *time.Time
}

func normaliseCompany(input *CompanyInput) error {
	input.Slug = strings.ToLower(strings.TrimSpace(input.Slug))
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.Location = strings.TrimSpace(input.Location)
	if input.Visibility == "" {
		input.Visibility = Members
	}

	switch {
	case !companySlug.MatchString(input.Slug):
		return invalid("slug", "lower-case letters, digits and hyphens, up to 40")
	case input.Name == "":
		return invalid("name", "is required")
	case utf8.RuneCountInString(input.Name) > maxCompanyName:
		return invalid("name", fmt.Sprintf("at most %d characters", maxCompanyName))
	case utf8.RuneCountInString(input.Description) > maxCompanyDescription:
		return invalid("description", fmt.Sprintf("at most %d characters", maxCompanyDescription))
	case utf8.RuneCountInString(input.Location) > maxLocation:
		return invalid("location", fmt.Sprintf("at most %d characters", maxLocation))
	case input.Visibility != Public && input.Visibility != Members:
		return invalid("visibility", `must be "public" or "members"`)
	}
	return nil
}

// CreateCompany makes a company with its creator as its first administrator.
func (s *Service) CreateCompany(ctx context.Context, creator uuid.UUID, role string, input CompanyInput) (Company, error) {
	if err := normaliseCompany(&input); err != nil {
		return Company{}, err
	}
	role, err := normaliseRole(role)
	if err != nil {
		return Company{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Company{}, fmt.Errorf("begin creating a company: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)

	row, err := queries.CreateCompanyProfile(ctx, dbgen.CreateCompanyProfileParams{
		ID:          uuid.New(),
		Slug:        input.Slug,
		Name:        input.Name,
		Description: input.Description,
		Location:    input.Location,
		Visibility:  string(input.Visibility),
		CreatedBy:   &creator,
	})
	if isUniqueViolation(err) {
		return Company{}, ErrCompanyTaken
	}
	if err != nil {
		return Company{}, fmt.Errorf("create a company: %w", err)
	}

	if err := queries.AddCompanyFounder(ctx, dbgen.AddCompanyFounderParams{
		CompanyID: row.ID, UserID: creator, Role: role,
	}); err != nil {
		return Company{}, fmt.Errorf("add a company's founder: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Company{}, fmt.Errorf("commit a company: %w", err)
	}
	return s.ViewCompany(ctx, row.Slug, Owner, creator)
}

/*
ViewCompany returns a company as a viewer may see it.

viewer is the signed-in person, or uuid.Nil for somebody who is not. Only
accepted members are ever listed; the pending ones are shown to the company's
own administrators, who sent the invitations and so learn nothing new.
*/
func (s *Service) ViewCompany(ctx context.Context, slug string, rank Rank, viewer uuid.UUID) (Company, error) {
	row, err := s.queries.ViewCompanyProfile(ctx, dbgen.ViewCompanyProfileParams{
		Slug: slug, ViewerRank: int16(rank),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Company{}, ErrNoCompany
	}
	if err != nil {
		return Company{}, fmt.Errorf("read a company: %w", err)
	}

	company := Company{
		ID: row.ID, Slug: row.Slug, Name: row.Name, Description: row.Description,
		Location: row.Location, Visibility: Visibility(row.Visibility),
	}

	members, err := s.queries.ListAcceptedCompanyMembers(ctx, row.ID)
	if err != nil {
		return Company{}, fmt.Errorf("list a company's members: %w", err)
	}
	for _, member := range members {
		since := time.Time{}
		if member.AcceptedAt != nil {
			since = *member.AcceptedAt
		}
		company.Members = append(company.Members, CompanyMember{
			UserID: member.ID, Username: member.Username, DisplayName: member.DisplayName,
			Role: member.Role, Administrator: member.Administrator, Since: since,
		})
		if member.ID == viewer && member.Administrator {
			company.Administered = true
		}
	}

	if company.Administered {
		pending, err := s.queries.ListPendingCompanyMembers(ctx, row.ID)
		if err != nil {
			return Company{}, fmt.Errorf("list a company's invitations: %w", err)
		}
		for _, member := range pending {
			company.Pending = append(company.Pending, CompanyMember{
				UserID: member.ID, Username: member.Username, DisplayName: member.DisplayName,
				Role: member.Role, Since: member.InvitedAt,
			})
		}
	}

	return company, nil
}

// administered returns a company the actor administers, whatever its
// visibility, or the reason they may not change it.
func (s *Service) administered(ctx context.Context, slug string, actor uuid.UUID) (dbgen.CompanyProfile, error) {
	row, err := s.queries.GetCompanyBySlug(ctx, slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.CompanyProfile{}, ErrNoCompany
	}
	if err != nil {
		return dbgen.CompanyProfile{}, fmt.Errorf("read a company: %w", err)
	}

	membership, err := s.queries.GetCompanyMembership(ctx, dbgen.GetCompanyMembershipParams{
		CompanyID: row.ID, UserID: actor,
	})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (!membership.Administrator || membership.Status != "accepted")) {
		return dbgen.CompanyProfile{}, ErrNotCompanyAdministrator
	}
	if err != nil {
		return dbgen.CompanyProfile{}, fmt.Errorf("read a company membership: %w", err)
	}
	return row, nil
}

// UpdateCompany changes what a company says about itself. Its address does
// not change: links to it would break, and a new address is a new company.
func (s *Service) UpdateCompany(ctx context.Context, actor uuid.UUID, slug string, input CompanyInput) (Company, error) {
	row, err := s.administered(ctx, slug, actor)
	if err != nil {
		return Company{}, err
	}
	input.Slug = row.Slug
	if err := normaliseCompany(&input); err != nil {
		return Company{}, err
	}

	if _, err := s.queries.UpdateCompanyProfile(ctx, dbgen.UpdateCompanyProfileParams{
		ID: row.ID, Name: input.Name, Description: input.Description,
		Location: input.Location, Visibility: string(input.Visibility),
	}); err != nil {
		return Company{}, fmt.Errorf("update a company: %w", err)
	}
	return s.ViewCompany(ctx, row.Slug, Owner, actor)
}

// DeleteCompany removes a company and every association with it.
func (s *Service) DeleteCompany(ctx context.Context, actor uuid.UUID, slug string) error {
	row, err := s.administered(ctx, slug, actor)
	if err != nil {
		return err
	}
	if err := s.queries.DeleteCompanyProfile(ctx, row.ID); err != nil {
		return fmt.Errorf("delete a company: %w", err)
	}
	return nil
}

/*
InviteToCompany asks somebody to be listed by a company.

Nothing is displayed until they accept. Returns ErrNotAssociated when there is
no such person — the inviting administrator already knows who is on this
server, since member search tells any member as much.
*/
func (s *Service) InviteToCompany(ctx context.Context, actor uuid.UUID, slug string, invitee uuid.UUID, role string) error {
	row, err := s.administered(ctx, slug, actor)
	if err != nil {
		return err
	}
	role, err = normaliseRole(role)
	if err != nil {
		return err
	}
	if _, err := s.queries.InviteCompanyMember(ctx, dbgen.InviteCompanyMemberParams{
		CompanyID: row.ID, UserID: invitee, Role: role, InvitedBy: &actor,
	}); err != nil {
		return fmt.Errorf("invite to a company: %w", err)
	}
	return nil
}

// AcceptCompany is somebody agreeing to be listed by a company that asked.
func (s *Service) AcceptCompany(ctx context.Context, userID uuid.UUID, slug string) error {
	row, err := s.queries.GetCompanyBySlug(ctx, slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotAssociated
	}
	if err != nil {
		return fmt.Errorf("read a company: %w", err)
	}
	accepted, err := s.queries.AcceptCompanyMembership(ctx, dbgen.AcceptCompanyMembershipParams{
		CompanyID: row.ID, UserID: userID,
	})
	if err != nil {
		return fmt.Errorf("accept a company: %w", err)
	}
	if accepted == 0 {
		return ErrNotAssociated
	}
	return nil
}

/*
LeaveCompany ends somebody's association with a company, from either side.

The person themselves may always leave, decline an invitation, or withdraw a
pending one — their name is theirs. An administrator may remove anybody else.
The last administrator cannot leave a company with other people in it
unadministered; they delete it instead, which is the honest version of what
they are doing.
*/
func (s *Service) LeaveCompany(ctx context.Context, actor uuid.UUID, slug string, member uuid.UUID) error {
	row, err := s.queries.GetCompanyBySlug(ctx, slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotAssociated
	}
	if err != nil {
		return fmt.Errorf("read a company: %w", err)
	}

	if actor != member {
		if _, err := s.administered(ctx, slug, actor); err != nil {
			return err
		}
	}

	membership, err := s.queries.GetCompanyMembership(ctx, dbgen.GetCompanyMembershipParams{
		CompanyID: row.ID, UserID: member,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotAssociated
	}
	if err != nil {
		return fmt.Errorf("read a company membership: %w", err)
	}

	if membership.Administrator && membership.Status == "accepted" {
		administrators, err := s.queries.CountCompanyAdministrators(ctx, row.ID)
		if err != nil {
			return fmt.Errorf("count a company's administrators: %w", err)
		}
		if administrators <= 1 {
			return ErrLastAdministrator
		}
	}

	if _, err := s.queries.RemoveCompanyMember(ctx, dbgen.RemoveCompanyMemberParams{
		CompanyID: row.ID, UserID: member,
	}); err != nil {
		return fmt.Errorf("leave a company: %w", err)
	}

	/*
		A verified association ends with the association. Cleared rather than
		left to be hidden by the claim's absence, so that joining again later
		does not bring back an attestation about an association that ended.
	*/
	if _, err := clearStale(ctx, s.queries, member); err != nil && !errors.Is(err, ErrNoProfile) {
		return err
	}
	return nil
}

// Associations lists somebody's own companies, including invitations they
// have not answered.
func (s *Service) Associations(ctx context.Context, userID uuid.UUID) ([]Association, error) {
	rows, err := s.queries.ListMyCompanies(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list companies: %w", err)
	}
	associations := make([]Association, 0, len(rows))
	for _, row := range rows {
		associations = append(associations, Association{
			Slug: row.Slug, Name: row.Name, Role: row.Role, Administrator: row.Administrator,
			Pending: row.Status == "pending", InvitedAt: row.InvitedAt, AcceptedAt: row.AcceptedAt,
		})
	}
	return associations, nil
}

func normaliseRole(role string) (string, error) {
	role = strings.TrimSpace(role)
	if utf8.RuneCountInString(role) > maxCompanyRole {
		return "", invalid("role", fmt.Sprintf("at most %d characters", maxCompanyRole))
	}
	return role, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
