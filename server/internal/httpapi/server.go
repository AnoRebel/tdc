// Package httpapi exposes the TDC server over HTTP.
package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/attachments"
	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/buildinfo"
	"github.com/AnoRebel/tdc/server/internal/cache"
	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/gifs"
	"github.com/AnoRebel/tdc/server/internal/identity"
	"github.com/AnoRebel/tdc/server/internal/moderation"
	"github.com/AnoRebel/tdc/server/internal/notify"
	"github.com/AnoRebel/tdc/server/internal/presence"
	"github.com/AnoRebel/tdc/server/internal/realtime"
	"github.com/AnoRebel/tdc/server/internal/rooms"
	"github.com/AnoRebel/tdc/server/internal/search"
	"github.com/AnoRebel/tdc/server/internal/unfurl"
)

// Config describes how the HTTP server should listen.
type Config struct {
	// Address is the host:port to listen on.
	Address string
	// ReadTimeout bounds how long reading a request may take.
	ReadTimeout time.Duration
	// WriteTimeout bounds how long writing a response may take.
	WriteTimeout time.Duration
	// IdleTimeout bounds how long an idle keep-alive connection is held.
	IdleTimeout time.Duration
	// ShutdownTimeout bounds how long in-flight requests are given to finish
	// once shutdown begins.
	ShutdownTimeout time.Duration
	// ExposeInternalErrors includes internal error detail in responses. It
	// exists for local development and must stay off in production, where
	// error text can disclose internals to an unauthenticated caller.
	ExposeInternalErrors bool
	// WebRoot is the directory holding the built web application, served from
	// this same origin. Empty in development.
	WebRoot string

	// ServerName is the operator-chosen display name.
	ServerName string
	// ServerDescription says who this server is for.
	ServerDescription string
	// CanonicalAddress is how this server is known to clients and peers. It is
	// never inferred from a request header, so it stays correct behind a proxy.
	CanonicalAddress string
	// DefaultLanguage is the interface language for users who have not chosen
	// one.
	DefaultLanguage string
	// AllowedOrigins lists browser origins permitted to call this API from
	// another origin. Empty is correct when the web app is served from the
	// same origin as the API, which is the normal deployment.
	AllowedOrigins []string

	// SecureCookies marks session cookies Secure, so a browser sends them
	// only over TLS. Off only for local development over plain HTTP.
	SecureCookies bool
	// SessionLifetime is how long a session lasts without being renewed.
	SessionLifetime time.Duration
	// Keepalive is how often an idle live connection is pinged. Configurable
	// because the right interval depends on what sits between the client and
	// the server, and because a phone on metered data pays for each one.
	Keepalive time.Duration
}

// Server owns the HTTP listener and the routes mounted on it.
type Server struct {
	echo        *echo.Echo
	http        *http.Server
	log         *slog.Logger
	db          *db.Pool
	identity    *identity.Service
	authz       *authz.Service
	audit       *audit.Recorder
	limiter     *cache.Limiter
	rooms       *rooms.Service
	attachments *attachments.Service
	unfurl      *unfurl.Service
	search      *search.Service
	notify      *notify.Service
	moderation  *moderation.Service
	gifs        *gifs.Service
	queries     *dbgen.Queries
	presence    *presence.Service
	hub         *realtime.Hub
	realtime    *realtime.Server
	config      Config

	// shell is the built application's index.html, held in memory so social
	// metadata can be inserted per request.
	shell []byte

	// background counts work detached from requests — notifications and search
	// indexing — so shutdown can wait for it rather than cutting it off.
	background sync.WaitGroup
}

// Services are the collaborators the HTTP layer needs.
//
// Grouped into one parameter rather than a growing argument list, so adding a
// dependency does not change every call site.
type Services struct {
	Identity    *identity.Service
	Authz       *authz.Service
	Audit       *audit.Recorder
	Limiter     *cache.Limiter
	Rooms       *rooms.Service
	Attachments *attachments.Service
	Unfurl      *unfurl.Service
	Search      *search.Service
	Notify      *notify.Service
	Moderation  *moderation.Service
	GIFs        *gifs.Service
	Presence    *presence.Service
}

// New builds a server with the health and version routes mounted. It does not
// listen until Run is called.
//
// The database pool may be nil, which health reporting accounts for; that
// keeps the server testable without a database for routes that do not need
// one.
func New(cfg Config, pool *db.Pool, services Services, log *slog.Logger) *Server {
	e := echo.New()
	e.Logger = log
	// Echo v5 takes a factory here, and the parameter order of the handler it
	// returns is (context, error) — reversed from v4.
	e.HTTPErrorHandler = echo.DefaultHTTPErrorHandler(cfg.ExposeInternalErrors)

	s := &Server{
		echo:        e,
		log:         log,
		db:          pool,
		identity:    services.Identity,
		authz:       services.Authz,
		audit:       services.Audit,
		limiter:     services.Limiter,
		rooms:       services.Rooms,
		attachments: services.Attachments,
		unfurl:      services.Unfurl,
		search:      services.Search,
		notify:      services.Notify,
		moderation:  services.Moderation,
		gifs:        services.GIFs,
		queries:     dbgen.New(pool),
		presence:    services.Presence,
		config:      cfg,
	}

	// The hub reads membership from the rooms service at delivery time, which
	// is what makes a removal take effect on a connection that stays open.
	if services.Rooms != nil {
		s.hub = realtime.NewHub(services.Rooms, log)
		var lifecycle realtime.Lifecycle
		if services.Presence != nil {
			lifecycle = services.Presence
		}
		s.realtime = realtime.NewServer(s.hub, roomReplayer{rooms: services.Rooms, attachments: services.Attachments},
			lifecycle, cfg.Keepalive, log)

		// Wired here rather than at construction because the hub is built
		// here: notifications are suppressed while somebody is looking at the
		// conversation, which only the live connections know, and while their
		// status says not to interrupt them.
		if services.Notify != nil {
			var silencing notify.Silencing
			if services.Presence != nil {
				silencing = services.Presence
			}
			services.Notify.WithSuppression(s.hub, silencing)
		}
	}

	if len(cfg.AllowedOrigins) > 0 {
		e.Use(cors(cfg.AllowedOrigins))
	}
	e.Use(requestLogger(log))
	e.Use(recoverPanic(log))
	// Resolves the session if the request carries one. It does not refuse an
	// unauthenticated request: some routes are public, and requiring
	// authentication is the job of requireAuthentication on the routes that
	// need it.
	e.Use(s.authenticate)

	s.routes()

	s.http = &http.Server{
		Addr:              cfg.Address,
		Handler:           e,
		ReadTimeout:       cfg.ReadTimeout,
		ReadHeaderTimeout: cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	return s
}

// Handler exposes the router, so tests can drive it without a listener.
func (s *Server) Handler() http.Handler { return s.echo }

// Run listens until ctx is cancelled, then shuts down gracefully, allowing
// in-flight requests up to ShutdownTimeout to finish.
func (s *Server) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.config.Address)
	if err != nil {
		return err
	}

	serveErr := make(chan error, 1)
	go func() {
		s.log.Info("server listening",
			slog.String("address", listener.Addr().String()),
			slog.String("version", buildinfo.Version),
		)
		if err := s.http.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}

	s.log.Info("shutting down", slog.Duration("timeout", s.config.ShutdownTimeout))

	// Live connections are long-lived by design, so they would otherwise hold
	// the drain open for its full timeout.
	if s.hub != nil {
		s.hub.CloseAll()
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.config.ShutdownTimeout)
	defer cancel()

	// Work detached from requests — notifications, search indexing — is waited
	// for, so the server does not exit with a notification half-written. Each
	// piece carries its own deadline, so this cannot wait indefinitely.
	s.WaitForBackground()

	if err := s.http.Shutdown(shutdownCtx); err != nil {
		// Shutdown returning an error means in-flight requests outlasted the
		// grace period. Close drops them rather than hanging forever.
		s.log.Warn("graceful shutdown incomplete, closing", slog.String("error", err.Error()))
		if closeErr := s.http.Close(); closeErr != nil {
			return errors.Join(err, closeErr)
		}
		return err
	}

	s.log.Info("shutdown complete")
	return <-serveErr
}
