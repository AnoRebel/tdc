// Package httpapi exposes the TDC server over HTTP.
package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/buildinfo"
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
}

// Server owns the HTTP listener and the routes mounted on it.
type Server struct {
	echo   *echo.Echo
	http   *http.Server
	log    *slog.Logger
	config Config
}

// New builds a server with the health and version routes mounted. It does not
// listen until Run is called.
func New(cfg Config, log *slog.Logger) *Server {
	e := echo.New()
	e.Logger = log
	// Echo v5 takes a factory here, and the parameter order of the handler it
	// returns is (context, error) — reversed from v4.
	e.HTTPErrorHandler = echo.DefaultHTTPErrorHandler(cfg.ExposeInternalErrors)

	s := &Server{
		echo:   e,
		log:    log,
		config: cfg,
	}

	e.Use(requestLogger(log))
	e.Use(recoverPanic(log))

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

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.config.ShutdownTimeout)
	defer cancel()

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
