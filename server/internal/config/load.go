package config

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"regexp"
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

// list reads a comma-separated setting, discarding empty entries so that
// trailing commas and blank values are tolerated.
func (l *loader) list(key string) []string {
	value, ok := l.raw(key)
	if !ok || strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			items = append(items, trimmed)
		}
	}
	return items
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
	cfg.Server.WebRoot = l.str("server.web_root", d.Server.WebRoot)
	cfg.Server.AllowedOrigins = l.list("server.allowed_origins")
	cfg.Server.SecureCookies = l.boolean("server.secure_cookies", d.Server.SecureCookies)
	cfg.Server.SessionLifetime = l.duration("server.session_lifetime", d.Server.SessionLifetime)
	cfg.Server.Keepalive = l.duration("server.keepalive", d.Server.Keepalive)

	cfg.Database.URL = l.str("database.url", d.Database.URL)
	cfg.Database.MaxConnections = l.int32Value("database.max_connections", d.Database.MaxConnections)
	cfg.Database.MinConnections = l.int32Value("database.min_connections", d.Database.MinConnections)
	cfg.Database.ConnectTimeout = l.duration("database.connect_timeout", d.Database.ConnectTimeout)
	cfg.Database.MaxConnLifetime = l.duration("database.max_conn_lifetime", d.Database.MaxConnLifetime)
	cfg.Database.MaxConnIdleTime = l.duration("database.max_conn_idle_time", d.Database.MaxConnIdleTime)
	cfg.Database.AutoMigrate = l.boolean("database.auto_migrate", d.Database.AutoMigrate)

	cfg.Storage.Backend = l.oneOf("storage.backend", d.Storage.Backend, "local", "s3-aws", "s3-minio")
	cfg.Storage.Directory = l.str("storage.directory", d.Storage.Directory)
	cfg.Storage.S3.Endpoint = l.str("storage.s3.endpoint", d.Storage.S3.Endpoint)
	cfg.Storage.S3.Bucket = l.str("storage.s3.bucket", d.Storage.S3.Bucket)
	cfg.Storage.S3.Region = l.str("storage.s3.region", d.Storage.S3.Region)
	cfg.Storage.S3.AccessKey = l.str("storage.s3.access_key", d.Storage.S3.AccessKey)
	cfg.Storage.S3.SecretKey = l.str("storage.s3.secret_key", d.Storage.S3.SecretKey)
	cfg.Storage.S3.PathStyle = l.boolean("storage.s3.path_style", d.Storage.S3.PathStyle)

	cfg.Attachments.ScannerAddress = l.str("attachments.scanner_address", d.Attachments.ScannerAddress)
	cfg.Attachments.ScannerTimeout = l.duration("attachments.scanner_timeout", d.Attachments.ScannerTimeout)
	cfg.Attachments.ScannerFailOpen = l.boolean("attachments.scanner_fail_open", d.Attachments.ScannerFailOpen)
	cfg.Attachments.LinkPreviews = l.boolean("attachments.link_previews", d.Attachments.LinkPreviews)

	// Empty unless an operator sets both, which is what keeps searching off by
	// default and this server free of anybody else's account.
	cfg.Push.VAPIDPublic = l.str("push.vapid_public", d.Push.VAPIDPublic)
	cfg.Push.VAPIDPrivate = l.str("push.vapid_private", d.Push.VAPIDPrivate)
	cfg.Push.Subject = l.str("push.subject", d.Push.Subject)

	cfg.Mail.Host = l.str("mail.host", d.Mail.Host)
	cfg.Mail.Port = l.int32Value("mail.port", d.Mail.Port)
	cfg.Mail.Username = l.str("mail.username", d.Mail.Username)
	cfg.Mail.Password = l.str("mail.password", d.Mail.Password)
	cfg.Mail.From = l.str("mail.from", d.Mail.From)
	cfg.Mail.FromName = l.str("mail.from_name", d.Mail.FromName)
	cfg.Mail.TLS = l.oneOf("mail.tls", d.Mail.TLS, "starttls", "tls", "none")
	cfg.Mail.Timeout = l.duration("mail.timeout", d.Mail.Timeout)

	cfg.GIFs.Provider = l.str("gifs.provider", d.GIFs.Provider)
	cfg.GIFs.APIKey = l.str("gifs.api_key", d.GIFs.APIKey)
	cfg.GIFs.ContentFilter = l.str("gifs.content_filter", d.GIFs.ContentFilter)
	cfg.GIFs.Timeout = l.duration("gifs.timeout", d.GIFs.Timeout)
	cfg.Attachments.MediaSidecarAddress = l.str("attachments.media_sidecar_address", d.Attachments.MediaSidecarAddress)
	cfg.Attachments.MediaSidecarTimeout = l.duration("attachments.media_sidecar_timeout", d.Attachments.MediaSidecarTimeout)

	cfg.OAuth = l.oauth()

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

	/*
		Half a mail configuration is refused at startup.

		A host with no sender address produces mail most receiving servers
		discard, and a sender with no host produces nothing at all. Either way
		somebody meant to switch this on, and the first they would learn
		otherwise is a member who cannot get back into their account.
	*/
	if strings.TrimSpace(c.Mail.Host) != "" {
		if strings.TrimSpace(c.Mail.From) == "" {
			add("mail.from", "is required when mail.host is set — mail with no sender address is mail nobody accepts")
		} else if !strings.Contains(c.Mail.From, "@") {
			add("mail.from", "must be an email address, got %q", c.Mail.From)
		}

		/*
			Unencrypted only to this machine.

			"none" sends the password, the recovery link and everything else in
			the clear. Across a network that is not a choice anybody means to
			make; to a relay on loopback it is ordinary, and refusing it there
			would be refusing the simplest working setup there is.
		*/
		if c.Mail.TLS == "none" && !isLoopback(c.Mail.Host) {
			add("mail.tls", "may only be \"none\" for a mail server on this machine; %q is not one", c.Mail.Host)
		}
	} else if strings.TrimSpace(c.Mail.From) != "" {
		add("mail.host", "is required when mail.from is set — there is nowhere to send through")
	}

	/*
		Half a GIF configuration is refused at startup.

		A provider without a key, or a key without a provider, is somebody who
		meant to switch this on. Silently leaving it off would look like the
		feature is broken rather than unconfigured, and it is far cheaper to
		say so here than to have somebody discover it from an empty picker.
	*/
	provider := strings.TrimSpace(c.GIFs.Provider)
	key := strings.TrimSpace(c.GIFs.APIKey)

	switch {
	case provider == "" && key != "":
		add("gifs.provider", `is required when a key is set: "tenor" or "giphy"`)
	case provider != "" && key == "":
		add("gifs.api_key", "is required when a provider is set")
	case provider != "" && provider != "tenor" && provider != "giphy":
		add("gifs.provider", `must be "tenor" or "giphy"`)
	}

	switch backend := strings.TrimSpace(c.Storage.Backend); backend {
	case "", "local":
		if strings.TrimSpace(c.Storage.Directory) == "" {
			add("storage.directory", "is required when the storage backend is local")
		}
	case "s3-aws", "s3-minio":
		// Reported field by field rather than as one message, so an operator
		// sees exactly which values are missing.
		if strings.TrimSpace(c.Storage.S3.Endpoint) == "" {
			add("storage.s3.endpoint", "is required when the storage backend is %s", backend)
		}
		if strings.TrimSpace(c.Storage.S3.Bucket) == "" {
			add("storage.s3.bucket", "is required when the storage backend is %s", backend)
		}
		if strings.TrimSpace(c.Storage.S3.AccessKey) == "" {
			add("storage.s3.access_key", "is required when the storage backend is %s", backend)
		}
		if strings.TrimSpace(c.Storage.S3.SecretKey) == "" {
			add("storage.s3.secret_key", "is required when the storage backend is %s", backend)
		}
	default:
		add("storage.backend", "must be local, s3-aws, or s3-minio (got %q)", backend)
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

/*
 * Reading the sign-in providers.
 *
 * The names come first, from oauth.providers, and each name opens a namespace
 * of its own: a provider called "github" is configured under oauth.github.*,
 * which is TDC_OAUTH_GITHUB_CLIENT_SECRET in the environment. Nothing is
 * discovered over the network — the endpoints are stated — so a self-hosted
 * Forgejo or Keycloak is configured exactly like a commercial provider, and an
 * offline server starts as readily as a connected one.
 */
func (l *loader) oauth() OAuthConfig {
	names := l.list("oauth.providers")
	if len(names) == 0 {
		return OAuthConfig{}
	}

	seen := make(map[string]bool, len(names))
	providers := make([]OAuthProvider, 0, len(names))

	for _, name := range names {
		folded := strings.ToLower(name)
		if !providerName.MatchString(folded) {
			l.problem("oauth.providers", "%q is not a usable provider name (letters, digits, hyphen and underscore only)", name)
			continue
		}
		if seen[folded] {
			l.problem("oauth.providers", "%q is listed more than once", name)
			continue
		}
		seen[folded] = true

		at := func(setting string) string { return "oauth." + folded + "." + setting }

		provider := OAuthProvider{
			Name:         folded,
			Label:        l.str(at("label"), name),
			ClientID:     l.str(at("client_id"), ""),
			ClientSecret: l.str(at("client_secret"), ""),
			AuthURL:      l.str(at("auth_url"), ""),
			TokenURL:     l.str(at("token_url"), ""),
			UserURL:      l.str(at("user_url"), ""),
			Scopes:       l.list(at("scopes")),
		}

		// Every one of these is needed to complete a sign-in, so a provider
		// missing any of them is named here rather than at the moment somebody
		// clicks the button and gets a page that cannot work.
		required := []struct {
			setting string
			value   string
			hint    string
		}{
			{"client_id", provider.ClientID, "the application's identifier from the provider"},
			{"client_secret", provider.ClientSecret, "the application's secret from the provider"},
			{"auth_url", provider.AuthURL, "where somebody is sent to approve the sign-in"},
			{"token_url", provider.TokenURL, "where the approval is exchanged for a token"},
			{"user_url", provider.UserURL, "where the signed-in account is read from"},
		}
		for _, r := range required {
			if strings.TrimSpace(r.value) == "" {
				l.problem(at(r.setting), "is required — %s", r.hint)
			}
		}

		for _, endpoint := range []struct{ setting, value string }{
			{"auth_url", provider.AuthURL},
			{"token_url", provider.TokenURL},
			{"user_url", provider.UserURL},
		} {
			if strings.TrimSpace(endpoint.value) == "" {
				continue
			}
			// Plain HTTP would put the secret and the returned token on the
			// wire in the clear, so it is refused rather than warned about.
			if !strings.HasPrefix(endpoint.value, "https://") {
				l.problem(at(endpoint.setting), "must be an https:// URL, got %q", endpoint.value)
			}
		}

		providers = append(providers, provider)
	}

	return OAuthConfig{Providers: providers}
}

// providerName is what may appear in oauth.providers. It is deliberately
// narrow: the name becomes part of a settings key, an environment variable and
// a callback path, and all three are happier without surprises in them.
var providerName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

/*
isLoopback says whether a host is this machine.

By name as well as by address, because an operator running a relay in the same
container writes "localhost" far more often than "127.0.0.1", and a check that
only understood the address would refuse the commonest correct configuration.
*/
func isLoopback(host string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(host))
	if trimmed == "localhost" || strings.HasSuffix(trimmed, ".localhost") {
		return true
	}
	if address, err := netip.ParseAddr(trimmed); err == nil {
		return address.IsLoopback()
	}
	return false
}
