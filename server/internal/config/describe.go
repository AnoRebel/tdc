package config

import (
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
)

// Setting is one configuration value as it currently stands, for display.
type Setting struct {
	Key string
	// Value is redacted when the setting is a secret.
	Value string
	// Source says where the value came from: "default", "file" or
	// "environment". An administrator inspecting configuration needs to know
	// whether a value was chosen or merely defaulted.
	Source string
	// Secret marks values that must never be displayed in full.
	Secret bool
}

// settingValue is one row of the listing below: the key, what it currently is,
// and what it would have been had nobody set it.
type settingValue struct {
	key      string
	current  string
	fallback string
}

// secretKeys are settings whose values must be redacted wherever configuration
// is displayed, logged or exported.
var secretKeys = map[string]bool{
	"database.url": true,
	// The object store's credentials. The access key is an identifier rather
	// than a secret, but it is redacted too: together the pair is a working
	// credential, and an operator reading this view needs neither half of it.
	"storage.s3.access_key": true,
	"storage.s3.secret_key": true,
	/*
		The push signing key. Anybody holding it can send notifications as this
		server to every device that has subscribed to it, which is why it is
		redacted wherever configuration is shown or exported.

		The public half is deliberately absent from this list: it is handed to
		every browser that subscribes, so hiding it from the operator would
		conceal nothing while making a misconfiguration harder to diagnose.
	*/
	"push.vapid_private": true,
	/*
		The GIF provider's API key. It is billed to whoever holds it, so it is
		redacted for the same reason the storage credentials are.
	*/
	"gifs.api_key": true,
	/*
		The mail account's password. Whoever holds it can send mail as this
		server, which for a server that sends recovery links is the whole
		account-takeover story in one credential.
	*/
	"mail.password": true,
	// The cache's password. Whoever holds it can read and rewrite every
	// rate-limit counter on the server.
	"cache.password": true,
}

/*
isSecret says whether a setting must be redacted wherever configuration is
shown.

Most secrets are a fixed list, but a sign-in provider's client secret is not:
the key contains the provider's own name, so "oauth.github.client_secret" and
"oauth.forgejo.client_secret" are different keys naming the same kind of thing.
Matching the ending rather than the whole key means a provider added later is
redacted without anybody remembering to come back here.

The client_id is left visible on purpose. It is handed to every browser that
starts a sign-in, so hiding it would conceal nothing while making a
misconfiguration harder to see.
*/
func isSecret(key string) bool {
	if secretKeys[key] {
		return true
	}
	return strings.HasPrefix(key, "oauth.") && strings.HasSuffix(key, ".client_secret")
}

// Redact returns a value safe to display.
//
// A database URL keeps its shape but loses its password, because an operator
// checking configuration needs to confirm the host and database name, and
// replacing the whole value would defeat that.
func Redact(key, value string) string {
	if value == "" || !isSecret(key) {
		return value
	}

	if parsed, err := url.Parse(value); err == nil && parsed.User != nil {
		if _, hasPassword := parsed.User.Password(); hasPassword {
			parsed.User = url.UserPassword(parsed.User.Username(), "redacted")
			return parsed.String()
		}
		return value
	}

	return "redacted"
}

// Describe returns every setting with its current value and origin, with
// secrets redacted. It backs the administrative view of configuration.
func (c Config) Describe(path string) []Setting {
	fileValues := map[string]string{}
	if path != "" {
		if parsed, err := parseFile(path); err == nil {
			fileValues = parsed
		}
	}

	defaults := Default()
	values := []settingValue{
		{"server.name", c.Server.Name, defaults.Server.Name},
		{"server.description", c.Server.Description, defaults.Server.Description},
		{"server.canonical_address", c.Server.CanonicalAddress, defaults.Server.CanonicalAddress},
		{"server.address", c.Server.Address, defaults.Server.Address},
		{"server.default_language", c.Server.DefaultLanguage, defaults.Server.DefaultLanguage},
		{"server.read_timeout", c.Server.ReadTimeout.String(), defaults.Server.ReadTimeout.String()},
		{"server.write_timeout", c.Server.WriteTimeout.String(), defaults.Server.WriteTimeout.String()},
		{"server.idle_timeout", c.Server.IdleTimeout.String(), defaults.Server.IdleTimeout.String()},
		{"server.shutdown_timeout", c.Server.ShutdownTimeout.String(), defaults.Server.ShutdownTimeout.String()},
		{"server.expose_internal_errors", fmt.Sprint(c.Server.ExposeInternalErrors), fmt.Sprint(defaults.Server.ExposeInternalErrors)},
		{"database.url", c.Database.URL, defaults.Database.URL},
		{"database.max_connections", fmt.Sprint(c.Database.MaxConnections), fmt.Sprint(defaults.Database.MaxConnections)},
		{"database.min_connections", fmt.Sprint(c.Database.MinConnections), fmt.Sprint(defaults.Database.MinConnections)},
		{"database.connect_timeout", c.Database.ConnectTimeout.String(), defaults.Database.ConnectTimeout.String()},
		{"database.max_conn_lifetime", c.Database.MaxConnLifetime.String(), defaults.Database.MaxConnLifetime.String()},
		{"database.max_conn_idle_time", c.Database.MaxConnIdleTime.String(), defaults.Database.MaxConnIdleTime.String()},
		{"database.auto_migrate", fmt.Sprint(c.Database.AutoMigrate), fmt.Sprint(defaults.Database.AutoMigrate)},
		{"storage.backend", c.Storage.Backend, defaults.Storage.Backend},
		{"storage.directory", c.Storage.Directory, defaults.Storage.Directory},
		{"storage.s3.endpoint", c.Storage.S3.Endpoint, defaults.Storage.S3.Endpoint},
		{"storage.s3.bucket", c.Storage.S3.Bucket, defaults.Storage.S3.Bucket},
		{"storage.s3.region", c.Storage.S3.Region, defaults.Storage.S3.Region},
		{"storage.s3.access_key", c.Storage.S3.AccessKey, defaults.Storage.S3.AccessKey},
		{"storage.s3.secret_key", c.Storage.S3.SecretKey, defaults.Storage.S3.SecretKey},
		{"storage.s3.path_style", fmt.Sprint(c.Storage.S3.PathStyle), fmt.Sprint(defaults.Storage.S3.PathStyle)},
		{"push.vapid_public", c.Push.VAPIDPublic, defaults.Push.VAPIDPublic},
		{"push.vapid_private", c.Push.VAPIDPrivate, defaults.Push.VAPIDPrivate},
		{"push.subject", c.Push.Subject, defaults.Push.Subject},
		{"cache.address", c.Cache.Address, defaults.Cache.Address},
		{"cache.username", c.Cache.Username, defaults.Cache.Username},
		{"cache.password", c.Cache.Password, defaults.Cache.Password},
		{"cache.database", fmt.Sprint(c.Cache.Database), fmt.Sprint(defaults.Cache.Database)},
		{"cache.tls", fmt.Sprint(c.Cache.TLS), fmt.Sprint(defaults.Cache.TLS)},
		{"mail.host", c.Mail.Host, defaults.Mail.Host},
		{"mail.port", fmt.Sprint(c.Mail.Port), fmt.Sprint(defaults.Mail.Port)},
		{"mail.username", c.Mail.Username, defaults.Mail.Username},
		{"mail.password", c.Mail.Password, defaults.Mail.Password},
		{"mail.from", c.Mail.From, defaults.Mail.From},
		{"mail.from_name", c.Mail.FromName, defaults.Mail.FromName},
		{"mail.tls", c.Mail.TLS, defaults.Mail.TLS},
		{"mail.timeout", c.Mail.Timeout.String(), defaults.Mail.Timeout.String()},
		{"gifs.provider", c.GIFs.Provider, defaults.GIFs.Provider},
		{"gifs.api_key", c.GIFs.APIKey, defaults.GIFs.APIKey},
		{"log.level", c.Log.Level, defaults.Log.Level},
		{"log.format", c.Log.Format, defaults.Log.Format},
	}

	/*
		Each configured sign-in provider contributes its own settings.

		They are appended rather than listed above because there is no fixed set
		of them: which keys exist depends on what oauth.providers names. The
		secret among them is redacted by isSecret, which matches the ending of
		the key rather than the whole of it for exactly this reason.
	*/
	for _, provider := range c.OAuth.Providers {
		at := "oauth." + provider.Name + "."
		values = append(values,
			settingValue{at + "label", provider.Label, ""},
			settingValue{at + "client_id", provider.ClientID, ""},
			settingValue{at + "client_secret", provider.ClientSecret, ""},
			settingValue{at + "auth_url", provider.AuthURL, ""},
			settingValue{at + "token_url", provider.TokenURL, ""},
			settingValue{at + "user_url", provider.UserURL, ""},
			settingValue{at + "scopes", strings.Join(provider.Scopes, ","), ""},
		)
	}

	settings := make([]Setting, 0, len(values))
	for _, v := range values {
		settings = append(settings, Setting{
			Key:    v.key,
			Value:  Redact(v.key, v.current),
			Source: sourceOf(v.key, fileValues),
			Secret: isSecret(v.key),
		})
	}

	sort.Slice(settings, func(i, j int) bool { return settings[i].Key < settings[j].Key })
	return settings
}

func sourceOf(key string, fileValues map[string]string) string {
	if _, ok := os.LookupEnv(envName(key)); ok {
		return "environment"
	}
	if _, ok := fileValues[key]; ok {
		return "file"
	}
	return "default"
}

// Warnings reports settings that are valid but unsafe for production, so an
// operator is told rather than silently running an exposed server.
func (c Config) Warnings() []string {
	var warnings []string

	if c.Server.ExposeInternalErrors {
		warnings = append(warnings,
			"server.expose_internal_errors is on: internal error detail is returned to callers. Use only for local development.")
	}
	if c.Server.CanonicalAddress == "" {
		warnings = append(warnings,
			"server.canonical_address is not set: the server cannot state a stable address for clients or federated peers.")
	}
	if !c.Server.SecureCookies {
		warnings = append(warnings,
			"server.secure_cookies is off: session cookies are sent over plain HTTP and can be read in transit. Acceptable only for local development.")
	}
	if strings.Contains(c.Database.URL, "sslmode=disable") {
		warnings = append(warnings,
			"database.url disables TLS: traffic to the database is unencrypted. Acceptable only when the database is on the same host.")
	}

	return warnings
}
