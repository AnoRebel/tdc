package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
Exporting the audit log.

Newline-delimited JSON, one entry per line. A single JSON array would have to
be held whole by whatever reads it, and a log is exactly the thing that outgrows
memory; a line at a time is streamable, appendable, and readable by ordinary
tools without a parser that understands the whole document.

Written straight to the response as each batch is read, rather than gathered
first. An export of a large log should not need the server to hold it either.
*/

// exportedEntry is one line of an export.
//
// Every field of the entry, since an export that dropped one would be a record
// somebody later discovers is incomplete — and this is the format an operator
// keeps when the log itself has been pruned.
type exportedEntry struct {
	ID         int64           `json:"id"`
	OccurredAt string          `json:"occurredAt"`
	Actor      string          `json:"actor"`
	ActorID    string          `json:"actorId,omitempty"`
	Action     string          `json:"action"`
	TargetKind string          `json:"targetKind,omitempty"`
	TargetID   string          `json:"targetId,omitempty"`
	SourceIP   string          `json:"sourceIp,omitempty"`
	UserAgent  string          `json:"userAgent,omitempty"`
	Detail     json.RawMessage `json:"detail,omitempty"`
}

func (s *Server) handleExportAudit(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)

	query := audit.Query{}
	if raw := c.QueryParam("since"); raw != "" {
		since, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest,
				"since must be a date and time")
		}
		query.Since = &since
	}
	if raw := c.QueryParam("until"); raw != "" {
		until, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest,
				"until must be a date and time")
		}
		query.Until = &until
	}

	/*
		Recorded before the export runs.

		Taking a copy of the whole log is exactly the sort of thing somebody
		later needs to account for — more so than reading a page of it, which
		is already recorded. Recording it first means an export that fails
		part way through still leaves the attempt on the record.
	*/
	if err := s.audit.Record(ctx, audit.Entry{
		ActorID:    &user.ID,
		ActorLabel: user.Username,
		Action:     audit.ActionAuditExported,
		SourceIP:   requestIP(c),
		UserAgent:  c.Request().UserAgent(),
	}); err != nil {
		return err
	}

	response := c.Response()
	response.Header().Set(echo.HeaderContentType, "application/x-ndjson")
	// Named so a browser saving it produces something recognisable rather than
	// the endpoint's last path segment.
	response.Header().Set(echo.HeaderContentDisposition,
		`attachment; filename="audit-log.ndjson"`)
	response.WriteHeader(http.StatusOK)

	encoder := json.NewEncoder(response)

	var after int64
	for {
		entries, next, err := s.audit.Export(ctx, after, 1000, query)
		if err != nil {
			/*
				The response has already begun, so there is no status left to
				change. Logged and stopped: a truncated export is detectable by
				whoever reads it, where a partial one silently presented as
				whole would not be.
			*/
			s.log.Error("audit export failed part way through", "error", err)
			return nil
		}

		for _, entry := range entries {
			if err := encoder.Encode(exportedEntryOf(entry)); err != nil {
				s.log.Error("cannot write an exported audit entry", "error", err)
				return nil
			}
		}

		/*
			Flushed per batch so a large export begins arriving at once rather
			than after the whole log has been read.

			Through the Flusher interface rather than a method on the response:
			Echo's writer does not expose one, and an implementation that
			cannot flush is not an error — it means the buffering is somebody
			else's, and the export still completes.
		*/
		if flusher, ok := response.(http.Flusher); ok {
			flusher.Flush()
		}

		if next == 0 {
			return nil
		}
		after = next
	}
}

func exportedEntryOf(entry dbgen.AuditEntry) exportedEntry {
	exported := exportedEntry{
		ID:         entry.ID,
		OccurredAt: entry.OccurredAt.UTC().Format(time.RFC3339Nano),
		Actor:      entry.ActorLabel,
		Action:     entry.Action,
		TargetKind: entry.TargetKind,
		TargetID:   entry.TargetID,
		UserAgent:  entry.UserAgent,
	}
	if entry.ActorID != nil {
		exported.ActorID = entry.ActorID.String()
	}
	if entry.SourceIp != nil {
		exported.SourceIP = entry.SourceIp.String()
	}
	if len(entry.Detail) > 0 && string(entry.Detail) != "{}" {
		exported.Detail = json.RawMessage(entry.Detail)
	}
	return exported
}
