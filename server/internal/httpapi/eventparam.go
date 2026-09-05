package httpapi

import (
	"net/url"

	"github.com/labstack/echo/v5"
)

/*
eventIDParam reads an event identifier from the path, decoded.

Event identifiers begin with "$", which a correct client percent-encodes as
"%24". Echo hands back the raw path segment rather than the decoded one, so
reading the parameter directly yields "%24abc" and every lookup misses.

This stayed invisible while clients sent the "$" unencoded — a browser will
happily leave it alone in a path — and appeared the moment one encoded it
properly. Decoding here means both spellings reach the same message.

A segment that is not valid percent-encoding is returned unchanged rather than
refused: it will simply not match an event, which is the same answer as any
other identifier that does not exist.
*/
func eventIDParam(c *echo.Context) string {
	raw := c.Param("eventId")

	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return raw
	}
	return decoded
}
