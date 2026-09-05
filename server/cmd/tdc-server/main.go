// Command tdc-server runs a TDC server.
package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/attachments"
	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/buildinfo"
	"github.com/AnoRebel/tdc/server/internal/cache"
	"github.com/AnoRebel/tdc/server/internal/config"
	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/httpapi"
	"github.com/AnoRebel/tdc/server/internal/identity"
	"github.com/AnoRebel/tdc/server/internal/moderation"
	"github.com/AnoRebel/tdc/server/internal/notify"
	"github.com/AnoRebel/tdc/server/internal/presence"
	"github.com/AnoRebel/tdc/server/internal/rooms"
	"github.com/AnoRebel/tdc/server/internal/search"
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
	)
	flag.Parse()

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

	services := httpapi.Services{
		Identity: identity.NewService(queries, log),
		Authz:    authz.NewService(queries),
		Audit:    audit.NewRecorder(queries, log),
		Limiter:  cache.NewLimiter(memoryCache),
		Rooms: rooms.NewService(queries, events.NewKeyring(signingKey),
			serverName(cfg), log),
		Attachments: newAttachmentService(pool, objectStore, cfg, log),
		Search:      search.NewService(pool),
		Notify:      notify.NewService(pool, log),
		Moderation:  moderation.NewService(pool, log),
		Unfurl: unfurl.NewService(memoryCache,
			unfurl.Settings{Enabled: cfg.Attachments.LinkPreviews}, log),
		Presence: presenceService,
	}

	// Messages written now and sent later. The pass takes a row lock and skips
	// what is already locked, so running more than one server does not send
	// anything twice.
	go deliverScheduledPeriodically(ctx, services.Rooms, services.Attachments, log)

	server := httpapi.New(httpapi.Config{
		Address:              cfg.Server.Address,
		ReadTimeout:          cfg.Server.ReadTimeout,
		WriteTimeout:         cfg.Server.WriteTimeout,
		IdleTimeout:          cfg.Server.IdleTimeout,
		ShutdownTimeout:      cfg.Server.ShutdownTimeout,
		ExposeInternalErrors: cfg.Server.ExposeInternalErrors,
		ServerName:           cfg.Server.Name,
		ServerDescription:    cfg.Server.Description,
		CanonicalAddress:     cfg.Server.CanonicalAddress,
		DefaultLanguage:      cfg.Server.DefaultLanguage,
		AllowedOrigins:       cfg.Server.AllowedOrigins,
		SecureCookies:        cfg.Server.SecureCookies,
		SessionLifetime:      cfg.Server.SessionLifetime,
		Keepalive:            cfg.Server.Keepalive,
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
