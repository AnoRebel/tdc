/*
Package settings holds what an operator can change while the server is running.

Distinct from the configuration file, which is read once at startup and
describes how the process is wired: where the database is, what port to listen
on, which storage backend to use. Those cannot change without a restart and
should not.

What lives here is policy — who may register, how many channels a server will
hold, what it calls itself to a reader. Requiring a restart to close
registration during an incident is the difference between a setting and a
deployment, and an operator dealing with a flood of spam accounts at two in the
morning should not have to redeploy.

Every value is stored as text and parsed on read. A settings table typed to the
values it holds would need a migration for each new setting, which is exactly
the friction this exists to avoid.
*/
package settings

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/moderation"
)

// ErrUnknownSetting is returned for a key this server does not define.
//
// A fixed set rather than free-form keys: a typo that silently created a new
// setting would look like a change that did nothing.
var ErrUnknownSetting = errors.New("that is not a setting this server has")

// ErrInvalidValue is returned when a value is not one the setting accepts.
var ErrInvalidValue = errors.New("that is not a value this setting accepts")

// Key names a setting.
type Key string

const (
	/*
		RegistrationPolicy decides who may create an account.

		"open" — anybody may.
		"invitation" — only somebody holding an invitation.
		"closed" — nobody; the operator creates accounts.
		"domain" — anybody with an email address in RegistrationDomains.
	*/
	RegistrationPolicy Key = "registration.policy"
	// RegistrationDomains is a comma-separated list, used by the domain
	// policy. Empty under any other policy.
	RegistrationDomains Key = "registration.domains"

	// MaxUsers bounds how many accounts this server will hold. Zero is no
	// limit, which is the default: a limit nobody chose is a limit that
	// surprises somebody.
	MaxUsers Key = "limits.max_users"
	// MaxChannels bounds how many channels may exist.
	MaxChannels Key = "limits.max_channels"
	// MaxStorageBytes bounds total attachment storage.
	MaxStorageBytes Key = "limits.max_storage_bytes"

	/*
		FilterWords is the operator's own list of words to act on, comma
		separated. Empty — the default — is filtering switched off, which makes
		"disabled by default" the absence of configuration rather than a
		separate switch to forget.
	*/
	FilterWords Key = "filter.words"
	/*
		FilterAction is what happens to a message that matches: "flag", "mask"
		or "block".

		Flagging by default. A word list cannot tell a slur from a discussion
		of one, so the filter decides what a human is shown rather than
		deciding on its own.
	*/
	FilterAction Key = "filter.action"

	/*
		ObscureSensitiveMedia requires clients to hide media marked sensitive
		until a viewer chooses to see it.

		On by default. Somebody who marked their upload did so meaning it to be
		covered, and a server that ignored that until an operator found the
		setting would be one where the marking did nothing.

		An operator can turn it off for a server where everybody has agreed
		otherwise — a private team channel where covering images is friction
		rather than protection.
	*/
	ObscureSensitiveMedia Key = "moderation.obscure_sensitive_media"
)

// definition describes one setting: how to validate it, and what it means when
// nobody has set it.
type definition struct {
	fallback string
	// validate returns why a value is unacceptable, or nil.
	validate func(string) error
	// Description is what an operator reading the settings sees.
	description string
	// secret marks a value redacted when configuration is displayed.
	secret bool
}

var definitions = map[Key]definition{
	RegistrationPolicy: {
		fallback:    "open",
		description: `Who may create an account: "open", "invitation", "closed" or "domain".`,
		validate: func(value string) error {
			switch value {
			case "open", "invitation", "closed", "domain":
				return nil
			}
			return fmt.Errorf(`%w: expected "open", "invitation", "closed" or "domain"`,
				ErrInvalidValue)
		},
	},
	RegistrationDomains: {
		fallback:    "",
		description: "Email domains that may register, comma separated, under the domain policy.",
		validate: func(value string) error {
			for _, domain := range strings.Split(value, ",") {
				domain = strings.TrimSpace(domain)
				if domain == "" {
					continue
				}
				// A leading @ is what somebody naturally types, and accepting
				// it silently would match nothing.
				if strings.ContainsAny(domain, "@ ") {
					return fmt.Errorf("%w: %q should be a bare domain like example.com",
						ErrInvalidValue, domain)
				}
			}
			return nil
		},
	},
	FilterWords: {
		fallback:    "",
		description: "Words to act on, comma separated. Empty means no filtering.",
		// No validation beyond what the filter itself does. Entries are matched
		// literally rather than as patterns, so there is no input here that can
		// be malformed — only a list an operator may regret.
	},
	FilterAction: {
		fallback:    "flag",
		description: `What happens to a matching message: "flag", "mask" or "block".`,
		validate: func(value string) error {
			if !moderation.KnownAction(moderation.Action(value)) {
				return fmt.Errorf(`%w: expected "flag", "mask" or "block"`, ErrInvalidValue)
			}
			return nil
		},
	},
	ObscureSensitiveMedia: {
		fallback:    "true",
		description: "Hide media marked sensitive until a viewer chooses to see it.",
		validate:    boolean,
	},
	MaxUsers:        {fallback: "0", description: "Maximum accounts, or 0 for no limit.", validate: nonNegative},
	MaxChannels:     {fallback: "0", description: "Maximum channels, or 0 for no limit.", validate: nonNegative},
	MaxStorageBytes: {fallback: "0", description: "Maximum attachment storage in bytes, or 0 for no limit.", validate: nonNegative},
}

// boolean accepts what a checkbox sends, and nothing else.
func boolean(value string) error {
	if value != "true" && value != "false" {
		return fmt.Errorf(`%w: expected "true" or "false"`, ErrInvalidValue)
	}
	return nil
}

func nonNegative(value string) error {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: expected a whole number", ErrInvalidValue)
	}
	if parsed < 0 {
		return fmt.Errorf("%w: expected zero or more", ErrInvalidValue)
	}
	return nil
}

/*
Service reads and writes settings.

Cached, because these are read on paths that run constantly — every
registration checks the policy, every upload checks the storage limit — and a
database round trip for a value that changes a few times a year is waste.

The cache is invalidated on write rather than expiring, so a change an operator
makes takes effect at once. That holds for a single server; a deployment
running several would see a change reach the others when their caches next
miss, which is why the cache carries a short lifetime as well.
*/
type Service struct {
	queries *dbgen.Queries
	log     *slog.Logger

	mu     sync.RWMutex
	cache  map[Key]string
	loaded time.Time
}

/*
cacheLifetime bounds how stale a cached setting may be.

Short enough that a second server picks up a change quickly, long enough that
the common path does not query. A setting is not something that changes during
a conversation.
*/
const cacheLifetime = 30 * time.Second

func NewService(pool *db.Pool, log *slog.Logger) *Service {
	return &Service{
		queries: dbgen.New(pool),
		log:     log,
		cache:   map[Key]string{},
	}
}

// Known reports whether a key is one this server defines.
func Known(key Key) bool {
	_, ok := definitions[key]
	return ok
}

// Keys lists every setting, for an operator's own inspection.
func Keys() []Key {
	keys := make([]Key, 0, len(definitions))
	for key := range definitions {
		keys = append(keys, key)
	}
	return keys
}

// Describe returns what a setting means and what it falls back to.
func Describe(key Key) (description, fallback string, ok bool) {
	definition, found := definitions[key]
	if !found {
		return "", "", false
	}
	return definition.description, definition.fallback, true
}

/*
Get reads a setting, falling back to its default.

A value that cannot be read falls back rather than failing. A server that
refused to register anybody because a settings query timed out would be worse
than one that briefly applied the default policy — and the default for every
setting here is the permissive, unsurprising one.
*/
func (s *Service) Get(ctx context.Context, key Key) string {
	definition, known := definitions[key]
	if !known {
		return ""
	}

	if value, ok := s.cached(key); ok {
		return value
	}

	if err := s.refresh(ctx); err != nil {
		s.log.Warn("cannot read settings, using defaults", "error", err)
		return definition.fallback
	}

	if value, ok := s.cached(key); ok {
		return value
	}
	return definition.fallback
}

// Int reads a setting as a number, falling back to its default.
func (s *Service) Int(ctx context.Context, key Key) int64 {
	parsed, err := strconv.ParseInt(s.Get(ctx, key), 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

/*
Set records a setting, after checking it is one this server has and a value it
accepts.

Validated here rather than at the boundary, so that every path writing a
setting gets the same rules — an administrative endpoint, a command-line tool,
and a migration all have to agree about what "open" means.
*/
func (s *Service) Set(ctx context.Context, key Key, value, by string) error {
	definition, known := definitions[key]
	if !known {
		return fmt.Errorf("%w: %s", ErrUnknownSetting, key)
	}

	value = strings.TrimSpace(value)
	if definition.validate != nil {
		if err := definition.validate(value); err != nil {
			return err
		}
	}

	err := s.queries.SetServerConfigValue(ctx, dbgen.SetServerConfigValueParams{
		Key:       string(key),
		Value:     value,
		IsSecret:  definition.secret,
		UpdatedBy: optional(by),
	})
	if err != nil {
		return fmt.Errorf("settings: writing %s: %w", key, err)
	}

	// Invalidated rather than updated in place: the next read fetches every
	// setting, which keeps one code path for loading them.
	s.invalidate()

	s.log.Info("a setting changed", "key", key, "by", by)
	return nil
}

// All returns every setting and its current value, for inspection.
//
// Secrets are redacted: this is what an administrator sees, and a settings
// page that displays a credential is a credential in a screenshot.
func (s *Service) All(ctx context.Context) map[Key]string {
	values := make(map[Key]string, len(definitions))

	for key, definition := range definitions {
		if definition.secret {
			values[key] = "••••••••"
			continue
		}
		values[key] = s.Get(ctx, key)
	}

	return values
}

func (s *Service) cached(key Key) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if time.Since(s.loaded) > cacheLifetime {
		return "", false
	}
	value, ok := s.cache[key]
	return value, ok
}

func (s *Service) refresh(ctx context.Context) error {
	rows, err := s.queries.ListServerConfig(ctx)
	if err != nil {
		return fmt.Errorf("settings: reading: %w", err)
	}

	fresh := make(map[Key]string, len(rows))
	for _, row := range rows {
		fresh[Key(row.Key)] = row.Value
	}

	// Defaults filled in here, so a cache hit never has to know about them.
	for key, definition := range definitions {
		if _, set := fresh[key]; !set {
			fresh[key] = definition.fallback
		}
	}

	s.mu.Lock()
	s.cache = fresh
	s.loaded = time.Now()
	s.mu.Unlock()

	return nil
}

func (s *Service) invalidate() {
	s.mu.Lock()
	s.loaded = time.Time{}
	s.mu.Unlock()
}

func optional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

/*
raw reads a stored value by key, without requiring a definition.

Get deliberately refuses a key it has no definition for, which is what stops a
typo looking like a setting. Feature toggles are stored in the same table but
are not settings — they are validated by name against their own list — so they
need a reader that does not consult the definitions map.

Returns the empty string when nothing is stored, leaving the caller to decide
what absence means; for a feature that is its default.
*/
func (s *Service) raw(ctx context.Context, key Key) string {
	if value, ok := s.cached(key); ok {
		return value
	}

	if err := s.refresh(ctx); err != nil {
		s.log.Warn("cannot read settings, using defaults", "error", err)
		return ""
	}

	value, _ := s.cached(key)
	return value
}

// IsSecret reports whether a setting's value must be redacted wherever it is
// displayed, logged or exported.
//
// Exported so that callers outside this package — the audit log, in
// particular — can avoid recording a credential rather than having to know
// which keys are sensitive.
func IsSecret(key Key) bool {
	definition, known := definitions[key]
	return known && definition.secret
}
