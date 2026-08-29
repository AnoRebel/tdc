// Command tdc-server runs a TDC server.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/AnoRebel/tdc/server/internal/buildinfo"
	"github.com/AnoRebel/tdc/server/internal/config"
	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/httpapi"
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
	}, pool, log)

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
