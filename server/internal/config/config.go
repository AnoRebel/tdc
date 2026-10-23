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
	OAuth       OAuthConfig
	Mail        MailConfig
	Cache       CacheConfig
	Calls       CallsConfig
	Backup      BackupConfig
}

/*
BackupConfig is what backups need that must not live in the database.

The passphrase scheduled backups are encrypted with is configuration rather than
a setting: a setting is stored in the database the archive protects, and a
backup whose key sits beside the data it locks is not protected from whoever
reads that database. Backups taken by hand are encrypted with a passphrase
the administrator types, which is never stored at all.
*/
type BackupConfig struct {
	// Passphrase encrypts scheduled backups. Empty is no scheduled backups.
	Passphrase string
	// WorkDirectory is where an archive is put together before it is stored.
	// It needs room for one archive and its largest table.
	WorkDirectory string
}

/*
OAuthConfig is the set of providers an operator has configured.

Named rather than enumerated: this project will not ship a list of commercial
providers with a blessed few at the top. An operator names whichever they run
or trust — a self-hosted Gitea or Keycloak, or a commercial one if they want
one — and each is configured the same way. Nothing here knows the difference.

Empty is a working state, and the common one: a server with no providers uses
passwords, which is what every server does today.
*/
type OAuthConfig struct {
	// Providers, in the order they should be offered.
	Providers []OAuthProvider
}

/*
OAuthProvider is one provider, described entirely by configuration.

The endpoints are given rather than discovered. Discovery would be one more
network call at startup that can fail, and an operator who can paste a client
id can paste two URLs — which also means a provider that does not publish a
discovery document works exactly like one that does.
*/
type OAuthProvider struct {
	/*
		Name identifies the provider in URLs and in the interface.

		Lowercase and URL-safe, because it appears in the callback address the
		operator registers with the provider: a name that needed escaping would
		be a name somebody typed wrongly into a form they cannot see.
	*/
	Name string
	// Label is what somebody reads on the button: "Sign in with Gitea".
	Label string

	ClientID     string
	ClientSecret string

	// AuthURL is where somebody is sent to approve.
	AuthURL string
	// TokenURL is where the code is exchanged, server to server.
	TokenURL string
	// UserURL returns the account: its identifier, and an email if it has one.
	UserURL string

	/*
		Scopes to request, which differ between providers for the same thing.

		Defaulted to the two every provider understands rather than left empty:
		a provider asked for no scopes returns a token that can read nothing.
	*/
	Scopes []string
}

/*
PushConfig configures notifications to browsers that are closed.

Off unless an operator has generated a key pair. The keys identify this server
to whichever push service a browser chose; no account with anybody is needed,
and the server can generate a pair itself.
*/
/*
CacheConfig is where short-lived shared state lives.

Empty is the default and the right answer for almost every server: one process,
one in-memory cache, nothing else to run. What it costs is that rate-limit
counters are per-process, which matters the moment there are two processes —
two nodes behind a load balancer would each allow the full number of attempts,
so a limit of five would in practice be ten.

So an operator running more than one node configures a Valkey, and the counters
become one shared count rather than one per process. Valkey rather than Redis
because Redis is no longer open source under a licence this project can depend
on, and Valkey is the fork that stayed.
*/
/*
CallsConfig holds what voice and video calls need that must not be shown.

The relay's shared secret: with it anybody can mint credentials for the relay
and use it as an open proxy, so it is configuration — redacted wherever
configuration is shown — rather than a setting an administrator reads back.
Which relay and the rest of calling's policy are settings, changeable while the
server runs.
*/
type CallsConfig struct {
	// TURNSecret is the static-auth-secret configured on the TURN relay
	// (coturn's use-auth-secret), from which short-lived credentials are made.
	TURNSecret string
}

type CacheConfig struct {
	// Address of the Valkey, host:port. Empty means the in-process cache.
	Address string
	// Username and Password, where the server wants them.
	Username string
	Password string
	// Database selects one of Valkey's numbered databases, for an operator
	// sharing an instance between applications.
	Database int32
	/*
		TLS encrypts the connection.

		Off by default, because the ordinary deployment is a Valkey on a private
		network beside the server. An operator reaching one across a network
		somebody else can see turns it on.
	*/
	TLS bool
}

/*
MailConfig is how this server sends mail, if it does.

Empty is the default and a working state: a server that sends nothing still runs,
and everything that would have been sent — a recovery link, most of all — is
simply unavailable rather than silently failing.

SMTP rather than an API, because a provider's API is a provider: recovery must
work with a mail server the operator runs themselves, and every self-hosted one
speaks SMTP.
*/
type MailConfig struct {
	// Host and Port of the mail server. Empty host is mail switched off.
	Host string
	Port int32

	// Username and Password, where the server wants them. Plenty of
	// self-hosted relays on a private network do not.
	Username string
	Password string

	/*
		From is the address mail appears to come from.

		Required when mail is configured at all: a message with no sender is one
		most receiving servers refuse, and the operator's own domain is the only
		thing that makes it deliverable.
	*/
	From string
	// FromName is what appears beside the address, falling back to the
	// server's own name.
	FromName string

	/*
		TLS says how the connection is protected.

		"starttls" — connect in the clear and upgrade, which is what port 587
		does and what almost every self-hosted server expects.
		"tls" — TLS from the first byte, which is port 465.
		"none" — no encryption at all. Refused unless the host is loopback,
		because a password and a recovery link in the clear across a network is
		not a configuration anybody means to choose.
	*/
	TLS string

	// Timeout bounds a send, so a mail server that stops answering does not
	// hold a request open.
	Timeout time.Duration
}

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
	/*
		MultiNode says other processes serve this same database.

		Off by default, because almost every deployment is one process and a
		broker there costs a held connection and a round trip through the
		database to show somebody their own message.

		Switched on, a node announces every event it stores and hears about
		every event the others store — without which somebody connected to the
		second node sees nothing from the first until they reload, and the only
		symptom is a conversation that appears to have stopped.

		It goes with cache.address: two nodes sharing events but not rate-limit
		counters is a server that fans out correctly and enforces half of every
		limit.
	*/
	MultiNode bool

	// Keepalive is how often an idle live connection is pinged. Configurable
	// because the right interval depends on what sits between client and
	// server, and because a phone on metered data pays for every one.
	Keepalive time.Duration

	/*
		RestartMethod is how this process comes back when an administrator
		restarts it.

		"exec" — it replaces itself with its own binary after shutting down,
		which keeps its process identifier and needs nothing outside it.
		"supervisor" — it exits with status 75 for systemd, or whatever runs
		it, to start again; refused unless one is detected or this is set.
		"auto" — supervisor under systemd, exec otherwise.
		"off" — restarting from the interface is refused.
	*/
	RestartMethod string
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
			RestartMethod:   "auto",
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
		Cache: CacheConfig{
			// In-process, which is right for the single-node deployment almost
			// every operator runs.
			Address: "",
		},

		Mail: MailConfig{
			// Off. A server that sends nothing runs perfectly well, and
			// recovery simply is not offered rather than failing quietly.
			Host: "",
			Port: 587,
			// What almost every self-hosted mail server on 587 expects.
			TLS:     "starttls",
			Timeout: 15 * time.Second,
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

		// Scheduled backups are off: a schedule needs a passphrase nobody
		// types, and that is the operator's to choose.
		Backup: BackupConfig{},
	}
}
