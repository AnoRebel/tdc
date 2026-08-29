package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Problem is one thing wrong with the configuration.
type Problem struct {
	// Key is the setting, named as an operator would write it in the file
	// (e.g. "database.url").
	Key string
	// Message says what is wrong and, where useful, what is accepted.
	Message string
}

func (p Problem) String() string { return p.Key + ": " + p.Message }

// InvalidError reports every problem found, not merely the first.
type InvalidError struct {
	Problems []Problem
}

func (e *InvalidError) Error() string {
	if len(e.Problems) == 1 {
		return "invalid configuration: " + e.Problems[0].String()
	}
	lines := make([]string, 0, len(e.Problems)+1)
	lines = append(lines, fmt.Sprintf("invalid configuration: %d problems:", len(e.Problems)))
	for _, p := range e.Problems {
		lines = append(lines, "  - "+p.String())
	}
	return strings.Join(lines, "\n")
}

// loader accumulates problems while reading values, so that one pass reports
// everything rather than stopping at the first mistake.
type loader struct {
	file     map[string]string
	problems []Problem
}

func (l *loader) problem(key, format string, args ...any) {
	l.problems = append(l.problems, Problem{Key: key, Message: fmt.Sprintf(format, args...)})
}

// raw returns the value for key, preferring the environment over the file.
//
// Environment wins so that a container deployment can override a baked-in file
// without rewriting it.
func (l *loader) raw(key string) (string, bool) {
	if value, ok := os.LookupEnv(envName(key)); ok {
		return value, true
	}
	value, ok := l.file[key]
	return value, ok
}

// envName maps a setting key to its environment variable, so "database.url"
// becomes "TDC_DATABASE_URL".
func envName(key string) string {
	return EnvPrefix + strings.ToUpper(strings.NewReplacer(".", "_", "-", "_").Replace(key))
}

func (l *loader) str(key, fallback string) string {
	if value, ok := l.raw(key); ok {
		return value
	}
	return fallback
}

func (l *loader) boolean(key string, fallback bool) bool {
	value, ok := l.raw(key)
	if !ok {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		l.problem(key, "%q is not a boolean (use true or false)", value)
		return fallback
	}
	return parsed
}

func (l *loader) duration(key string, fallback time.Duration) time.Duration {
	value, ok := l.raw(key)
	if !ok {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		l.problem(key, "%q is not a duration (for example 30s, 5m, 1h)", value)
		return fallback
	}
	if parsed <= 0 {
		l.problem(key, "must be greater than zero, got %s", parsed)
		return fallback
	}
	return parsed
}

func (l *loader) int32Value(key string, fallback int32) int32 {
	value, ok := l.raw(key)
	if !ok {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		l.problem(key, "%q is not a whole number", value)
		return fallback
	}
	if parsed <= 0 {
		l.problem(key, "must be greater than zero, got %d", parsed)
		return fallback
	}
	return int32(parsed)
}

func (l *loader) oneOf(key, fallback string, allowed ...string) string {
	value := l.str(key, fallback)
	for _, candidate := range allowed {
		if value == candidate {
			return value
		}
	}
	l.problem(key, "%q is not valid (expected one of: %s)", value, strings.Join(allowed, ", "))
	return fallback
}

// Load reads configuration from path, applies environment overrides, and
// validates the result.
//
// An empty path skips the file, which is the normal case for a container
// deployment configured entirely by environment. A missing file at an
// explicitly given path is an error, because it means the operator expected it
// to be read.
func Load(path string) (Config, error) {
	l := &loader{file: map[string]string{}}

	if path != "" {
		parsed, err := parseFile(path)
		if err != nil {
			return Config{}, err
		}
		l.file = parsed
	}

	cfg := Default()
	d := cfg // defaults, for fallbacks

	cfg.Server.Name = l.str("server.name", d.Server.Name)
	cfg.Server.Description = l.str("server.description", d.Server.Description)
	cfg.Server.CanonicalAddress = l.str("server.canonical_address", d.Server.CanonicalAddress)
	cfg.Server.Address = l.str("server.address", d.Server.Address)
	cfg.Server.DefaultLanguage = l.str("server.default_language", d.Server.DefaultLanguage)
	cfg.Server.ReadTimeout = l.duration("server.read_timeout", d.Server.ReadTimeout)
	cfg.Server.WriteTimeout = l.duration("server.write_timeout", d.Server.WriteTimeout)
	cfg.Server.IdleTimeout = l.duration("server.idle_timeout", d.Server.IdleTimeout)
	cfg.Server.ShutdownTimeout = l.duration("server.shutdown_timeout", d.Server.ShutdownTimeout)
	cfg.Server.ExposeInternalErrors = l.boolean("server.expose_internal_errors", d.Server.ExposeInternalErrors)

	cfg.Database.URL = l.str("database.url", d.Database.URL)
	cfg.Database.MaxConnections = l.int32Value("database.max_connections", d.Database.MaxConnections)
	cfg.Database.MinConnections = l.int32Value("database.min_connections", d.Database.MinConnections)
	cfg.Database.ConnectTimeout = l.duration("database.connect_timeout", d.Database.ConnectTimeout)
	cfg.Database.MaxConnLifetime = l.duration("database.max_conn_lifetime", d.Database.MaxConnLifetime)
	cfg.Database.MaxConnIdleTime = l.duration("database.max_conn_idle_time", d.Database.MaxConnIdleTime)
	cfg.Database.AutoMigrate = l.boolean("database.auto_migrate", d.Database.AutoMigrate)

	cfg.Log.Level = l.oneOf("log.level", d.Log.Level, "debug", "info", "warn", "error")
	cfg.Log.Format = l.oneOf("log.format", d.Log.Format, "json", "text")

	l.problems = append(l.problems, cfg.validate()...)

	if len(l.problems) > 0 {
		return Config{}, &InvalidError{Problems: l.problems}
	}
	return cfg, nil
}

// validate checks constraints that span settings or cannot be expressed while
// parsing a single value.
func (c Config) validate() []Problem {
	var problems []Problem
	add := func(key, format string, args ...any) {
		problems = append(problems, Problem{Key: key, Message: fmt.Sprintf(format, args...)})
	}

	if strings.TrimSpace(c.Database.URL) == "" {
		add("database.url", "is required (for example postgres://user:password@host:5432/tdc)")
	} else if !strings.HasPrefix(c.Database.URL, "postgres://") &&
		!strings.HasPrefix(c.Database.URL, "postgresql://") {
		add("database.url", "must be a postgres:// or postgresql:// URL")
	}

	if strings.TrimSpace(c.Server.Name) == "" {
		add("server.name", "must not be empty")
	}

	if c.Database.MinConnections > c.Database.MaxConnections {
		add("database.min_connections", "must not exceed database.max_connections (%d)",
			c.Database.MaxConnections)
	}

	if c.Server.ShutdownTimeout > c.Server.IdleTimeout+c.Server.WriteTimeout {
		// Not fatal, but almost always a mistake: shutdown waits for
		// connections that the timeouts would already have closed.
		add("server.shutdown_timeout",
			"is longer than idle and write timeouts combined, which delays shutdown for no benefit")
	}

	return problems
}

var errMissingFile = errors.New("configuration file not found")

// IsMissingFile reports whether err is a missing configuration file, so a
// caller can distinguish "no file" from "a bad file".
func IsMissingFile(err error) bool { return errors.Is(err, errMissingFile) }
