// Package audit records security- and administration-relevant actions.
//
// The log answers who did what and when. It is append-only: no interface
// exists to edit or delete an individual entry, so an administrator cannot
// quietly remove the record of their own action.
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"time"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

// Action names what happened. They are namespaced by area so that a reader can
// tell at a glance what was attempted.
type Action string

const (
	ActionSignInSucceeded Action = "auth.signin.succeeded"
	ActionSignInFailed    Action = "auth.signin.failed"
	ActionSignedOut       Action = "auth.signout"
	ActionSessionRevoked  Action = "auth.session.revoked"
	ActionPasswordChanged Action = "auth.password.changed"
	ActionRateLimited     Action = "auth.rate_limited"

	ActionUserCreated    Action = "user.created"
	ActionUserSuspended  Action = "user.suspended"
	ActionUserReinstated Action = "user.reinstated"

	// Moderation. A block is deliberately absent: it is a personal boundary
	// rather than an exercise of authority, and a log a moderator reads should
	// not turn it into one.
	ActionChannelBan        Action = "channel.ban"
	ActionChannelBanLifted  Action = "channel.ban.lifted"
	ActionPostingRestricted Action = "posting.restricted"
	ActionRestrictionLifted Action = "posting.restriction.lifted"

	// Invitations: who issued one, who accepted it, and who withdrew it. The
	// secret itself is never recorded — a log holding working invitations
	// would be a second place they leak from.
	ActionInvitationIssued   Action = "invitation.issued"
	ActionInvitationAccepted Action = "invitation.accepted"
	ActionInvitationRevoked  Action = "invitation.revoked"

	// Server configuration an operator changes while it runs.
	ActionSettingChanged Action = "server.setting.changed"

	// ActionMediaMarked and ActionMediaUnmarked record a decision about
	// whether media arrives obscured. Audited because a mark somebody disputes
	// needs a trail — particularly a moderator's, which the uploader cannot
	// lift.
	ActionMediaMarked   Action = "media.marked_sensitive"
	ActionMediaUnmarked Action = "media.unmarked_sensitive"

	/*
		ActionRetentionShortened records a change that makes existing content
		eligible for removal.

		Distinct from an ordinary setting change because it destroys rather
		than configures. Somebody asking why a year of messages vanished should
		find this entry, not have to infer it from two numbers in a generic
		one.
	*/
	ActionRetentionShortened Action = "server.retention.shortened"

	// ActionMessageLifetimeSet records a moderator putting an expiry on
	// somebody else's message, which is deletion on a delay and deserves the
	// same trail deletion gets.
	ActionMessageLifetimeSet Action = "message.lifetime_set"

	/*
		ActionMessageRemoved records a moderator deleting somebody else's
		message.

		Only a moderator's deletions. Somebody removing their own message is
		ordinary use of the application, and recording every one of those would
		bury the deletions that were an exercise of authority — which are the
		ones anybody reviewing the log is looking for.
	*/
	ActionMessageRemoved Action = "message.removed"

	// ActionAuditPruned records the log removing its own oldest entries, so a
	// gap in it is explained rather than merely present.
	ActionAuditPruned Action = "server.audit.pruned"

	// ActionAuditExported records somebody taking a copy of the whole log,
	// which is more worth accounting for than reading a page of it.
	ActionAuditExported Action = "server.audit.exported"

	ActionRoleCreated       Action = "role.created"
	ActionRoleDeleted       Action = "role.deleted"
	ActionRoleAssigned      Action = "role.assigned"
	ActionRoleRemoved       Action = "role.removed"
	ActionPermissionChanged Action = "role.permission.changed"

	ActionConfigChanged Action = "server.config.changed"
	ActionSetupComplete Action = "server.setup.completed"

	// Reading the log is itself audited, so that access to the record is part
	// of the record.
	ActionAuditRead Action = "server.audit.read"
)

// Entry describes an action to record.
type Entry struct {
	// ActorID is nil when the system acted, or when the caller was
	// unauthenticated — a failed sign-in has no established identity.
	ActorID *uuid.UUID
	// ActorLabel survives the account being deleted, so an old entry stays
	// meaningful. For an unauthenticated action it holds the identifier that
	// was attempted.
	ActorLabel string
	Action     Action
	// TargetKind and TargetID reference what was acted upon, by identifier
	// rather than by copying content, so the log does not become a second
	// store of message text.
	TargetKind string
	TargetID   string
	SourceIP   netip.Addr
	UserAgent  string
	// Detail carries structured context such as the previous and new value of
	// a setting. It must never contain credentials or message content.
	Detail map[string]any
}

// Store is the database surface the recorder needs.
type Store interface {
	AppendAuditEntry(ctx context.Context, arg dbgen.AppendAuditEntryParams) (dbgen.AuditEntry, error)
	ListAuditEntries(ctx context.Context, arg dbgen.ListAuditEntriesParams) ([]dbgen.AuditEntry, error)
	DeleteAuditEntriesBefore(ctx context.Context, occurredAt time.Time) (int64, error)
	// Reading the span before removing it is what lets the entry recording a
	// removal say what the gap covers.
	CountAuditEntriesBefore(ctx context.Context, occurredAt time.Time) (dbgen.CountAuditEntriesBeforeRow, error)
	ExportAuditEntries(ctx context.Context, arg dbgen.ExportAuditEntriesParams) ([]dbgen.AuditEntry, error)
}

// Recorder appends entries to the audit log.
type Recorder struct {
	store Store
	log   *slog.Logger
}

// NewRecorder builds a recorder over the given store.
func NewRecorder(store Store, log *slog.Logger) *Recorder {
	return &Recorder{store: store, log: log}
}

// ErrNotRecorded reports that an entry could not be written.
//
// A privileged action whose record cannot be written must be refused rather
// than performed unrecorded, so callers are expected to treat this as fatal to
// the action they were about to take.
var ErrNotRecorded = errors.New("audit entry could not be recorded")

// Record appends an entry.
//
// Sensitive keys are stripped from Detail before writing. That is a safety net
// rather than a licence to pass credentials: callers must not put them there,
// but a mistake should not become a permanent record of a secret.
func (r *Recorder) Record(ctx context.Context, entry Entry) error {
	detail, err := encodeDetail(entry.Detail)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrNotRecorded, err)
	}

	params := dbgen.AppendAuditEntryParams{
		ActorID:    entry.ActorID,
		ActorLabel: entry.ActorLabel,
		Action:     string(entry.Action),
		TargetKind: entry.TargetKind,
		TargetID:   entry.TargetID,
		UserAgent:  truncate(entry.UserAgent, 512),
		Detail:     detail,
	}
	if entry.SourceIP.IsValid() {
		params.SourceIp = &entry.SourceIP
	}

	if _, err := r.store.AppendAuditEntry(ctx, params); err != nil {
		// Logged as well as returned: if the database is refusing writes, the
		// operator needs to see it even if the caller handles the error.
		r.log.Error("audit entry not recorded",
			slog.String("action", string(entry.Action)),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("%w: %w", ErrNotRecorded, err)
	}

	return nil
}

// sensitiveKeys are removed from detail before an entry is written.
var sensitiveKeys = map[string]bool{
	"password": true, "newPassword": true, "oldPassword": true,
	"token": true, "secret": true, "credential": true,
	"apiKey": true, "authorization": true, "cookie": true,
	"privateKey": true, "sessionToken": true,
}

func encodeDetail(detail map[string]any) ([]byte, error) {
	if len(detail) == 0 {
		return []byte("{}"), nil
	}

	safe := make(map[string]any, len(detail))
	for key, value := range detail {
		if sensitiveKeys[key] {
			safe[key] = "[redacted]"
			continue
		}
		safe[key] = value
	}

	encoded, err := json.Marshal(safe)
	if err != nil {
		return nil, fmt.Errorf("encode audit detail: %w", err)
	}
	return encoded, nil
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

// Query describes a search of the audit log.
type Query struct {
	ActorID    *uuid.UUID
	Action     *string
	TargetKind *string
	Since      *time.Time
	Until      *time.Time
	Limit      int32
	Offset     int32
}

// List returns matching entries, most recent first.
//
// Callers must have checked the audit-read permission and must record their
// own read: this method does not do it for them, because it has no request
// context to attribute the read to.
func (r *Recorder) List(ctx context.Context, query Query) ([]dbgen.AuditEntry, error) {
	limit := query.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	entries, err := r.store.ListAuditEntries(ctx, dbgen.ListAuditEntriesParams{
		ActorID:    query.ActorID,
		Action:     query.Action,
		TargetKind: query.TargetKind,
		Since:      query.Since,
		Until:      query.Until,
		Limit:      limit,
		Offset:     query.Offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list audit entries: %w", err)
	}
	return entries, nil
}

// Prune removes entries older than the retention period and records that it
// did so, in the same log, so a gap is explained rather than unexplained.
func (r *Recorder) Prune(ctx context.Context, retention time.Duration) (int64, error) {
	if retention <= 0 {
		return 0, nil
	}

	cutoff := time.Now().Add(-retention)

	/*
		Measured before it is removed.

		The entry that records the removal has to say what went, and afterwards
		there is nothing left to count. "Entries older than a year were
		removed" is not an account of anything — a reader finding a gap needs
		to know how many entries it holds and what period it spans, or the gap
		is exactly as unexplained as it would have been with no entry at all.
	*/
	span, err := r.store.CountAuditEntriesBefore(ctx, cutoff)
	if err != nil {
		return 0, fmt.Errorf("measure audit entries to prune: %w", err)
	}

	removed, err := r.store.DeleteAuditEntriesBefore(ctx, cutoff)
	if err != nil {
		return 0, fmt.Errorf("prune audit entries: %w", err)
	}

	if removed > 0 {
		detail := map[string]any{
			"removed": removed,
			"before":  cutoff.Format(time.RFC3339),
		}
		// The span the gap covers, so a reader can tell what is missing rather
		// than only that something is.
		if earliest, ok := asTime(span.Earliest); ok {
			detail["earliest"] = earliest.Format(time.RFC3339)
		}
		if latest, ok := asTime(span.Latest); ok {
			detail["latest"] = latest.Format(time.RFC3339)
		}

		if err := r.Record(ctx, Entry{
			ActorLabel: "system",
			Action:     ActionAuditPruned,
			Detail:     detail,
		}); err != nil {
			return removed, err
		}
	}

	return removed, nil
}

/*
Export walks the whole log in order, oldest first.

Paged by the last identifier seen rather than by offset: an export of a large
log walks it in batches, and OFFSET makes each batch re-scan everything before
it. The identifier is the order entries were written, so an export is the log
as it happened.

Returns the entries and the identifier to continue from, which is zero when
there are no more.
*/
func (r *Recorder) Export(ctx context.Context, after int64, limit int32, query Query) ([]dbgen.AuditEntry, int64, error) {
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}

	entries, err := r.store.ExportAuditEntries(ctx, dbgen.ExportAuditEntriesParams{
		Limit: limit,
		After: after,
		Since: query.Since,
		Until: query.Until,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("export audit entries: %w", err)
	}

	if len(entries) < int(limit) {
		// The last page. Zero rather than the last identifier, so a caller
		// looping until it is zero terminates rather than asking once more for
		// nothing.
		return entries, 0, nil
	}
	return entries, entries[len(entries)-1].ID, nil
}

/*
asTime reads a value sqlc could not give a concrete type.

MIN and MAX over a nullable column come back as interface{}, because the
generator cannot tell from the query alone that they are timestamps. Asserting
here rather than changing the query keeps the SQL saying what it means, and a
value that is not a time is treated as absent — which for an aggregate over no
rows is exactly what it is.
*/
func asTime(value any) (time.Time, bool) {
	switch typed := value.(type) {
	case time.Time:
		return typed, true
	case *time.Time:
		if typed == nil {
			return time.Time{}, false
		}
		return *typed, true
	default:
		return time.Time{}, false
	}
}
