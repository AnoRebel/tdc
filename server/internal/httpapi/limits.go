package httpapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/settings"
)

/*
Capacity limits: how much an operator is willing to host.

Checked before anything is created, never part way through. A limit that
refuses after the account exists but before its first channel leaves somebody
half-registered, which is worse than either outcome — so each of these runs
first and refuses cleanly.

Zero means no limit, and zero is the default. A server that quietly enforced a
number nobody chose would be a server that stops working for reasons its
operator cannot see.
*/

// withinUserLimit refuses when the server holds as many accounts as it will.
func (s *Server) withinUserLimit(ctx context.Context) error {
	limit := s.limitFor(ctx, settings.MaxUsers)
	if limit == 0 {
		return nil
	}

	count, err := s.queries.CountUsers(ctx)
	if err != nil {
		// A limit that cannot be read is not enforced. Refusing every
		// registration because a count failed would turn a transient database
		// problem into an outage of the thing people notice most.
		s.log.Warn("cannot count accounts to enforce a limit", "error", err)
		return nil
	}

	if count >= limit {
		return echo.NewHTTPError(http.StatusForbidden,
			"this server has reached the number of accounts its operator allows")
	}
	return nil
}

// withinChannelLimit refuses when the server holds as many channels as it will.
func (s *Server) withinChannelLimit(ctx context.Context) error {
	limit := s.limitFor(ctx, settings.MaxChannels)
	if limit == 0 {
		return nil
	}

	count, err := s.queries.CountRooms(ctx)
	if err != nil {
		s.log.Warn("cannot count channels to enforce a limit", "error", err)
		return nil
	}

	if count >= limit {
		return echo.NewHTTPError(http.StatusForbidden,
			"this server has reached the number of channels its operator allows")
	}
	return nil
}

// withinStorageLimit refuses an upload that would exceed the operator's total.
//
// Checked against the size being added rather than after the fact, so the
// limit is a limit rather than a threshold that is always slightly exceeded.
func (s *Server) withinStorageLimit(ctx context.Context, adding int64) error {
	limit := s.limitFor(ctx, settings.MaxStorageBytes)
	if limit == 0 {
		return nil
	}

	used, err := s.queries.TotalAttachmentBytes(ctx)
	if err != nil {
		s.log.Warn("cannot total attachment storage to enforce a limit", "error", err)
		return nil
	}

	if used+adding > limit {
		return echo.NewHTTPError(http.StatusInsufficientStorage,
			fmt.Sprintf("this server allows %d bytes of attachments and is using %d",
				limit, used))
	}
	return nil
}

// limitFor reads a limit, treating an unwired settings service as no limit.
func (s *Server) limitFor(ctx context.Context, key settings.Key) int64 {
	if s.settings == nil {
		return 0
	}
	return s.settings.Int(ctx, key)
}

// usageResponse is what an operator sees about their own server.
type usageResponse struct {
	Users    usageFigure `json:"users"`
	Channels usageFigure `json:"channels"`
	Storage  usageFigure `json:"storage"`
}

// usageFigure is one measure against its limit.
type usageFigure struct {
	Used int64 `json:"used"`
	// Limit is zero when there is none, which a client shows as "no limit"
	// rather than as a limit of zero.
	Limit int64 `json:"limit"`
}

/*
handleUsage reports what the server holds against what it allows.

The other half of a limit: a number that refuses things is only usable if an
operator can see how close they are before it starts refusing.
*/
func (s *Server) handleUsage(c *echo.Context) error {
	ctx := c.Request().Context()

	users, err := s.queries.CountUsers(ctx)
	if err != nil {
		return err
	}
	channels, err := s.queries.CountRooms(ctx)
	if err != nil {
		return err
	}
	storage, err := s.queries.TotalAttachmentBytes(ctx)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, usageResponse{
		Users:    usageFigure{Used: users, Limit: s.limitFor(ctx, settings.MaxUsers)},
		Channels: usageFigure{Used: channels, Limit: s.limitFor(ctx, settings.MaxChannels)},
		Storage:  usageFigure{Used: storage, Limit: s.limitFor(ctx, settings.MaxStorageBytes)},
	})
}
