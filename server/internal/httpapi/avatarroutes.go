package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"uuid"

	"github.com/AnoRebel/tdc/server/internal/attachments"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/labstack/echo/v5"
)

/*
Pictures people choose for themselves.

A picture is sent already cropped by the interface, checked and written again
here (attachments.PutAvatar), and served at an address that changes with every
new picture. That address needs no session: the desktop client draws pictures
with plain image elements, which cannot carry one, and an address nobody can
guess and that never changes what it shows can be cached for good by anybody
who has it. Who has a picture is listed only to people signed in.
*/

// avatarURL is where a stored picture is served, from its storage key.
func avatarURL(userID uuid.UUID, key string) string {
	if key == "" {
		return ""
	}
	return "/api/avatars/" + userID.String() + "/" + key[strings.LastIndex(key, "/")+1:]
}

// handleSetAvatar stores a new picture for the person signed in.
func (s *Server) handleSetAvatar(c *echo.Context) error {
	if s.attachments == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "this server cannot store pictures")
	}
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	/*
		The picture's bytes as the body, or as base64 in {"image": …}: the
		desktop client sends through Go, whose request carries JSON, so both
		clients can use the API client they already have.
	*/
	var body io.Reader
	if strings.HasPrefix(c.Request().Header.Get("Content-Type"), "application/json") {
		var request struct {
			Image string `json:"image"`
		}
		limited := http.MaxBytesReader(c.Response(), c.Request().Body, attachments.MaxAvatarBytes*4/3+1024)
		if err := json.NewDecoder(limited).Decode(&request); err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				return echo.NewHTTPError(http.StatusRequestEntityTooLarge,
					"the picture is larger than 2 MB; crop or shrink it and try again")
			}
			return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
		}
		decoded, err := base64.StdEncoding.DecodeString(request.Image)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "the picture is not base64")
		}
		body = bytes.NewReader(decoded)
	} else {
		body = http.MaxBytesReader(c.Response(), c.Request().Body, attachments.MaxAvatarBytes+1)
	}
	stored, err := s.attachments.PutAvatar(ctx, user.ID, body)
	var tooBig *http.MaxBytesError
	switch {
	case errors.Is(err, attachments.ErrAvatarTooLarge), errors.As(err, &tooBig):
		return echo.NewHTTPError(http.StatusRequestEntityTooLarge,
			"the picture is larger than 2 MB; crop or shrink it and try again")
	case errors.Is(err, attachments.ErrNotAnImage):
		return echo.NewHTTPError(http.StatusUnsupportedMediaType,
			"the picture must be a PNG, JPEG, GIF or WebP image")
	case err != nil:
		return err
	}

	// Recording the new key is what queues the old picture for deletion.
	if err := s.queries.SetUserAvatar(ctx, dbgen.SetUserAvatarParams{
		ID: user.ID, AvatarKey: stored.Key, AvatarType: stored.ContentType,
	}); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]string{"url": avatarURL(user.ID, stored.Key)})
}

// handleRemoveAvatar goes back to initials.
func (s *Server) handleRemoveAvatar(c *echo.Context) error {
	user, _ := currentUser(c)
	if err := s.queries.SetUserAvatar(c.Request().Context(), dbgen.SetUserAvatarParams{ID: user.ID}); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// handleListAvatars says who on this server has a picture, and where it is.
func (s *Server) handleListAvatars(c *echo.Context) error {
	rows, err := s.queries.UsersWithAvatars(c.Request().Context())
	if err != nil {
		return err
	}
	type avatar struct {
		Address string `json:"address"`
		URL     string `json:"url"`
	}
	avatars := make([]avatar, 0, len(rows))
	for _, row := range rows {
		avatars = append(avatars, avatar{
			Address: s.rooms.UserAddress(row.Username),
			URL:     avatarURL(row.ID, row.AvatarKey),
		})
	}
	return c.JSON(http.StatusOK, map[string]any{"avatars": avatars})
}

// handleAvatar serves a picture by the address handleListAvatars gave.
func (s *Server) handleAvatar(c *echo.Context) error {
	if s.attachments == nil {
		return echo.ErrNotFound
	}
	userID, err := uuid.Parse(c.Param("user"))
	if err != nil {
		return echo.ErrNotFound
	}
	ctx := c.Request().Context()
	row, err := s.queries.UserAvatar(ctx, userID)
	// An address for a picture since replaced is gone, not redirected: the
	// new one has a new address, which the list gives.
	if err != nil || row.AvatarKey == "" || !strings.HasSuffix(row.AvatarKey, "/"+c.Param("token")) {
		return echo.ErrNotFound
	}

	body, err := s.attachments.OpenAvatar(ctx, row.AvatarKey)
	if err != nil {
		return echo.ErrNotFound
	}
	defer body.Close()

	header := c.Response().Header()
	header.Set("Cache-Control", "public, max-age=31536000, immutable")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Type", row.AvatarType)
	c.Response().WriteHeader(http.StatusOK)
	_, err = io.Copy(c.Response(), body)
	return err
}
