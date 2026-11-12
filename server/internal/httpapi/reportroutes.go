package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"uuid"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/moderation"
)

type reportRequest struct {
	// Room and Event say what is being reported. Both may be absent when the
	// report is about a person rather than a message.
	Room    string `json:"room,omitempty"`
	EventID string `json:"eventId,omitempty"`
	// Subject is who is being reported, as an address.
	Subject string `json:"subject"`
	Reason  string `json:"reason"`
	Detail  string `json:"detail,omitempty"`
}

type reportResponse struct {
	ID      string `json:"id"`
	RoomID  string `json:"roomId,omitempty"`
	EventID string `json:"eventId,omitempty"`
	Subject string `json:"subject"`
	// Reporter is shown only to moderators; the listing endpoint is the only
	// place this is returned.
	Reporter     string `json:"reporter,omitempty"`
	Reason       string `json:"reason"`
	Detail       string `json:"detail,omitempty"`
	ReportedBody string `json:"reportedBody,omitempty"`
	Status       string `json:"status"`
	Resolution   string `json:"resolution,omitempty"`
	CreatedAt    int64  `json:"createdAt"`
}

/*
handleReport files a report of abuse.

Any signed-in person may file one, deliberately: requiring membership of the
room would mean somebody who has already been removed from it, or who saw the
message before leaving, could not report what they saw.

The reported text is copied into the report here rather than referenced,
because moderation usually happens after the author has deleted it.
*/
func (s *Server) handleReport(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	var request reportRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	filing := moderation.Filing{
		EventID: request.EventID,
		Subject: request.Subject,
		Reason:  moderation.Reason(request.Reason),
		Detail:  request.Detail,
	}

	// When a message is named, its text and its author are taken from the
	// message rather than from what the reporter typed: a report has to
	// describe what was actually said.
	if request.Room != "" {
		roomID, err := uuid.Parse(request.Room)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "that is not a room")
		}

		// Readable by the reporter, so this cannot be used to fish for the
		// contents of a conversation by reporting identifiers from it.
		if _, err := s.rooms.Visible(ctx, roomID, user.ID); err != nil {
			return roomError(err)
		}
		filing.RoomID = &roomID

		if request.EventID != "" {
			event, err := s.rooms.EventInRoom(ctx, roomID, request.EventID)
			if err != nil {
				return roomError(err)
			}
			filing.ReportedBody = moderation.BodyOf(event.Content)
			filing.Subject = event.SenderAddress
		}
	}

	if filing.Subject == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "who is being reported")
	}

	report, err := s.moderation.File(ctx, user.ID, s.rooms.UserAddress(user.Username), filing)
	if errors.Is(err, moderation.ErrUnknownReason) {
		return echo.NewHTTPError(http.StatusBadRequest, "that is not a reason a report can be filed under")
	}
	if errors.Is(err, moderation.ErrTooManyReports) {
		return echo.NewHTTPError(http.StatusTooManyRequests,
			"too many reports filed recently; try again later")
	}
	if err != nil {
		return err
	}

	// The filed report comes back without the reporter's own address: there is
	// nothing to tell them that they do not already know.
	return c.JSON(http.StatusCreated, reportResponse{
		ID:        report.ID.String(),
		EventID:   report.EventID,
		Subject:   report.SubjectAddress,
		Reason:    string(report.Reason),
		Status:    report.Status,
		CreatedAt: report.CreatedAt,
	})
}

// handleListReports is the moderation queue.
func (s *Server) handleListReports(c *echo.Context) error {
	limit := 0
	if raw := c.QueryParam("limit"); raw != "" {
		limit, _ = strconv.Atoi(raw)
	}

	reports, err := s.moderation.Open(c.Request().Context(), int32(limit))
	if err != nil {
		return err
	}

	response := make([]reportResponse, 0, len(reports))
	for _, report := range reports {
		item := reportResponse{
			ID:           report.ID.String(),
			EventID:      report.EventID,
			Subject:      report.SubjectAddress,
			Reporter:     report.Reporter,
			Reason:       string(report.Reason),
			Detail:       report.Detail,
			ReportedBody: report.ReportedBody,
			Status:       report.Status,
			CreatedAt:    report.CreatedAt,
		}
		if report.RoomID != nil {
			item.RoomID = report.RoomID.String()
		}
		response = append(response, item)
	}

	return c.JSON(http.StatusOK, response)
}

// handleReportCount is the badge on a moderator's queue.
func (s *Server) handleReportCount(c *echo.Context) error {
	count, err := s.moderation.Count(c.Request().Context())
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]int64{"open": count})
}

type resolveReportRequest struct {
	// Dismissed marks a report as considered and not acted on, which is a
	// different outcome from acted on and worth recording as such.
	Dismissed  bool   `json:"dismissed"`
	Resolution string `json:"resolution,omitempty"`
}

func (s *Server) handleResolveReport(c *echo.Context) error {
	user, _ := currentUser(c)

	reportID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such report")
	}

	var request resolveReportRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	report, err := s.moderation.Resolve(
		c.Request().Context(), reportID, user.ID, request.Dismissed, request.Resolution)
	if errors.Is(err, moderation.ErrReportNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, "no such open report")
	}
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, reportResponse{
		ID:         report.ID.String(),
		Subject:    report.SubjectAddress,
		Reason:     string(report.Reason),
		Status:     report.Status,
		Resolution: report.Resolution,
		CreatedAt:  report.CreatedAt,
	})
}

// handleReportReasons lists what a report may be filed under, so a client does
// not hard-code a list that can drift from what the server accepts.
func (s *Server) handleReportReasons(c *echo.Context) error {
	reasons := moderation.Reasons()

	response := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		response = append(response, string(reason))
	}

	return c.JSON(http.StatusOK, response)
}
