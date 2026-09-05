package httpapi

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/settings"
)

// settingResponse is one setting, with what it means.
type settingResponse struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	// Description is what an operator deciding whether to change it needs.
	Description string `json:"description"`
	// Fallback is what applies when nothing is set, so the difference between
	// "set to open" and "not set, therefore open" is visible.
	Fallback string `json:"fallback"`
}

// handleListSettings returns every setting and its current value.
//
// Secrets are redacted by the settings service: this is what an administrator
// sees on a screen, and a settings page displaying a credential is a
// credential in a screenshot.
func (s *Server) handleListSettings(c *echo.Context) error {
	ctx := c.Request().Context()
	values := s.settings.All(ctx)

	response := make([]settingResponse, 0, len(values))
	for _, key := range settings.Keys() {
		description, fallback, _ := settings.Describe(key)
		response = append(response, settingResponse{
			Key:         string(key),
			Value:       values[key],
			Description: description,
			Fallback:    fallback,
		})
	}

	return c.JSON(http.StatusOK, response)
}

type setSettingRequest struct {
	Value string `json:"value"`
}

func (s *Server) handleSetSetting(c *echo.Context) error {
	user, _ := currentUser(c)

	var request setSettingRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	key := settings.Key(c.Param("key"))

	err := s.settings.Set(c.Request().Context(), key, request.Value,
		s.rooms.UserAddress(user.Username))

	switch {
	case errors.Is(err, settings.ErrUnknownSetting):
		// Named rather than silently accepted: a typo that created a new
		// setting would look like a change that did nothing.
		return echo.NewHTTPError(http.StatusNotFound, "this server has no such setting")
	case errors.Is(err, settings.ErrInvalidValue):
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	case err != nil:
		return err
	}

	/*
		Recorded, because changing who may register or how much a server holds
		is exactly the sort of change somebody later needs to account for.

		The value is included: "somebody changed the registration policy" is
		not an answer to "why did registration stop working on Tuesday".
	*/
	if err := s.audit.Record(c.Request().Context(), audit.Entry{
		ActorID:    &user.ID,
		ActorLabel: user.Username,
		Action:     audit.ActionSettingChanged,
		TargetKind: "setting",
		TargetID:   string(key),
		SourceIP:   requestIP(c),
		UserAgent:  c.Request().UserAgent(),
		Detail:     map[string]any{"value": request.Value},
	}); err != nil {
		s.log.Error("cannot record a setting change", "key", key, "error", err)
	}

	return c.NoContent(http.StatusNoContent)
}
