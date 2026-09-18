// Command tdc-server runs a TDC server.
package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/attachments"
	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/buildinfo"
	"github.com/AnoRebel/tdc/server/internal/cache"
	"github.com/AnoRebel/tdc/server/internal/config"
	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/gifs"
	"github.com/AnoRebel/tdc/server/internal/httpapi"
	"github.com/AnoRebel/tdc/server/internal/identity"
	"github.com/AnoRebel/tdc/server/internal/invitations"
	"github.com/AnoRebel/tdc/server/internal/mail"
	"github.com/AnoRebel/tdc/server/internal/moderation"
	"github.com/AnoRebel/tdc/server/internal/notify"
	"github.com/AnoRebel/tdc/server/internal/oauth"
	"github.com/AnoRebel/tdc/server/internal/passkeys"
	"github.com/AnoRebel/tdc/server/internal/presence"
	"github.com/AnoRebel/tdc/server/internal/push"
	"github.com/AnoRebel/tdc/server/internal/recovery"
	"github.com/AnoRebel/tdc/server/internal/rooms"
	"github.com/AnoRebel/tdc/server/internal/search"
	"github.com/AnoRebel/tdc/server/internal/secondfactor"
	"github.com/AnoRebel/tdc/server/internal/settings"
	"github.com/AnoRebel/tdc/server/internal/storage"
	"github.com/AnoRebel/tdc/server/internal/unfurl"
)

func main() {
	if err := run(); err != nil {
		// Configuration problems are reported in full, one per line, so an
		// operator can fix every one before restarting.
		fmt.Fprintf(os.Stderr, "tdc-server: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		configPath  = flag.String("config", "", "path to the configuration file (optional; environment alone is enough)")
		showVersion = flag.Bool("version", false, "print the version and exit")
		pushKeys    = flag.Bool("push-keys", false,
			"generate a push notification key pair and exit")
	)
	flag.Parse()

	/*
		Generating the keys push needs, so enabling it requires nothing but
		this server.

		A feature that told an operator to visit a website and paste a key back
		is one that does not work on a machine with no outbound access, and
		makes them trust a stranger with the identity of their own server.
	*/
	if *pushKeys {
		keys, err := push.GenerateKeys()
		if err != nil {
			return err
		}

		fmt.Println("# Notifications to closed browsers. Keep the private key secret:")
		fmt.Println("# anybody holding it can send as this server to every device")
		fmt.Println("# that has subscribed.")
		fmt.Printf("TDC_PUSH_VAPID_PUBLIC=%s\n", keys.Public)
		fmt.Printf("TDC_PUSH_VAPID_PRIVATE=%s\n", keys.Private)
		fmt.Println("# A way for a push service to reach you about a problem:")
		fmt.Println("TDC_PUSH_SUBJECT=mailto:you@example.org")
		return nil
	}

	if *showVersion {
		info := buildinfo.Current()
		fmt.Printf("tdc-server %s (protocol %s)\n", info.Version, info.ProtocolVersion)
		if info.Commit != "" {
			fmt.Printf("commit %s built %s\n", info.Commit, info.BuildTime)
		}
		return nil
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}

	log := newLogger(cfg.Log)

	for _, warning := range cfg.Warnings() {
		log.Warn(warning)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cfg.Database.AutoMigrate {
		if err := db.Migrate(ctx, cfg.Database.URL, log); err != nil {
			return err
		}
	}

	pool, err := db.Open(ctx, db.Config{
		URL:             cfg.Database.URL,
		MaxConnections:  cfg.Database.MaxConnections,
		MinConnections:  cfg.Database.MinConnections,
		ConnectTimeout:  cfg.Database.ConnectTimeout,
		MaxConnLifetime: cfg.Database.MaxConnLifetime,
		MaxConnIdleTime: cfg.Database.MaxConnIdleTime,
	}, log)
	if err != nil {
		return err
	}
	defer pool.Close()

	queries := dbgen.New(pool)

	// The in-process cache is the default, so a single-server deployment needs
	// no cache service. An operator running several processes configures a
	// shared implementation instead.
	memoryCache := cache.NewMemory()
	defer func() { _ = memoryCache.Close() }()

	// The signing key identifies this server. It is loaded from storage rather
	// than generated per start: a server that made a new key each time would
	// invalidate every signature it had ever produced.
	signingKey, err := loadOrCreateSigningKey(ctx, queries)
	if err != nil {
		return err
	}

	presenceService := presence.NewService(queries)

	// Presence and typing state lapse rather than being deleted on an event,
	// so a client that goes away without a clean close does not leave someone
	// permanently present or permanently typing. The sweep is what makes that
	// lapse take effect.
	go sweepPeriodically(ctx, presenceService, log)

	// Where attachments are kept is the operator's choice; the default keeps
	// them in a directory, so a small deployment needs no object store.
	objectStore, err := storage.Open(storage.Config{
		Backend:   storage.Backend(cfg.Storage.Backend),
		Directory: cfg.Storage.Directory,
		S3: storage.S3Config{
			Endpoint:     cfg.Storage.S3.Endpoint,
			Bucket:       cfg.Storage.S3.Bucket,
			Region:       cfg.Storage.S3.Region,
			AccessKey:    cfg.Storage.S3.AccessKey,
			SecretKey:    cfg.Storage.S3.SecretKey,
			UsePathStyle: cfg.Storage.S3.PathStyle,
		},
	})
	if err != nil {
		log.Error("attachment storage is unavailable", "error", err)
		return err
	}
	defer objectStore.Close()

	// Named, because the background worker that removes expired content needs
	// the same instances the request handlers use rather than second ones with
	// their own caches.
	attachmentService := newAttachmentService(pool, objectStore, cfg, log)
	moderationService := moderation.NewService(pool, log)
	settingsService := settings.NewService(pool, log)
	auditRecorder := audit.NewRecorder(queries, log)

	// Off unless the operator generated keys, which is a working state: a
	// notification still exists and is shown, it simply does not reach a
	// closed browser.
	pushService := push.NewService(pool, push.Keys{
		Public:  cfg.Push.VAPIDPublic,
		Private: cfg.Push.VAPIDPrivate,
		Subject: cfg.Push.Subject,
	}, log)

	// Content past its retention period, removed on the same principle as
	// lapsed presence: a policy nothing enforces is a policy that misleads the
	// people it was disclosed to.
	go expirePeriodically(ctx, moderationService, settingsService,
		attachmentService, auditRecorder, pushService, log)

	/*
		Passkeys, or nothing.

		Nil on a server with no canonical address: a passkey is bound to a
		hostname, and binding one to a guess produces passkeys that enrol and
		then stop working when the guess changes. The routes answer as if the
		feature were not there, which is the truth.
	*/
	passkeyService, err := passkeys.NewService(passkeys.Settings{
		DisplayName:      serverName(cfg),
		CanonicalAddress: cfg.Server.CanonicalAddress,
		Origins:          cfg.Server.AllowedOrigins,
	}, queries, log)
	if err != nil {
		return err
	}

	/*
		Mail, and the recovery that depends on it.

		Both present whatever the configuration says: a sender with no host
		reports that it cannot send, and recovery asks it before offering
		anything. That keeps "this server sends no mail" one answer in one
		place rather than a nil check at every call site.
	*/
	mailService := mail.NewService(cfg.Mail, serverName(cfg), log)
	recoveryService := recovery.NewService(queries, mailService,
		cfg.Server.CanonicalAddress, serverName(cfg), log)

	services := httpapi.Services{
		Identity: identity.NewService(queries, log),
		Authz:    authz.NewService(queries),
		Audit:    auditRecorder,
		Limiter:  cache.NewLimiter(memoryCache),
		Rooms: rooms.NewService(queries, events.NewKeyring(signingKey),
			serverName(cfg), log),
		Attachments: attachmentService,
		Search:      search.NewService(pool),
		Notify:      notify.NewService(pool, log),
		Moderation:  moderationService,
		// Policy an operator changes while the server runs, as distinct from
		// the configuration file that decides how the process is wired.
		Settings:    settingsService,
		Invitations: invitations.NewService(pool, log),
		/*
			Off unless an operator configured a provider. Endpoints are stated
			rather than discovered, so this needs no network at startup and a
			self-hosted provider is set up exactly like a commercial one.

			The same guarded dialer as link previews and GIF search: a
			provider's hostname resolving to a private address is how a
			configured endpoint becomes a way to reach this server's own
			network, and the day one does is the day this matters.
		*/
		OAuth: oauth.NewService(cfg.OAuth.Providers, queries, &http.Client{
			Timeout:   oauthTimeout,
			Transport: &http.Transport{DialContext: unfurl.DialContext},
		}, log),
		// Off unless an operator configured a provider and a key. Searches are
		// proxied through this server, so the key stays here and the provider
		// sees one server rather than everybody using it.
		GIFs: gifs.NewService(gifs.Settings{
			Provider:      gifs.Provider(cfg.GIFs.Provider),
			APIKey:        cfg.GIFs.APIKey,
			ContentFilter: cfg.GIFs.ContentFilter,
			Timeout:       cfg.GIFs.Timeout,
			// The same dialer that guards link previews: a provider hostname
			// should never resolve to a private address, and the day one does
			// is the day this matters.
		}, unfurl.DialContext, log),
		Unfurl: unfurl.NewService(memoryCache,
			unfurl.Settings{Enabled: cfg.Attachments.LinkPreviews}, log),
		Presence: presenceService,
		Push:     pushService,
		Passkeys: passkeyService,
		// Always present: optional per account rather than per server, and an
		// operator may require it of everybody or of administrators.
		SecondFactor: secondfactor.NewService(serverName(cfg), queries, log),
		Mail:         mailService,
		Recovery:     recoveryService,
	}

	// Messages written now and sent later. The pass takes a row lock and skips
	// what is already locked, so running more than one server does not send
	// anything twice.
	go deliverScheduledPeriodically(ctx, services.Rooms, services.Attachments, log)

	server := httpapi.New(httpapi.Config{
		// Described once at startup: the values cannot change without a
		// restart, so re-resolving them per request would report the same
		// thing after paying to look it up.
		EffectiveConfiguration: cfg.Describe(*configPath),
		ConfigurationWarnings:  cfg.Warnings(),
		Address:                cfg.Server.Address,
		ReadTimeout:            cfg.Server.ReadTimeout,
		WriteTimeout:           cfg.Server.WriteTimeout,
		IdleTimeout:            cfg.Server.IdleTimeout,
		ShutdownTimeout:        cfg.Server.ShutdownTimeout,
		ExposeInternalErrors:   cfg.Server.ExposeInternalErrors,
		WebRoot:                cfg.Server.WebRoot,
		ServerName:             cfg.Server.Name,
		ServerDescription:      cfg.Server.Description,
		CanonicalAddress:       cfg.Server.CanonicalAddress,
		DefaultLanguage:        cfg.Server.DefaultLanguage,
		AllowedOrigins:         cfg.Server.AllowedOrigins,
		SecureCookies:          cfg.Server.SecureCookies,
		SessionLifetime:        cfg.Server.SessionLifetime,
		Keepalive:              cfg.Server.Keepalive,
	}, pool, services, log)

	return server.Run(ctx)
}

// newLogger builds the logger described by the configuration. JSON suits log
// aggregation; text suits a terminal during development.
func newLogger(cfg config.LogConfig) *slog.Logger {
	options := &slog.HandlerOptions{Level: parseLevel(cfg.Level)}

	var handler slog.Handler
	if cfg.Format == "text" {
		handler = slog.NewTextHandler(os.Stdout, options)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, options)
	}

	return slog.New(handler)
}

func parseLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// serverName is the name this server publishes in addresses.
//
// The canonical address when the operator has set one, since that is what
// clients and peers use to reach this server; otherwise a local default, which
// is adequate until the server federates.
func serverName(cfg config.Config) string {
	if cfg.Server.CanonicalAddress != "" {
		return cfg.Server.CanonicalAddress
	}
	return "localhost"
}

// signingKeyConfigKey is where the server's signing seed is stored.
const signingKeyConfigKey = "server.signing_key_seed"

// loadOrCreateSigningKey returns this server's signing key, generating one on
// first start.
//
// Stored in the database rather than a file so that the key travels with the
// data it signs: a server restored from a backup keeps the identity its events
// were signed with, and history stays verifiable.
func loadOrCreateSigningKey(ctx context.Context, queries *dbgen.Queries) (*events.KeyPair, error) {
	stored, err := queries.GetServerConfigValue(ctx, signingKeyConfigKey)
	if err == nil {
		seed, decodeErr := base64.RawURLEncoding.DecodeString(stored.Value)
		if decodeErr != nil {
			return nil, fmt.Errorf("stored signing key is not valid: %w", decodeErr)
		}
		return events.KeyPairFromSeed("key-1", seed)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("read signing key: %w", err)
	}

	key, err := events.NewKeyPair("key-1")
	if err != nil {
		return nil, err
	}

	actor := "system"
	if err := queries.SetServerConfigValue(ctx, dbgen.SetServerConfigValueParams{
		Key:   signingKeyConfigKey,
		Value: base64.RawURLEncoding.EncodeToString(key.Seed()),
		// Anything holding this can sign as this server, so it is redacted
		// wherever configuration is displayed.
		IsSecret:  true,
		UpdatedBy: &actor,
	}); err != nil {
		return nil, fmt.Errorf("store signing key: %w", err)
	}

	return key, nil
}

// sweepInterval is how often lapsed presence, statuses and typing indications
// are cleared.
//
// Frequent enough that a stale indicator does not linger noticeably, rare
// enough to be negligible work.
const sweepInterval = 30 * time.Second

// sweepPeriodically clears state that has lapsed, until the context ends.
func sweepPeriodically(ctx context.Context, service *presence.Service, log *slog.Logger) {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := service.Sweep(ctx); err != nil && ctx.Err() == nil {
				// Logged rather than fatal: a failed sweep means a stale
				// indicator lingers until the next one, not that the server
				// cannot serve.
				log.Error("presence sweep failed", slog.String("error", err.Error()))
			}
		}
	}
}

/*
scheduleInterval is how often due messages are looked for.

A minute rather than a second: somebody scheduling a message for tomorrow
morning does not care about the difference, and the query runs against an index
of exactly what is due, so a pass that finds nothing costs almost nothing.
*/
const scheduleInterval = time.Minute

// deliverScheduledPeriodically sends messages that have come due.
func deliverScheduledPeriodically(
	ctx context.Context,
	roomService *rooms.Service,
	attachmentService *attachments.Service,
	log *slog.Logger,
) {
	ticker := time.NewTicker(scheduleInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sent, err := roomService.DeliverDue(ctx, 50, attachmentService.Bind, log)
			if err != nil && ctx.Err() == nil {
				// Logged rather than fatal: a failed pass means a message goes
				// out on the next one, not that the server cannot serve.
				log.Error("scheduled delivery failed", slog.String("error", err.Error()))
			}
			if sent > 0 {
				log.Info("sent scheduled messages", "count", sent)
			}
		}
	}
}

/*
retentionInterval is how often content past its retention period is removed.

Every few minutes rather than every few seconds. A retention period is measured
in days or weeks, so the difference between removing something now and removing
it in five minutes is not one anybody can perceive — and a pass that finds
nothing still costs a query.
*/
const retentionInterval = 5 * time.Minute

/*
oauthTimeout bounds a request to a sign-in provider.

Two requests happen inside somebody's callback — a token exchange and reading
the account — and a provider that hangs must not hold that request open. Fifteen
seconds is long enough for a slow self-hosted provider on a slow link and short
enough that a dead one fails rather than lingers.
*/
const oauthTimeout = 15 * time.Second

// retentionBatch bounds one pass.
//
// A server that has run with no policy and then gets one has everything it has
// ever held to expire at once. Draining it over successive passes keeps each
// one short rather than holding locks for however long the whole backlog takes.
const retentionBatch = 200

// expirePeriodically removes content past its retention period, until the
// context ends.
func expirePeriodically(
	ctx context.Context,
	moderationService *moderation.Service,
	settingsService *settings.Service,
	attachmentService *attachments.Service,
	auditRecorder *audit.Recorder,
	pushService *push.Service,
	log *slog.Logger,
) {
	ticker := time.NewTicker(retentionInterval)
	defer ticker.Stop()

	removeAttachments := func(ctx context.Context, ids []uuid.UUID) error {
		for _, id := range ids {
			if err := attachmentService.Delete(ctx, id); err != nil {
				return err
			}
		}
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Read each pass rather than held, so an operator's change takes
			// effect without a restart.
			defaultSeconds := int32(settingsService.Int(ctx, settings.RetentionSeconds))

			removed, err := moderationService.ExpireDue(
				ctx, defaultSeconds, retentionBatch, removeAttachments)
			if err != nil && ctx.Err() == nil {
				// Logged rather than fatal: a failed pass means content goes
				// on the next one, not that the server cannot serve.
				log.Error("expiring content failed", slog.String("error", err.Error()))
			}
			if removed > 0 {
				log.Info("expired content past its retention period", "messages", removed)
			}

			/*
				Messages carrying their own lifetime, on the same pass.

				Separate from retention because the two answer to different
				policies — one the operator's, one the sender's — and a message
				can be subject to both. Running them together means one timer
				rather than two doing the same kind of work.
			*/
			byLifetime, err := moderationService.ExpireLifetimes(
				ctx, retentionBatch, removeAttachments)
			if err != nil && ctx.Err() == nil {
				log.Error("expiring messages by lifetime failed",
					slog.String("error", err.Error()))
			}
			if byLifetime > 0 {
				log.Info("expired messages by their own lifetime", "messages", byLifetime)
			}

			/*
				Audit entries past their own retention, which is a separate
				policy from the one governing messages.

				Kept indefinitely unless an operator says otherwise: a log that
				quietly discarded its own history would be one where the answer
				to a question depends on how promptly somebody thought to ask.
			*/
			auditDays := settingsService.Int(ctx, settings.AuditRetentionDays)
			if auditDays > 0 {
				pruned, err := auditRecorder.Prune(ctx,
					time.Duration(auditDays)*24*time.Hour)
				if err != nil && ctx.Err() == nil {
					log.Error("pruning the audit log failed",
						slog.String("error", err.Error()))
				}
				if pruned > 0 {
					log.Info("pruned audit entries", "entries", pruned)
				}
			}

			/*
				Devices that stopped answering.

				Their push service never said they were gone — a service that
				says so has them removed at once — they simply stopped working.
				Without this pass they are retried on every notification for as
				long as the account exists, which is a request per notification
				forever for a phone that was thrown away.
			*/
			// Prune reports what it removed itself, so the count is not
			// logged twice.
			if pushService != nil {
				if _, err := pushService.Prune(ctx); err != nil && ctx.Err() == nil {
					log.Error("pruning push subscriptions failed",
						slog.String("error", err.Error()))
				}
			}
		}
	}
}

// newAttachmentService wires attachments, with malware scanning only when an
// operator has configured a scanner.
//
// Scanning is optional and off by default: this server must work without any
// external service, which is what makes it possible to self-host at no cost.
func newAttachmentService(
	pool *db.Pool,
	store storage.Store,
	cfg config.Config,
	log *slog.Logger,
) *attachments.Service {
	service := attachments.NewService(pool, store, attachments.DefaultLimits(), log)

	// Preview generation is done in this process unless an operator has asked
	// for the isolation of a separate container.
	if address := cfg.Attachments.MediaSidecarAddress; address != "" {
		log.Info("using a media sidecar for previews", "address", address)
		service = service.WithProcessor(
			attachments.NewSidecarProcessor(address, cfg.Attachments.MediaSidecarTimeout),
		)
	}

	if address := cfg.Attachments.ScannerAddress; address != "" {
		log.Info("malware scanning is enabled",
			"address", address, "failOpen", cfg.Attachments.ScannerFailOpen)

		return service.WithScanner(
			attachments.NewClamAVScanner(address, cfg.Attachments.ScannerTimeout),
			cfg.Attachments.ScannerFailOpen,
		)
	}

	return service
}
