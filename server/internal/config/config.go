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
	Server      ServerConfig
	Database    DatabaseConfig
	Storage     StorageConfig
	Attachments AttachmentsConfig
	Log         LogConfig
	GIFs        GIFsConfig
	Push        PushConfig
}

/*
PushConfig configures notifications to browsers that are closed.

Off unless an operator has generated a key pair. The keys identify this server
to whichever push service a browser chose; no account with anybody is needed,
and the server can generate a pair itself.
*/
type PushConfig struct {
	// VAPIDPublic and VAPIDPrivate are the operator's own pair. Empty disables
	// push, which is a working state: notifications are still stored and shown
	// in the application.
	VAPIDPublic  string
	VAPIDPrivate string
	/*
		Subject is a mailto: or https: address a push service can use to reach
		the operator about a problem with their traffic. Required by the
		specification, and some services refuse without it.
	*/
	Subject string
}

// AttachmentsConfig configures what happens to an uploaded file.
type AttachmentsConfig struct {
	// ScannerAddress is a clamd host and port. Empty — the default — disables
	// scanning, which is how this server runs unless an operator asks
	// otherwise: it must not require an external service.
	ScannerAddress string
	// ScannerTimeout bounds a scan, so a scanner that hangs does not hold an
	// upload open.
	ScannerTimeout time.Duration
	// ScannerFailOpen accepts a file when the scanner cannot be reached. Off
	// by default: a scanner that is down must not become a silent bypass, and
	// choosing otherwise should be a decision rather than an accident.
	ScannerFailOpen bool

	// MediaSidecarAddress sends preview generation to another container.
	//
	// Empty — the default — does the work in this process, which is safe
	// because decoding is pure Go and there is no image library in C to be
	// exploited. An operator who would rather a decoder failure took down
	// something disposable can point this at a sidecar.
	MediaSidecarAddress string
	// MediaSidecarTimeout bounds a request to it.
	MediaSidecarTimeout time.Duration

	// LinkPreviews lets this server fetch a summary of links people post.
	//
	// On by default because a preview is genuinely useful, but an operator who
	// would rather their server never made an outbound request on a member's
	// behalf can turn it off for everyone.
	LinkPreviews bool
}

/*
GIFsConfig configures searching a third-party GIF library.

Off unless a provider and a key are both set, and it stays off in every
default. Every library worth searching requires an account and an API key,
which conflicts with running this server at no cost and with no external
accounts — so searching is something an operator opts into, knowing what it
costs.

Sending a GIF as an attachment needs none of this and works regardless.
*/
type GIFsConfig struct {
	// Provider is "tenor", "giphy", or empty for no search.
	Provider string
	// APIKey is the operator's own key with that provider.
	APIKey string
	// ContentFilter bounds what a search returns. Empty means the strictest
	// each provider offers, which is the right default for a search box in a
	// shared conversation.
	ContentFilter string
	// Timeout bounds a search.
	Timeout time.Duration
}

// StorageConfig says where uploaded files are kept.
//
// The default keeps them in a directory, so a small deployment needs no object
// store at all. An operator who wants one picks whichever S3 client they would
// rather run: the two speak to the same stores and behave identically, so the
// choice is preference, and changing it is one word.
type StorageConfig struct {
	// Backend is "local", "s3-aws", or "s3-minio".
	Backend string
	// Directory is where the local backend keeps files.
	Directory string
	// S3 configures either S3 backend, and is ignored by the local one.
	S3 S3Config
}

// S3Config addresses an S3-compatible object store.
type S3Config struct {
	// Endpoint is the store's address, e.g. "https://s3.example.org".
	Endpoint string
	Bucket   string
	// Region is usually ignored by self-hosted stores, but signing requires
	// one, so it defaults rather than forcing an operator to invent a value.
	Region    string
	AccessKey string
	SecretKey string
	// PathStyle addresses buckets as endpoint/bucket rather than
	// bucket.endpoint. Most self-hosted stores need it, and a host without dots
	// cannot do virtual-host style at all.
	PathStyle bool
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

	// WebRoot is the directory holding the built web application.
	//
	// Served from the same origin as the API, which is what keeps the session
	// cookie first-party: a browser treats a cookie from a different host as
	// third-party and, under SameSite=Lax, declines to send it back.
	//
	// Empty in development, where the application runs from its own dev
	// server and reaches the API across origins.
	WebRoot string

	// ExposeInternalErrors includes internal error detail in responses. For
	// local development only: error text can disclose internals to an
	// unauthenticated caller.
	ExposeInternalErrors bool

	// AllowedOrigins lists browser origins permitted to call this API from
	// another origin, comma-separated in configuration. Empty is correct when
	// the web app is served from the same origin, which is the normal
	// deployment; it is set during development, when the app runs on a
	// different port.
	AllowedOrigins []string

	// SecureCookies marks session cookies Secure, so a browser sends them only
	// over TLS. It defaults to on: a deployment served over plain HTTP has to
	// say so explicitly rather than silently weakening its cookies.
	SecureCookies bool
	// SessionLifetime is how long a session lasts without being renewed.
	SessionLifetime time.Duration
	// Keepalive is how often an idle live connection is pinged. Configurable
	// because the right interval depends on what sits between client and
	// server, and because a phone on metered data pays for every one.
	Keepalive time.Duration
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
			// The image puts the built application here. Empty on a
			// development machine, where the app runs from its own server.
			WebRoot:         "/usr/share/tdc/web",
			SecureCookies:   true,
			SessionLifetime: 30 * 24 * time.Hour,
			Keepalive:       45 * time.Second,
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
		Storage: StorageConfig{
			Backend:   "local",
			Directory: "./data/attachments",
			S3: S3Config{
				Region:    "us-east-1",
				PathStyle: true,
			},
		},
		Attachments: AttachmentsConfig{
			ScannerAddress:      "",
			ScannerTimeout:      30 * time.Second,
			ScannerFailOpen:     false,
			LinkPreviews:        true,
			MediaSidecarAddress: "",
			MediaSidecarTimeout: 30 * time.Second,
		},
		GIFs: GIFsConfig{
			// Off: searching requires somebody else's account, and this server
			// must work without one.
			Provider: "",
			APIKey:   "",
			Timeout:  5 * time.Second,
		},

		Log: LogConfig{
			Level:  "info",
			Format: "json",
		},
	}
}
