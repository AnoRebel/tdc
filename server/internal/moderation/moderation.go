// Package moderation records reports of abuse and the decisions made about
// them.
//
// A report outlives what it is about. Somebody who posts something and deletes
// it should not thereby erase the complaint, so the reported text is copied
// into the report rather than referenced, and the event identifier is stored
// without a foreign key.
package moderation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

var (
	// ErrUnknownReason is returned for a reason outside the fixed set.
	ErrUnknownReason = errors.New("that is not a reason a report can be filed under")
	// ErrTooManyReports is returned when somebody is filing faster than any
	// person plausibly reads.
	ErrTooManyReports = errors.New("too many reports filed recently; try again later")
	// ErrReportNotFound is returned for a report that does not exist or has
	// already been decided.
	ErrReportNotFound = errors.New("no such open report")
)

/*
Reason is why something was reported.

A fixed set rather than free text, because a queue somebody has to work
through needs to be sortable and groupable — twenty reports of the same
harassment should be visible as one problem. The free-text detail is kept
alongside for what the categories cannot express.
*/
type Reason string

const (
	ReasonSpam       Reason = "spam"
	ReasonHarassment Reason = "harassment"
	ReasonHate       Reason = "hate"
	ReasonViolence   Reason = "violence"
	ReasonSexual     Reason = "sexual"
	ReasonIllegal    Reason = "illegal"
	ReasonOther      Reason = "other"
)

// Reasons is every reason a report may be filed under, for a client to offer.
func Reasons() []Reason {
	return []Reason{
		ReasonSpam, ReasonHarassment, ReasonHate,
		ReasonViolence, ReasonSexual, ReasonIllegal, ReasonOther,
	}
}

func knownReason(reason Reason) bool {
	for _, known := range Reasons() {
		if reason == known {
			return true
		}
	}
	return false
}

/*
reportBurst bounds how many reports one person may file in an hour.

Reporting has to be easy, so this is deliberately loose: it stops somebody
burying a moderation queue under hundreds of reports, and is far above what
anybody encountering real problems would file.
*/
const (
	reportBurst  = 20
	reportWindow = time.Hour
)

// maxDetail bounds the free-text part of a report.
const maxDetail = 2000

// Report is a filed complaint.
type Report struct {
	ID             uuid.UUID
	RoomID         *uuid.UUID
	EventID        string
	SubjectAddress string
	Reporter       string
	Reason         Reason
	Detail         string
	ReportedBody   string
	Status         string
	Resolution     string
	CreatedAt      int64
}

// Service files and resolves reports.
type Service struct {
	queries *dbgen.Queries
	log     *slog.Logger
}

func NewService(pool *db.Pool, log *slog.Logger) *Service {
	return &Service{queries: dbgen.New(pool), log: log}
}

// Filing is what somebody is reporting.
type Filing struct {
	RoomID  *uuid.UUID
	EventID string
	// Subject is who is being reported, as an address.
	Subject string
	Reason  Reason
	Detail  string
	// ReportedBody is a copy of what was reported, taken at filing time so the
	// report still means something after the message is deleted.
	ReportedBody string
}

/*
File records a report.

The reported text is copied rather than referenced. Moderation happens after
the fact, often after the author has noticed and deleted what they wrote, and a
report that says only "message $abc, now missing" cannot be acted on.
*/
func (s *Service) File(
	ctx context.Context,
	reporterID uuid.UUID,
	reporterAddress string,
	filing Filing,
) (Report, error) {
	if !knownReason(filing.Reason) {
		return Report{}, ErrUnknownReason
	}

	recent, err := s.queries.CountRecentReportsBy(ctx, dbgen.CountRecentReportsByParams{
		ReporterID: &reporterID,
		Column2:    pgtype.Interval{Microseconds: reportWindow.Microseconds(), Valid: true},
	})
	if err != nil {
		return Report{}, fmt.Errorf("moderation: counting recent reports: %w", err)
	}
	if recent >= reportBurst {
		return Report{}, ErrTooManyReports
	}

	detail := strings.TrimSpace(filing.Detail)
	if len([]rune(detail)) > maxDetail {
		detail = string([]rune(detail)[:maxDetail])
	}

	row, err := s.queries.CreateReport(ctx, dbgen.CreateReportParams{
		ID:              uuid.New(),
		RoomID:          filing.RoomID,
		EventID:         optional(filing.EventID),
		SubjectAddress:  filing.Subject,
		ReporterID:      &reporterID,
		ReporterAddress: reporterAddress,
		Reason:          string(filing.Reason),
		Detail:          detail,
		ReportedBody:    truncate(filing.ReportedBody, 4000),
	})
	if err != nil {
		return Report{}, fmt.Errorf("moderation: filing a report: %w", err)
	}

	// Logged as well as stored, so a server whose moderators are not watching
	// the queue still leaves a trail.
	s.log.Info("abuse reported",
		"reportId", row.ID, "reason", row.Reason, "subject", row.SubjectAddress)

	return reportOf(row), nil
}

// Open lists reports nobody has decided yet, oldest first.
func (s *Service) Open(ctx context.Context, limit int32) ([]Report, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	rows, err := s.queries.ListOpenReports(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("moderation: reading the queue: %w", err)
	}

	reports := make([]Report, 0, len(rows))
	for _, row := range rows {
		reports = append(reports, reportOf(row))
	}
	return reports, nil
}

// Count is the number of open reports, for a badge.
func (s *Service) Count(ctx context.Context) (int64, error) {
	count, err := s.queries.CountOpenReports(ctx)
	if err != nil {
		return 0, fmt.Errorf("moderation: counting open reports: %w", err)
	}
	return count, nil
}

/*
Resolve records a decision.

Only an open report can be decided, which is what stops two moderators acting
on the same report from both recording an outcome — the second gets
ErrReportNotFound and can see what the first decided.
*/
func (s *Service) Resolve(
	ctx context.Context,
	reportID, moderatorID uuid.UUID,
	dismissed bool,
	resolution string,
) (Report, error) {
	status := "resolved"
	if dismissed {
		status = "dismissed"
	}

	row, err := s.queries.ResolveReport(ctx, dbgen.ResolveReportParams{
		ID:         reportID,
		Status:     status,
		ResolvedBy: &moderatorID,
		Resolution: truncate(strings.TrimSpace(resolution), maxDetail),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Report{}, ErrReportNotFound
	}
	if err != nil {
		return Report{}, fmt.Errorf("moderation: resolving a report: %w", err)
	}

	return reportOf(row), nil
}

// BodyOf pulls the readable text out of a message, for the copy kept in a
// report.
func BodyOf(content []byte) string {
	var parsed struct {
		Body string `json:"body"`
	}
	if json.Unmarshal(content, &parsed) != nil {
		return ""
	}
	return parsed.Body
}

func reportOf(row dbgen.Report) Report {
	report := Report{
		ID:             row.ID,
		RoomID:         row.RoomID,
		SubjectAddress: row.SubjectAddress,
		Reporter:       row.ReporterAddress,
		Reason:         Reason(row.Reason),
		Detail:         row.Detail,
		ReportedBody:   row.ReportedBody,
		Status:         row.Status,
		Resolution:     row.Resolution,
		CreatedAt:      row.CreatedAt.UnixMilli(),
	}
	if row.EventID != nil {
		report.EventID = *row.EventID
	}
	return report
}

func optional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

/*
FileAutomatically records a report the server raised itself.

Separate from File because the two differ in ways that matter. There is no
reporter, so the per-reporter rate limit does not apply and would divide by
nobody if it did. And the reason a filter matched is not a person's judgement:
it is a fact about the text, which is why the detail says which words hit
rather than what somebody thought of them.

The queue is shared with people's own reports on purpose. A separate queue for
automatic flags is a queue somebody stops reading, and then the filter is
decorative.
*/
func (s *Service) FileAutomatically(ctx context.Context, filing Filing) (Report, error) {
	if !knownReason(filing.Reason) {
		return Report{}, ErrUnknownReason
	}

	detail := strings.TrimSpace(filing.Detail)
	if len([]rune(detail)) > maxDetail {
		detail = string([]rune(detail)[:maxDetail])
	}

	row, err := s.queries.CreateReport(ctx, dbgen.CreateReportParams{
		ID:      uuid.New(),
		RoomID:  filing.RoomID,
		EventID: optional(filing.EventID),
		// A nil reporter is what marks this as the server's own doing. The
		// column is nullable for exactly this.
		ReporterID:      nil,
		ReporterAddress: AutomaticReporter,
		SubjectAddress:  filing.Subject,
		Reason:          string(filing.Reason),
		Detail:          detail,
		ReportedBody:    truncate(filing.ReportedBody, 4000),
	})
	if err != nil {
		return Report{}, fmt.Errorf("moderation: filing an automatic report: %w", err)
	}

	s.log.Info("content filter flagged a message",
		"reportId", row.ID, "subject", row.SubjectAddress)

	return reportOf(row), nil
}

/*
AutomaticReporter is what stands in for a reporter when the server raised the
report itself.

A recognisable label rather than an empty string, so a moderator reading the
queue can tell "the filter caught this" from "somebody reported this" — which
changes how much weight the report deserves.
*/
const AutomaticReporter = "content filter"
