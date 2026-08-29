package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/labstack/echo/v5"
)

// requestLogger records one line per request.
//
// It deliberately logs the route pattern rather than the raw path: the pattern
// is bounded, so it can be aggregated, while raw paths contain identifiers that
// would both explode cardinality and put user data in the logs.
func requestLogger(log *slog.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			start := time.Now()
			err := next(c)

			status := http.StatusOK
			if res, unwrapErr := echo.UnwrapResponse(c.Response()); unwrapErr == nil {
				status = res.Status
			}
			if err != nil {
				// Echo's sentinel errors (ErrNotFound and friends) are an
				// unexported type, not *HTTPError, so match the interface both
				// they and *HTTPError implement rather than a concrete type.
				var coder echo.HTTPStatusCoder
				if errors.As(err, &coder) {
					status = coder.StatusCode()
				} else {
					status = http.StatusInternalServerError
				}
			}

			level := slog.LevelInfo
			switch {
			case status >= 500:
				level = slog.LevelError
			case status >= 400:
				level = slog.LevelWarn
			}

			attrs := []slog.Attr{
				slog.String("method", c.Request().Method),
				slog.String("route", c.RouteInfo().Path),
				slog.Int("status", status),
				slog.Duration("duration", time.Since(start)),
			}
			if err != nil {
				attrs = append(attrs, slog.String("error", err.Error()))
			}

			log.LogAttrs(c.Request().Context(), level, "request", attrs...)
			return err
		}
	}
}

// recoverPanic turns a panic in a handler into a 500 rather than taking the
// process down. One bad request must not stop the server serving everyone else.
func recoverPanic(log *slog.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) (err error) {
			defer func() {
				r := recover()
				if r == nil {
					return
				}
				if r == http.ErrAbortHandler {
					// The handler deliberately aborted; let it propagate.
					panic(r)
				}
				log.LogAttrs(c.Request().Context(), slog.LevelError, "handler panic",
					slog.Any("panic", r),
					slog.String("route", c.RouteInfo().Path),
					slog.String("stack", string(debug.Stack())),
				)
				err = echo.NewHTTPError(http.StatusInternalServerError, "internal error")
			}()
			return next(c)
		}
	}
}
