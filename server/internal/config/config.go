// Package config loads and validates the server's configuration.
//
// Configuration comes from a file, overridden by environment variables. Every
// problem found is reported at once rather than one per run: an operator
// correcting a deployment should not have to restart five times to discover
// five mistakes.
package config

import "time"

// EnvPrefix is prepended to every environment variable this server reads.
const EnvPrefix = "TDC_"

// Config is the server's complete configuration.
type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	Log      LogConfig
}

// ServerConfig describes how the server presents itself and listens.
type ServerConfig struct {
	// Name is the operator-chosen display name.
	Name string
	// Description says who the server is for.
	Description string
	// CanonicalAddress is how this server is known to clients and peers. It is
	// set by the operator and never inferred from a request header, so it
	// stays correct behind a proxy.
	CanonicalAddress string
	// Address is the host:port to listen on.
	Address string
	// DefaultLanguage is the interface language for users who have not chosen
	// one.
	DefaultLanguage string

	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration

	// ExposeInternalErrors includes internal error detail in responses. For
	// local development only: error text can disclose internals to an
	// unauthenticated caller.
	ExposeInternalErrors bool
}

// DatabaseConfig describes the connection to Postgres.
type DatabaseConfig struct {
	// URL is the connection string. Secret: it usually carries a password.
	URL string

	MaxConnections  int32
	MinConnections  int32
	ConnectTimeout  time.Duration
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration

	// AutoMigrate applies pending migrations at startup. On by default so a
	// single-binary deployment needs no separate step.
	AutoMigrate bool
}

// LogConfig controls logging.
type LogConfig struct {
	// Level is one of debug, info, warn, error.
	Level string
	// Format is json or text. JSON suits aggregation; text suits a terminal.
	Format string
}

// Default returns the configuration used when nothing is set.
//
// The defaults must produce a server that starts and works for local
// evaluation, but must never be safe-looking in a way that hides an unsafe
// production setting: anything genuinely unsafe is reported by Warnings.
func Default() Config {
	return Config{
		Server: ServerConfig{
			Name:                 "TDC",
			Description:          "A Tanzania Developers Community server",
			CanonicalAddress:     "",
			Address:              ":8080",
			DefaultLanguage:      "en",
			ReadTimeout:          15 * time.Second,
			WriteTimeout:         30 * time.Second,
			IdleTimeout:          120 * time.Second,
			ShutdownTimeout:      20 * time.Second,
			ExposeInternalErrors: false,
		},
		Database: DatabaseConfig{
			URL:             "",
			MaxConnections:  10,
			MinConnections:  2,
			ConnectTimeout:  10 * time.Second,
			MaxConnLifetime: time.Hour,
			MaxConnIdleTime: 30 * time.Minute,
			AutoMigrate:     true,
		},
		Log: LogConfig{
			Level:  "info",
			Format: "json",
		},
	}
}
