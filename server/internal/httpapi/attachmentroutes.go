package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/attachments"
)

// presignedLifetime is how long a download link stays valid.
//
// Short, because the link carries its own authorisation: anyone holding it can
// fetch the file until it expires, whatever happens to the room's membership in
// the meantime. Long enough for a browser to follow a redirect and for a slow
// connection to start the transfer.
const presignedLifetime = 5 * time.Minute

// attachmentResponse is the public shape of an attachment.
type attachmentResponse struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	// ContentType is what the server detected, not what the uploader declared.
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
	// URL is where to fetch it. Always this server: a presigned redirect is
	// issued at request time rather than handed out here, so a link cannot
	// outlive the permission that granted it.
	URL string `json:"url"`
	// Renderable says whether a client can expect to display this inline,
	// which saves it guessing from the type.
	Renderable bool `json:"renderable"`
	// PreviewURL is a reduced version, where one exists. Empty otherwise, and
	// a client then shows the name, size and type instead of a broken image.
	PreviewURL string `json:"previewUrl,omitempty"`
	// Width and Height are the original's dimensions where known, so a client
	// can reserve the right space and the conversation does not jump as
	// attachments arrive.
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	UploadedBy string `json:"uploadedBy"`
	UploadedAt string `json:"uploadedAt"`
}

func attachmentResponseOf(attachment *attachments.Attachment) attachmentResponse {
	previewURL := ""
	if attachment.HasPreview() {
		previewURL = "/api/attachments/" + attachment.ID.String() + "/preview"
	}

	return attachmentResponse{
		ID:          attachment.ID.String(),
		Filename:    attachment.Filename,
		ContentType: attachment.ContentType,
		Size:        attachment.Size,
		URL:         "/api/attachments/" + attachment.ID.String(),
		Renderable:  attachments.IsRenderable(attachment.ContentType),
		PreviewURL:  previewURL,
		Width:       attachment.Width,
		Height:      attachment.Height,
		UploadedBy:  attachment.UploaderAddress,
		UploadedAt:  attachment.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// limitsResponse tells a client what it may upload.
//
// Sent so a client can refuse a file before spending someone's bandwidth on it.
// The server checks again regardless: this is a courtesy, not a control.
type limitsResponse struct {
	MaxFileSize    int64    `json:"maxFileSize"`
	MaxMessageSize int64    `json:"maxMessageSize"`
	MaxPerMessage  int      `json:"maxPerMessage"`
	AllowedTypes   []string `json:"allowedTypes"`
	BlockedTypes   []string `json:"blockedTypes"`
}

func (s *Server) handleAttachmentLimits(c *echo.Context) error {
	limits := s.attachments.Limits()

	return c.JSON(http.StatusOK, limitsResponse{
		MaxFileSize:    limits.MaxFileSize,
		MaxMessageSize: limits.MaxMessageSize,
		MaxPerMessage:  limits.MaxPerMessage,
		AllowedTypes:   limits.AllowedTypes,
		BlockedTypes:   limits.BlockedTypes,
	})
}

// handleUploadAttachment accepts a file for a room.
//
// Uploaded before the message that carries it, so that a refusal costs the
// sender nothing but the upload, and a failed send does not lose the file.
func (s *Server) handleUploadAttachment(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	// Membership is checked before a byte is read: someone who cannot post to a
	// room must not be able to use it as storage.
	if _, err := s.rooms.RequireMembership(ctx, roomID, user.ID); err != nil {
		return roomError(err)
	}

	file, err := c.FormFile("file")
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request carried no file")
	}

	body, err := file.Open()
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the uploaded file could not be read")
	}
	defer body.Close()

	attachment, err := s.attachments.Upload(
		ctx, roomID, user.ID, s.rooms.UserAddress(user.Username), file.Filename, body,
	)
	if err != nil {
		return attachmentError(err)
	}

	return c.JSON(http.StatusCreated, attachmentResponseOf(attachment))
}

// handleDownloadAttachment serves an attachment to someone allowed to read it.
func (s *Server) handleDownloadAttachment(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such attachment")
	}

	attachment, err := s.attachments.Get(ctx, id)
	if err != nil {
		return attachmentError(err)
	}

	// The room's permissions govern the file. Knowing an identifier is not
	// authorisation, so this is checked on every request rather than trusted
	// from whoever handed the link on.
	if _, err := s.rooms.RequireMembership(ctx, attachment.RoomID, user.ID); err != nil {
		return roomError(err)
	}

	// Where the object store can serve the bytes itself, hand the transfer over
	// rather than streaming a large file through this process. The redirect is
	// issued now, after the permission check, and expires quickly.
	if url, err := s.attachments.PresignedURL(ctx, attachment, presignedLifetime); err == nil && url != "" {
		return c.Redirect(http.StatusFound, url)
	}

	reader, err := s.attachments.Open(ctx, attachment)
	if err != nil {
		return attachmentError(err)
	}
	defer reader.Close()

	setInertHeaders(c, attachment)

	return c.Stream(http.StatusOK, attachments.ServingType(attachment.ContentType), reader)
}

// setInertHeaders makes a browser store an attachment rather than act on it.
//
// An attachment is content someone else uploaded, served from the application's
// own origin. Without these, an HTML or SVG file would run script with the
// application's cookies available to it. Each header covers a different way
// that happens, so they are set together:
//
//   - nosniff stops the browser from second-guessing the declared type;
//   - the disposition makes it a download rather than a page;
//   - the sandbox policy strips script, plugins and same-origin privileges from
//     anything that is rendered anyway;
//   - frame-ancestors keeps it from being embedded back into the application.
func setInertHeaders(c *echo.Context, attachment *attachments.Attachment) {
	header := c.Response().Header()

	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Disposition", downloadDisposition(attachment.Filename))
	header.Set("Content-Security-Policy", "default-src 'none'; sandbox; frame-ancestors 'none'")
	header.Set("X-Frame-Options", "DENY")
	// Attachments are private to a room, so no shared cache may keep one.
	header.Set("Cache-Control", "private, max-age=0, no-store")

	if attachment.Size > 0 {
		header.Set("Content-Length", strconv.FormatInt(attachment.Size, 10))
	}
}

// contentDisposition names the download, with the filename escaped.
//
// Both spellings are sent: the quoted form for older clients, and the RFC 5987
// form so a name outside ASCII survives. A quote or a backslash in the name
// would end the header value early, so they are removed rather than escaped.
func downloadDisposition(filename string) string {
	if filename == "" {
		filename = "file"
	}

	ascii := make([]rune, 0, len(filename))
	for _, r := range filename {
		switch {
		case r < 0x20, r == 0x7f, r == '"', r == '\\':
			// Dropped: these end the header value or confuse a parser.
		case r > 0x7e:
			ascii = append(ascii, '_')
		default:
			ascii = append(ascii, r)
		}
	}

	fallback := string(ascii)
	if fallback == "" {
		fallback = "file"
	}

	return fmt.Sprintf("attachment; filename=%q; filename*=UTF-8''%s",
		fallback, urlEncode(filename))
}

// urlEncode percent-encodes a filename for the RFC 5987 header form.
func urlEncode(s string) string {
	const hex = "0123456789ABCDEF"

	encoded := make([]byte, 0, len(s))
	for _, b := range []byte(s) {
		// Unreserved characters per RFC 5987, which need no encoding.
		if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') ||
			b == '-' || b == '.' || b == '_' || b == '~' {
			encoded = append(encoded, b)
			continue
		}
		encoded = append(encoded, '%', hex[b>>4], hex[b&0x0f])
	}

	return string(encoded)
}

// attachmentError turns a service error into the right HTTP response.
//
// A limit refusal says which limit was exceeded and by how much, because a
// bare "too large" leaves someone guessing whether to compress the file or
// send fewer of them.
func attachmentError(err error) error {
	var limitErr *attachments.LimitError
	if errors.As(err, &limitErr) {
		status := http.StatusRequestEntityTooLarge
		if limitErr.Detail != "" {
			// A refused type is not a size problem.
			status = http.StatusUnsupportedMediaType
		}
		return echo.NewHTTPError(status, limitErr.Error())
	}

	if errors.Is(err, attachments.ErrNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, "no such attachment")
	}

	return err
}

// handleAttachmentPreview serves the reduced version of an attachment.
//
// Separate from the attachment itself so that a conversation full of images
// costs a few kilobytes each rather than the originals, which is the difference
// between usable and unusable on metered mobile data.
func (s *Server) handleAttachmentPreview(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such attachment")
	}

	attachment, err := s.attachments.Get(ctx, id)
	if err != nil {
		return attachmentError(err)
	}

	// The same authorisation as the original: a preview is a smaller copy of
	// the same private content.
	if _, err := s.rooms.RequireMembership(ctx, attachment.RoomID, user.ID); err != nil {
		return roomError(err)
	}

	reader, err := s.attachments.OpenPreview(ctx, attachment)
	if err != nil {
		return attachmentError(err)
	}
	defer reader.Close()

	header := c.Response().Header()
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Security-Policy", "default-src 'none'; sandbox; frame-ancestors 'none'")
	header.Set("X-Frame-Options", "DENY")
	// A preview is displayed inline — that is its purpose — so it carries no
	// attachment disposition. That is safe because a preview is always an image
	// this server encoded itself, never the bytes somebody uploaded.
	header.Set("Cache-Control", "private, max-age=300")

	return c.Stream(http.StatusOK, attachment.PreviewType, reader)
}
