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

	// Server configuration an operator changes while it runs.
	ActionSettingChanged Action = "server.setting.changed"

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
	removed, err := r.store.DeleteAuditEntriesBefore(ctx, cutoff)
	if err != nil {
		return 0, fmt.Errorf("prune audit entries: %w", err)
	}

	if removed > 0 {
		if err := r.Record(ctx, Entry{
			ActorLabel: "system",
			Action:     "server.audit.pruned",
			Detail: map[string]any{
				"removed": removed,
				"before":  cutoff.Format(time.RFC3339),
			},
		}); err != nil {
			return removed, err
		}
	}

	return removed, nil
}
