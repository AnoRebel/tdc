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

type featureResponse struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	// Default is what this feature is when nobody has chosen, so an operator
	// can tell a deliberate change from an inherited one.
	Default bool `json:"default"`
	// Available is false when a feature is switched on but cannot work — GIF
	// search with no provider configured, say. The toggle is the operator's
	// intent; this is whether the intent can be honoured, and a settings page
	// that showed only the first would report a working feature that is not.
	Available bool `json:"available"`
}

func (s *Server) handleListFeatures(c *echo.Context) error {
	ctx := c.Request().Context()

	response := make([]featureResponse, 0, len(settings.Features()))
	for _, feature := range settings.Features() {
		description, byDefault, _ := settings.DescribeFeature(feature)

		response = append(response, featureResponse{
			Name:        string(feature),
			Description: description,
			Enabled:     s.settings.Enabled(ctx, feature),
			Default:     byDefault,
			Available:   s.featureAvailable(feature),
		})
	}

	return c.JSON(http.StatusOK, response)
}

/*
featureAvailable reports whether a feature could work if switched on.

Everything a server does by itself is always available. The two that reach
another service are available only once an operator has configured one, which
is why the toggle alone does not decide what is advertised.
*/
func (s *Server) featureAvailable(feature settings.Feature) bool {
	switch feature {
	case settings.FeatureGifSearch:
		return s.gifs != nil && s.gifs.Enabled()
	case settings.FeatureLinkPreviews:
		return s.unfurl != nil && s.unfurl.Enabled()
	default:
		return true
	}
}

type setFeatureRequest struct {
	Enabled bool `json:"enabled"`
}

func (s *Server) handleSetFeature(c *echo.Context) error {
	user, _ := currentUser(c)

	var request setFeatureRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	feature := settings.Feature(c.Param("feature"))

	err := s.settings.SetFeature(c.Request().Context(), feature, request.Enabled,
		s.rooms.UserAddress(user.Username))

	switch {
	case errors.Is(err, settings.ErrUnknownFeature):
		return echo.NewHTTPError(http.StatusNotFound, "this server has no such feature")
	case err != nil:
		return err
	}

	// Turning a capability off for everybody is a change somebody will later
	// need to account for, in both directions.
	if err := s.audit.Record(c.Request().Context(), audit.Entry{
		ActorID:    &user.ID,
		ActorLabel: user.Username,
		Action:     audit.ActionSettingChanged,
		TargetKind: "feature",
		TargetID:   string(feature),
		SourceIP:   requestIP(c),
		UserAgent:  c.Request().UserAgent(),
		Detail:     map[string]any{"enabled": request.Enabled},
	}); err != nil {
		s.log.Error("cannot record a feature change", "feature", feature, "error", err)
	}

	return c.NoContent(http.StatusNoContent)
}
