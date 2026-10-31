package directory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

var (
	// ErrNoSkill reports a skill that does not exist, or is not the caller's
	// to see.
	ErrNoSkill = errors.New("no such skill")
	// ErrNotProposed reports a review decision about something that is not
	// waiting for one.
	ErrNotProposed = errors.New("that skill is not waiting for review")
)

const (
	maxSkillName        = 60
	maxSkillDescription = 300
	maxProfileSkills    = 30
)

// SkillStatus says whether a skill is in the vocabulary or waiting to be.
type SkillStatus string

const (
	// SkillActive is in the vocabulary: attachable by anybody, and searchable.
	SkillActive SkillStatus = "active"
	// SkillProposed is waiting for review: attachable only by whoever proposed
	// it, and not searchable.
	SkillProposed SkillStatus = "proposed"
)

// Skill is one entry in the vocabulary, or one waiting for it.
type Skill struct {
	ID          uuid.UUID
	Name        string
	Description string
	Status      SkillStatus
}

// SkillClaim is a skill on somebody's profile, with what they say about it.
type SkillClaim struct {
	ID          uuid.UUID
	Name        string
	Status      SkillStatus
	Description string
	UpdatedAt   time.Time
}

// ProposedSkill is one entry in the review queue.
type ProposedSkill struct {
	ID          uuid.UUID
	Name        string
	Description string
	ProposedBy  string
	// Profiles is how many profiles carry it already, which is what a merge
	// would move.
	Profiles  int64
	CreatedAt time.Time
}

/*
FoldSkillName is the form two spellings are compared in.

Lower-cased, with runs of space collapsed: "Node.js", "node.js" and
"node.js  " are one name. Anything further — "golang" meaning "Go" — is an
alias, which is a reviewer's judgement rather than a rule this function could
apply.
*/
func FoldSkillName(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

func skillOf(row dbgen.Skill) Skill {
	return Skill{ID: row.ID, Name: row.Name, Description: row.Description, Status: SkillStatus(row.Status)}
}

// Skills lists the vocabulary matching what somebody typed, with their own
// proposals, which only they may attach.
func (s *Service) Skills(ctx context.Context, viewer uuid.UUID, query string) ([]Skill, error) {
	rows, err := s.queries.ListSkills(ctx, dbgen.ListSkillsParams{
		Viewer: viewer, Query: strings.TrimSpace(query), ResultLimit: 50,
	})
	if err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}
	skills := make([]Skill, 0, len(rows))
	for _, row := range rows {
		skills = append(skills, skillOf(row))
	}
	return skills, nil
}

/*
ProposeSkill asks for a skill to be added to the vocabulary.

A name that already means something — the skill itself, or a known other
spelling of it — returns that skill rather than a second entry, which is the
fragmentation a controlled vocabulary exists to stop. Returns whether a new
entry was made.

Somebody who may manage the vocabulary adds it directly: there is nobody for
them to ask. Everybody else's proposal waits in the queue, attachable to their
own profile and searchable by nobody.
*/
func (s *Service) ProposeSkill(ctx context.Context, proposer uuid.UUID, name, description string, manager bool) (Skill, bool, error) {
	name = strings.Join(strings.Fields(name), " ")
	description = strings.TrimSpace(description)
	switch {
	case name == "":
		return Skill{}, false, invalid("name", "is required")
	case utf8.RuneCountInString(name) > maxSkillName:
		return Skill{}, false, invalid("name", fmt.Sprintf("at most %d characters", maxSkillName))
	case utf8.RuneCountInString(description) > maxSkillDescription:
		return Skill{}, false, invalid("description", fmt.Sprintf("at most %d characters", maxSkillDescription))
	}

	folded := FoldSkillName(name)
	existing, err := s.queries.ResolveSkill(ctx, folded)
	if err == nil {
		return skillOf(existing), false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Skill{}, false, fmt.Errorf("look up a skill: %w", err)
	}

	status := SkillProposed
	var reviewer *uuid.UUID
	if manager {
		status = SkillActive
		reviewer = &proposer
	}
	row, err := s.queries.InsertSkill(ctx, dbgen.InsertSkillParams{
		ID: uuid.New(), Name: name, NameFolded: folded, Description: description,
		Status: string(status), ProposedBy: &proposer, ReviewedBy: reviewer,
	})
	if isUniqueViolation(err) {
		// Somebody proposed the same name a moment ago.
		existing, err := s.queries.ResolveSkill(ctx, folded)
		if err != nil {
			return Skill{}, false, fmt.Errorf("look up a skill: %w", err)
		}
		return skillOf(existing), false, nil
	}
	if err != nil {
		return Skill{}, false, fmt.Errorf("propose a skill: %w", err)
	}
	return skillOf(row), true, nil
}

// SkillQueue is every proposal waiting for a decision, oldest first.
func (s *Service) SkillQueue(ctx context.Context) ([]ProposedSkill, error) {
	rows, err := s.queries.ListSkillQueue(ctx)
	if err != nil {
		return nil, fmt.Errorf("list the skill queue: %w", err)
	}
	queue := make([]ProposedSkill, 0, len(rows))
	for _, row := range rows {
		queue = append(queue, ProposedSkill{
			ID: row.ID, Name: row.Name, Description: row.Description,
			ProposedBy: row.ProposedByUsername, Profiles: row.Profiles, CreatedAt: row.CreatedAt,
		})
	}
	return queue, nil
}

// ApproveSkill puts a proposal in the vocabulary, where anybody may attach it
// and search for it.
func (s *Service) ApproveSkill(ctx context.Context, id, reviewer uuid.UUID) (Skill, error) {
	approved, err := s.queries.ApproveSkill(ctx, dbgen.ApproveSkillParams{ID: id, Reviewer: &reviewer})
	if err != nil {
		return Skill{}, fmt.Errorf("approve a skill: %w", err)
	}
	if approved == 0 {
		return Skill{}, ErrNotProposed
	}
	row, err := s.queries.GetSkill(ctx, id)
	if err != nil {
		return Skill{}, fmt.Errorf("read a skill: %w", err)
	}
	return skillOf(row), nil
}

// RejectSkill removes a proposal, and it leaves every profile it was on.
func (s *Service) RejectSkill(ctx context.Context, id uuid.UUID) (Skill, error) {
	row, err := s.queries.GetSkill(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Skill{}, ErrNotProposed
	}
	if err != nil {
		return Skill{}, fmt.Errorf("read a skill: %w", err)
	}
	removed, err := s.queries.DeleteProposedSkill(ctx, id)
	if err != nil {
		return Skill{}, fmt.Errorf("reject a skill: %w", err)
	}
	if removed == 0 {
		return Skill{}, ErrNotProposed
	}
	return skillOf(row), nil
}

/*
MergeSkill decides a proposal is a skill the vocabulary already has.

Its name becomes another spelling of that skill, so the next person who types
it lands on the one entry; everybody who attached the proposal has the skill
instead, keeping what they wrote about it; and the proposal goes.
*/
func (s *Service) MergeSkill(ctx context.Context, id, into uuid.UUID) (Skill, Skill, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Skill{}, Skill{}, fmt.Errorf("begin merging a skill: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)

	proposal, err := queries.GetSkill(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && proposal.Status != string(SkillProposed)) {
		return Skill{}, Skill{}, ErrNotProposed
	}
	if err != nil {
		return Skill{}, Skill{}, fmt.Errorf("read a skill: %w", err)
	}
	target, err := queries.GetSkill(ctx, into)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && target.Status != string(SkillActive)) {
		return Skill{}, Skill{}, invalid("into", "must be a skill in the vocabulary")
	}
	if err != nil {
		return Skill{}, Skill{}, fmt.Errorf("read a skill: %w", err)
	}

	if err := queries.MoveProfileSkills(ctx, dbgen.MoveProfileSkillsParams{
		MergedInto: target.ID, MergedFrom: proposal.ID,
	}); err != nil {
		return Skill{}, Skill{}, fmt.Errorf("move a skill's profiles: %w", err)
	}
	if _, err := queries.DeleteProposedSkill(ctx, proposal.ID); err != nil {
		return Skill{}, Skill{}, fmt.Errorf("remove a merged proposal: %w", err)
	}
	// After the delete, since the proposal's own name was unique until then.
	if err := queries.AddSkillAlias(ctx, dbgen.AddSkillAliasParams{
		AliasFolded: proposal.NameFolded, SkillID: target.ID,
	}); err != nil {
		return Skill{}, Skill{}, fmt.Errorf("record another spelling: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Skill{}, Skill{}, fmt.Errorf("commit a merge: %w", err)
	}
	return skillOf(proposal), skillOf(target), nil
}

// SkillInput is one skill somebody puts on their profile: by identifier, or
// by a name the vocabulary knows.
type SkillInput struct {
	ID          *uuid.UUID
	Name        string
	Description string
}

// resolveSkills turns what somebody sent into skills they may attach, in
// order and without repeats.
func (s *Service) resolveSkills(ctx context.Context, queries *dbgen.Queries, userID uuid.UUID, inputs []SkillInput) ([]SkillInput, error) {
	if len(inputs) > maxProfileSkills {
		return nil, invalid("skills", fmt.Sprintf("at most %d skills", maxProfileSkills))
	}

	seen := map[uuid.UUID]bool{}
	resolved := make([]SkillInput, 0, len(inputs))
	for index, input := range inputs {
		field := fmt.Sprintf("skills[%d]", index)
		input.Description = strings.TrimSpace(input.Description)
		if utf8.RuneCountInString(input.Description) > maxSkillDescription {
			return nil, invalid(field+".description", fmt.Sprintf("at most %d characters", maxSkillDescription))
		}

		if input.ID == nil {
			row, err := queries.ResolveSkill(ctx, FoldSkillName(input.Name))
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, invalid(field, fmt.Sprintf("%q is not in the vocabulary; propose it first", input.Name))
			}
			if err != nil {
				return nil, fmt.Errorf("look up a skill: %w", err)
			}
			input.ID = &row.ID
		}

		attachable, err := queries.SkillAttachable(ctx, dbgen.SkillAttachableParams{SkillID: *input.ID, UserID: userID})
		if err != nil {
			return nil, fmt.Errorf("check a skill: %w", err)
		}
		if !attachable {
			// One wording for "no such skill" and "somebody else's proposal",
			// so the queue is not readable by guessing identifiers.
			return nil, invalid(field, "is not a skill you can add")
		}

		if seen[*input.ID] {
			continue
		}
		seen[*input.ID] = true
		resolved = append(resolved, input)
	}
	return resolved, nil
}

// saveSkills replaces the skills on a profile with those resolved.
func saveSkills(ctx context.Context, queries *dbgen.Queries, userID uuid.UUID, skills []SkillInput) error {
	keep := make([]uuid.UUID, 0, len(skills))
	for position, skill := range skills {
		if err := queries.SaveProfileSkill(ctx, dbgen.SaveProfileSkillParams{
			UserID: userID, SkillID: *skill.ID, Description: skill.Description, Position: int32(position),
		}); err != nil {
			return fmt.Errorf("save a profile skill: %w", err)
		}
		keep = append(keep, *skill.ID)
	}
	if err := queries.DeleteProfileSkillsExcept(ctx, dbgen.DeleteProfileSkillsExceptParams{
		UserID: userID, Keep: keep,
	}); err != nil {
		return fmt.Errorf("remove profile skills: %w", err)
	}
	return nil
}
