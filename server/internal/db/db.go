// Package db owns the connection to Postgres and the schema it expects.
//
// Postgres is the only datastore. Queries are hand-written SQL in
// internal/db/queries, compiled to typed Go by sqlc, so that every statement
// the server issues is visible in review rather than produced at runtime by an
// ORM.
package db

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Config describes how to reach Postgres.
type Config struct {
	// URL is a libpq connection string or postgres:// URL.
	URL string
	// MaxConnections bounds the pool. The default suits a small server; a
	// larger deployment raises it, and Postgres' own max_connections must be
	// raised to match.
	MaxConnections int32
	// MinConnections keeps a few connections warm so the first request after
	// an idle period does not pay connection setup.
	MinConnections int32
	// ConnectTimeout bounds establishing a single connection.
	ConnectTimeout time.Duration
	// MaxConnLifetime recycles connections, so a long-lived server does not
	// hold a connection across a database restart or failover indefinitely.
	MaxConnLifetime time.Duration
	// MaxConnIdleTime closes connections that have gone unused.
	MaxConnIdleTime time.Duration
}

// Pool is a set of connections to Postgres.
type Pool struct {
	*pgxpool.Pool
	log *slog.Logger
}

// Open connects to Postgres and verifies the connection before returning, so
// that a misconfigured database fails at startup rather than on the first
// request.
func Open(ctx context.Context, cfg Config, log *slog.Logger) (*Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}

	if cfg.MaxConnections > 0 {
		poolCfg.MaxConns = cfg.MaxConnections
	}
	if cfg.MinConnections > 0 {
		poolCfg.MinConns = cfg.MinConnections
	}
	if cfg.ConnectTimeout > 0 {
		poolCfg.ConnConfig.ConnectTimeout = cfg.ConnectTimeout
	}
	if cfg.MaxConnLifetime > 0 {
		poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	}
	if cfg.MaxConnIdleTime > 0 {
		poolCfg.MaxConnIdleTime = cfg.MaxConnIdleTime
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to database: %w", err)
	}

	log.Info("database connected",
		slog.String("host", poolCfg.ConnConfig.Host),
		slog.String("database", poolCfg.ConnConfig.Database),
		slog.Int("maxConnections", int(poolCfg.MaxConns)),
	)

	return &Pool{Pool: pool, log: log}, nil
}

// Health reports whether the database is reachable, for the health endpoint.
func (p *Pool) Health(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return p.Ping(ctx)
}
