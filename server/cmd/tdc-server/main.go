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

	"github.com/AnoRebel/tdc/server/internal/apitokens"
	"github.com/AnoRebel/tdc/server/internal/attachments"
	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/backup"
	"github.com/AnoRebel/tdc/server/internal/buildinfo"
	"github.com/AnoRebel/tdc/server/internal/cache"
	"github.com/AnoRebel/tdc/server/internal/config"
	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/federation"
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
	"github.com/AnoRebel/tdc/server/internal/realtime"
	"github.com/AnoRebel/tdc/server/internal/recovery"
	"github.com/AnoRebel/tdc/server/internal/rooms"
	"github.com/AnoRebel/tdc/server/internal/search"
	"github.com/AnoRebel/tdc/server/internal/secondfactor"
	"github.com/AnoRebel/tdc/server/internal/settings"
	"github.com/AnoRebel/tdc/server/internal/storage"
	"github.com/AnoRebel/tdc/server/internal/unfurl"
)

func main() {
	restart, err := run()
	if err != nil {
		// Configuration problems are reported in full, one per line, so an
		// operator can fix every one before restarting.
		fmt.Fprintf(os.Stderr, "tdc-server: %v\n", err)
		os.Exit(1)
	}
	// Shut down cleanly to come back: everything run held is closed by now.
	if restart.operation != uuid.Nil {
		if err := comeBack(restart.method, restart.operation); err != nil {
			fmt.Fprintf(os.Stderr, "tdc-server: %v\n", err)
			os.Exit(1)
		}
	}
}

// restartRequest is a restart that shutdown was for.
type restartRequest struct {
	operation uuid.UUID
	method    string
}

func run() (restartRequest, error) {
	var (
		configPath  = flag.String("config", "", "path to the configuration file (optional; environment alone is enough)")
		showVersion = flag.Bool("version", false, "print the version and exit")
		pushKeys    = flag.Bool("push-keys", false,
			"generate a push notification key pair and exit")
		checkOnly = flag.Bool("check", false,
			"check the configuration and the database this server would start with, change nothing, and exit")
	)
	flag.Parse()

	if *checkOnly {
		if err := check(*configPath); err != nil {
			return restartRequest{}, err
		}
		fmt.Println("tdc-server: the configuration and database are ready")
		return restartRequest{}, nil
	}

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
			return restartRequest{}, err
		}

		fmt.Println("# Notifications to closed browsers. Keep the private key secret:")
		fmt.Println("# anybody holding it can send as this server to every device")
		fmt.Println("# that has subscribed.")
		fmt.Printf("TDC_PUSH_VAPID_PUBLIC=%s\n", keys.Public)
		fmt.Printf("TDC_PUSH_VAPID_PRIVATE=%s\n", keys.Private)
		fmt.Println("# A way for a push service to reach you about a problem:")
		fmt.Println("TDC_PUSH_SUBJECT=mailto:you@example.org")
		return restartRequest{}, nil
	}

	if *showVersion {
		info := buildinfo.Current()
		fmt.Printf("tdc-server %s (protocol %s)\n", info.Version, info.ProtocolVersion)
		if info.Commit != "" {
			fmt.Printf("commit %s built %s\n", info.Commit, info.BuildTime)
		}
		return restartRequest{}, nil
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return restartRequest{}, err
	}

	level := &slog.LevelVar{}
	level.Set(parseLevel(cfg.Log.Level))
	log := newLogger(cfg.Log, level)
	startedAt := time.Now()

	for _, warning := range cfg.Warnings() {
		log.Warn(warning)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cfg.Database.AutoMigrate {
		if err := db.Migrate(ctx, cfg.Database.URL, log); err != nil {
			return restartRequest{}, err
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
		return restartRequest{}, err
	}
	defer pool.Close()

	queries := dbgen.New(pool)

	/*
		Where short-lived shared state lives.

		In-process by default, which is right for the single-node deployment
		almost every operator runs and needs nothing installed. Configured with
		a Valkey, the rate-limit counters become one shared count rather than
		one per process — which is the difference between a limit of five
		attempts and five attempts per node.

		Checked at startup rather than on the first request: a server pointing
		at a cache that is not there should say so now, not once somebody is
		trying to sign in.
	*/
	var (
		sharedCache cache.Cache
		limiter     cache.Rates
	)
	if cfg.Cache.Address != "" {
		valkeyCache, err := cache.NewValkey(cfg.Cache)
		if err != nil {
			return restartRequest{}, err
		}
		pingCtx, cancelPing := context.WithTimeout(ctx, 5*time.Second)
		err = valkeyCache.Ping(pingCtx)
		cancelPing()
		if err != nil {
			return restartRequest{}, fmt.Errorf("the configured cache at %s: %w", cfg.Cache.Address, err)
		}

		log.Info("counting rate limits in the shared cache", "address", cfg.Cache.Address)
		sharedCache = valkeyCache
		limiter = cache.NewSharedLimiter(valkeyCache)
	} else {
		memory := cache.NewMemory()
		sharedCache = memory
		limiter = cache.NewLimiter(memory)
	}
	defer func() { _ = sharedCache.Close() }()

	// The signing key identifies this server. It is loaded from storage rather
	// than generated per start: a server that made a new key each time would
	// invalidate every signature it had ever produced.
	signingKey, err := loadOrCreateSigningKey(ctx, queries)
	if err != nil {
		return restartRequest{}, err
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
		return restartRequest{}, err
	}
	defer objectStore.Close()

	// Named, because the background worker that removes expired content needs
	// the same instances the request handlers use rather than second ones with
	// their own caches.
	attachmentService := newAttachmentService(pool, objectStore, cfg, log)
	moderationService := moderation.NewService(pool, log)
	settingsService := settings.NewService(pool, log)
	auditRecorder := audit.NewRecorder(queries, log)

	/*
		Who this process is, to the other nodes and to a restart.

		The node identifier is new for each process; the host is what stays the
		same across a restart, which is how a process started by a supervisor —
		told nothing by the one before it — finds the restart it completes.
	*/
	node := realtime.NodeID()
	hostname, _ := os.Hostname()
	host := hostname + " " + cfg.Server.Address

	backups := backup.NewService(pool.Pool, objectStore, backup.Config{
		Node: node, Host: host, ServerName: serverName(cfg), ServerVersion: buildinfo.Version,
		WorkDirectory:      cfg.Backup.WorkDirectory,
		SchedulePassphrase: cfg.Backup.Passphrase,
		MaxBytes:           func(ctx context.Context) int64 { return settingsService.Int(ctx, settings.BackupMaxBytes) },
		Interval: func(ctx context.Context) time.Duration {
			return time.Duration(settingsService.Int(ctx, settings.BackupIntervalHours)) * time.Hour
		},
		Keep: func(ctx context.Context) int { return int(settingsService.Int(ctx, settings.BackupKeep)) },
	}, log)

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
		return restartRequest{}, err
	}

	/*
		Mail, and the recovery that depends on it.

		Both present whatever the configuration says: a sender with no host
		reports that it cannot send, and recovery asks it before offering
		anything. That keeps "this server sends no mail" one answer in one
		place rather than a nil check at every call site.
	*/
	mailService := mail.NewService(cfg.Mail, serverName(cfg), log)

	/*
		An emailed code is offered only where it could actually arrive.

		Both halves: a server that sends mail, and an address somebody has
		proved. An unconfirmed address is something typed into a form, and
		mailing a way past the second factor to one would undo the factor.
	*/
	secondFactorService := secondfactor.NewService(serverName(cfg), queries, log)
	secondFactorService.UseMail(func(ctx context.Context, userID uuid.UUID) bool {
		if !mailService.Configured() {
			return false
		}
		user, err := queries.GetUserByID(ctx, userID)
		if err != nil {
			return false
		}
		return user.EmailVerified && user.Email != nil
	})
	recoveryService := recovery.NewService(queries, mailService,
		cfg.Server.CanonicalAddress, serverName(cfg), log)

	/*
		How this node hears about events stored by another.

		Alone unless an operator says otherwise: a single-node server has
		nothing to hear from, and a broker there would be a connection held open
		and a round trip through the database to show somebody their own
		message.

		The multi-node broker is Postgres's own LISTEN and NOTIFY rather than a
		queue to install — the operator this is for already runs Postgres, and
		asking them to run and maintain a broker as well would be asking more
		than the problem is worth.
	*/
	var broker realtime.Broker = realtime.Alone{}
	if cfg.Server.MultiNode {
		log.Info("expecting other nodes; events will be shared through the database",
			"node", node)
		broker = realtime.NewPostgresBroker(pool.Pool, node, log)
	}

	services := httpapi.Services{
		Identity: identity.NewService(queries, log),
		Authz:    authz.NewService(queries),
		Audit:    auditRecorder,
		Limiter:  limiter,
		// Everything the web client does is an HTTP call somebody else can
		// make; this is what lets them make it.
		APITokens: apitokens.NewService(queries),
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
		Unfurl: unfurl.NewService(sharedCache,
			unfurl.Settings{Enabled: cfg.Attachments.LinkPreviews}, log),
		Presence: presenceService,
		Broker:   broker,
		Push:     pushService,
		Passkeys: passkeyService,
		// Always present: optional per account rather than per server, and an
		// operator may require it of everybody or of administrators.
		SecondFactor: secondFactorService,
		Mail:         mailService,
		Recovery:     recoveryService,
		Backups:      backups,
	}

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
		TURNSecret:             cfg.Calls.TURNSecret,
		DefaultLanguage:        cfg.Server.DefaultLanguage,
		AllowedOrigins:         cfg.Server.AllowedOrigins,
		SecureCookies:          cfg.Server.SecureCookies,
		SessionLifetime:        cfg.Server.SessionLifetime,
		Keepalive:              cfg.Server.Keepalive,
	}, pool, services, log)

	control := &controller{
		configPath: *configPath, level: level, log: log, server: server,
		settings: settingsService, stop: stop, current: cfg,
	}
	resumed, _ := uuid.Parse(os.Getenv(restartOperationVariable))
	_ = os.Unsetenv(restartOperationVariable)
	server.SetLifecycle(httpapi.Lifecycle{
		Node: node, Host: host, StartedAt: startedAt,
		Reload:           control.reload,
		PlanRestart:      control.plan,
		Describe:         control.describe,
		Restart:          control.begin,
		ResumedOperation: resumed,
	})

	// SIGHUP reloads, as daemons conventionally do.
	hangups := make(chan os.Signal, 1)
	signal.Notify(hangups, syscall.SIGHUP)
	defer signal.Stop(hangups)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-hangups:
				log.Info("reloading on SIGHUP")
				server.ReloadFromSignal(ctx)
			}
		}
	}()

	if err := server.Run(ctx); err != nil {
		return restartRequest{}, err
	}
	if operation := control.restarting(); operation != uuid.Nil {
		return restartRequest{operation: operation, method: control.describe().Method}, nil
	}
	return restartRequest{}, nil
}

// newLogger builds the logger described by the configuration. JSON suits log
// aggregation; text suits a terminal during development.
func newLogger(cfg config.LogConfig, level *slog.LevelVar) *slog.Logger {
	// A variable rather than a value, so a reload can change it in place.
	options := &slog.HandlerOptions{Level: level}

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
/*
serverName is what this server calls itself inside addresses.

A host, not a URL. Addresses are room@server and user@server, and an operator
who configured "https://tdc.example.org" would otherwise get
"general@https://tdc.example.org" — which is wrong everywhere it appears and
catastrophic in federation, where two servers computing an address for the same
room must reach the same string or neither will accept the other's events.

The host is also how peers are identified in the trust lists and how a room's
home server is recorded, so deriving it the same way here keeps those three
agreeing by construction rather than by coincidence.
*/
func serverName(cfg config.Config) string {
	if cfg.Server.CanonicalAddress == "" {
		return "localhost"
	}
	host, err := federation.Host(cfg.Server.CanonicalAddress)
	if err != nil {
		return cfg.Server.CanonicalAddress
	}
	return host
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
