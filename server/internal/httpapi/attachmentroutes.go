package httpapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/attachments"
)

// presignedLifetime is how long a redirect to the object store stays valid.
//
// Deliberately very short. A presigned URL is a bearer token: anyone holding it
// can fetch the file, whatever happens to the room's membership in the
// meantime, and the object store has no idea who the person is. This is long
// enough for the browser that was just authorised to follow the redirect it was
// given, and too short to be worth passing on.
//
// The requirement is that a download link be bound to the authorised person.
// An object store's presigned URL cannot be — so the redirect is issued per
// request, after the check, rather than handed out as a shareable link.
const presignedLifetime = 30 * time.Second

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
	// Kind says how to display it: "image", "video", "audio", "pdf", "text",
	// "markdown", or "" for anything only downloadable. Decided here so every
	// client makes the same choice, and so markdown — which has no magic bytes
	// and is detected as plain text — is recognised once rather than by each
	// client guessing from a filename.
	Kind string `json:"kind,omitempty"`
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
		Kind:        attachmentKind(attachment),
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
	// rather than streaming a large file through this process.
	//
	// Only for a request that is not a range request. A media element seeking
	// through a video would be redirected on every range, each hop costing a
	// signature and a round trip, and Range headers do not always survive a
	// redirect — so those are streamed here, where the range is handled
	// directly.
	if c.Request().Header.Get("Range") == "" {
		if url, err := s.attachments.PresignedURL(ctx, attachment, presignedLifetime); err == nil && url != "" {
			// Never cached: the URL carries its own authorisation and expires
			// in seconds, so a cache holding it would serve a stale token to
			// whoever asked next.
			c.Response().Header().Set("Cache-Control", "private, no-store")
			return c.Redirect(http.StatusFound, url)
		}
	}

	reader, err := s.attachments.Open(ctx, attachment)
	if err != nil {
		return attachmentError(err)
	}
	defer reader.Close()

	setInertHeaders(c, attachment)

	// A media element asks for byte ranges: it fetches the header to learn the
	// duration, then seeks. Answering every range request with the whole file
	// leaves it unable to seek, and some browsers will not even report a
	// duration without range support. ServeContent handles this properly when
	// the reader can seek, which the filesystem store's file can.
	if seeker, ok := reader.(io.ReadSeeker); ok {
		http.ServeContent(
			c.Response(), c.Request(),
			attachment.Filename, attachment.CreatedAt, seeker,
		)
		return nil
	}

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
	header.Set("X-Frame-Options", "DENY")
	// Attachments are private to a room, so no shared cache may keep one.
	header.Set("Cache-Control", "private, max-age=0, no-store")

	// The type the browser is told about, which for active content is
	// deliberately not the type it really is.
	header.Set("Content-Type", attachments.ServingType(attachment.ContentType))

	// Media plays rather than downloads. "inline" is safe here only because the
	// type above is inert for anything a browser would act on: an SVG or an
	// HTML file is already being served as application/octet-stream by the time
	// it reaches this line, and that downloads whatever the disposition says.
	if playsInline(attachment.ContentType) {
		header.Set("Content-Disposition", inlineDisposition(attachment.Filename))
		// Says seeking is possible, which a media element checks before it will
		// offer a seek bar.
		header.Set("Accept-Ranges", "bytes")

		// Deliberately without "sandbox". A sandbox directive on a response
		// puts the resource in an opaque origin, and a media or image element
		// then refuses to load it at all — the request simply hangs, with no
		// error to explain why. The remaining directives still stop this being
		// treated as a document; what makes it safe to relax is that only
		// audio, video and raster images reach this branch, none of which can
		// execute. Everything else keeps the stricter policy below.
		header.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		return
	}

	// Anything that is not plainly media is treated as hostile: sandboxed into
	// an opaque origin, unable to load anything, and unable to be framed.
	header.Set("Content-Security-Policy", "default-src 'none'; sandbox; frame-ancestors 'none'")

	header.Set("Content-Disposition", downloadDisposition(attachment.Filename))

	if attachment.Size > 0 {
		header.Set("Content-Length", strconv.FormatInt(attachment.Size, 10))
	}
}

// playsInline reports whether a browser should render this where it stands
// rather than save it.
//
// Only audio, video and images: types a media or image element handles, none of
// which execute. Everything else keeps the attachment disposition.
func playsInline(contentType string) bool {
	if contentType == "image/svg+xml" {
		// An image by type, a document by capability. Downloaded, never shown.
		return false
	}

	return strings.HasPrefix(contentType, "audio/") ||
		strings.HasPrefix(contentType, "video/") ||
		strings.HasPrefix(contentType, "image/")
}

// inlineDisposition names a file that is displayed rather than saved.
//
// The name still matters: it is what a browser suggests should the person
// choose to save it after all.
func inlineDisposition(filename string) string {
	return "inline" + strings.TrimPrefix(downloadDisposition(filename), "attachment")
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

// parseAttachmentIDs turns the identifiers a client sent into UUIDs.
//
// A malformed identifier is refused rather than skipped: silently dropping one
// would post a message missing a file the sender believed they had attached.
func parseAttachmentIDs(raw []string) ([]uuid.UUID, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	ids := make([]uuid.UUID, 0, len(raw))
	for _, value := range raw {
		id, err := uuid.Parse(value)
		if err != nil {
			return nil, echo.NewHTTPError(http.StatusBadRequest,
				"an attachment identifier could not be read")
		}
		ids = append(ids, id)
	}

	return ids, nil
}

// attachmentKind says how a client should display an attachment.
//
// Decided on the server so every client — web, desktop, and anything a third
// party writes — makes the same choice, and so a change of policy does not need
// each of them updated.
func attachmentKind(attachment *attachments.Attachment) string {
	contentType := attachment.ContentType

	switch {
	case contentType == "image/svg+xml":
		// Served as an opaque download, so not displayable at all.
		return ""
	case strings.HasPrefix(contentType, "image/"):
		return "image"
	case strings.HasPrefix(contentType, "video/"):
		return "video"
	case strings.HasPrefix(contentType, "audio/"):
		return "audio"
	case contentType == "application/pdf":
		return "pdf"
	case attachments.IsTextual(contentType):
		if attachments.IsMarkdown(contentType, attachment.Filename) {
			return "markdown"
		}
		return "text"
	default:
		return ""
	}
}
