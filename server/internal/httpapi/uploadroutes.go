package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/attachments"
	"github.com/AnoRebel/tdc/server/internal/settings"
)

/*
Files sent in pieces, and the limits on all files.

The single-request upload stays for integrations and for small files; both
clients send every file this way, because it is the one that survives a
dropped connection and fits behind a proxy with a small body limit.
*/
func init() {
	registerRoutes(func(s *Server) {
		if s.attachments == nil || s.rooms == nil {
			return
		}
		// The operator's settings decide the limits, read per request so a
		// change applies to the next upload.
		if s.settings != nil {
			s.attachments.WithLimits(s.fileLimits)
		}

		gate := s.requireFeature(settings.FeatureAttachments)
		s.echo.POST("/api/rooms/:id/uploads", s.handleBeginFileUpload, requireAuthentication, gate)
		uploads := s.echo.Group("/api/uploads/:id", requireAuthentication, gate)
		uploads.GET("", s.handleUploadStatus)
		uploads.PUT("/pieces/:piece", s.handleUploadFilePiece)
		uploads.POST("/finish", s.handleFinishFileUpload)
		uploads.DELETE("", s.handleAbandonUpload)
	})

	documentRoutes(map[string]entry{
		"POST /api/rooms/:id/uploads":        {Summary: "Begin sending a file in pieces", Authentication: "session or token"},
		"GET /api/uploads/:id":               {Summary: "How much of an upload the server holds", Authentication: "session or token"},
		"PUT /api/uploads/:id/pieces/:piece": {Summary: "Send the next piece of an upload", Authentication: "session or token"},
		"POST /api/uploads/:id/finish":       {Summary: "Finish an upload, making it an attachment", Authentication: "session or token"},
		"DELETE /api/uploads/:id":            {Summary: "Abandon an upload", Authentication: "session or token"},
	})
}

// fileLimits are the limits the operator's settings describe now.
func (s *Server) fileLimits(ctx context.Context) attachments.Limits {
	limits := attachments.DefaultLimits()
	if size := s.settings.Int(ctx, settings.FilesMaxFileBytes); size > 0 {
		limits.MaxFileSize = size
	}
	if size := s.settings.Int(ctx, settings.FilesMaxMessageBytes); size > 0 {
		limits.MaxMessageSize = size
	}
	return limits
}

// pieceBytes is the piece size clients are told to use, kept within bounds
// whatever is stored.
func (s *Server) pieceBytes(ctx context.Context) int {
	size := int64(4 << 20)
	if s.settings != nil {
		size = s.settings.Int(ctx, settings.FilesUploadPieceBytes)
	}
	return int(max(settings.MinUploadPieceBytes, min(size, settings.MaxUploadPieceBytes)))
}

/*
withinPersonQuota refuses a file that would take somebody past what their files
may occupy. Counted per stored file, as the server-wide limit is, plus what
their uploads in progress have reserved.
*/
func (s *Server) withinPersonQuota(ctx context.Context, userID uuid.UUID, adding int64) error {
	quota := s.limitFor(ctx, settings.FilesQuotaPerPersonBytes)
	if quota == 0 {
		return nil
	}
	used, err := s.usedBy(ctx, userID)
	if err != nil {
		s.log.Warn("cannot total somebody's files to enforce a quota", "error", err)
		return nil
	}
	if used+adding > quota {
		return echo.NewHTTPError(http.StatusInsufficientStorage,
			fmt.Sprintf("your files may occupy %d bytes and you are using %d", quota, used))
	}
	return nil
}

// usedBy is what somebody's files occupy, with their uploads in progress.
func (s *Server) usedBy(ctx context.Context, userID uuid.UUID) (int64, error) {
	stored, err := s.queries.AttachmentBytesBy(ctx, &userID)
	if err != nil {
		return 0, err
	}
	pending, err := s.queries.PendingUploadBytesBy(ctx, userID)
	if err != nil {
		return 0, err
	}
	return stored + pending, nil
}

type beginUploadRequest struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
}

type uploadResponse struct {
	ID       string `json:"id"`
	RoomID   string `json:"roomId"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	// Received and Pieces are what the server holds, which is where a client
	// resumes from.
	Received   int64  `json:"received"`
	Pieces     int    `json:"pieces"`
	PieceBytes int    `json:"pieceBytes"`
	UpdatedAt  string `json:"updatedAt"`
}

func uploadResponseOf(upload *attachments.Upload) uploadResponse {
	return uploadResponse{
		ID: upload.ID.String(), RoomID: upload.RoomID.String(), Filename: upload.Filename,
		Size: upload.Size, Received: upload.Received, Pieces: upload.Pieces,
		PieceBytes: upload.PieceBytes, UpdatedAt: upload.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

/*
handleBeginFileUpload declares a file to be sent in pieces.

Every limit is checked here against the declared size — the file limit, the
person's quota and the server's — so a file that cannot be accepted is refused
before anybody's bandwidth is spent on it, and many uploads at once cannot
together pass a limit each would pass alone.
*/
func (s *Server) handleBeginFileUpload(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}
	if _, err := s.rooms.RequireMembership(ctx, roomID, user.ID); err != nil {
		return roomError(err)
	}

	var request beginUploadRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	if request.Size < 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "a size cannot be negative")
	}

	if err := s.attachments.LimitsNow(ctx).CheckSize(request.Size); err != nil {
		return attachmentError(err)
	}
	if err := s.withinPersonQuota(ctx, user.ID, request.Size); err != nil {
		return err
	}
	if err := s.withinStorageLimit(ctx, request.Size); err != nil {
		return err
	}

	upload, err := s.attachments.BeginUpload(ctx, roomID, user.ID,
		request.Filename, request.Size, s.pieceBytes(ctx))
	if err != nil {
		return uploadError(err)
	}
	return c.JSON(http.StatusCreated, uploadResponseOf(upload))
}

func (s *Server) handleUploadStatus(c *echo.Context) error {
	user, _ := currentUser(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return uploadError(attachments.ErrUploadNotFound)
	}
	upload, err := s.attachments.UploadFor(c.Request().Context(), id, user.ID)
	if err != nil {
		return uploadError(err)
	}
	return c.JSON(http.StatusOK, uploadResponseOf(upload))
}

/*
handleUploadFilePiece stores the next piece.

Raw bytes or base64 in JSON, whichever the client's transport carries: the web
sends raw, which costs nothing extra, and the desktop sends JSON through Go,
which is the one way its webview reaches the server with the session.
*/
func (s *Server) handleUploadFilePiece(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return uploadError(attachments.ErrUploadNotFound)
	}
	piece, err := strconv.Atoi(c.Param("piece"))
	if err != nil || piece < 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "a piece is numbered from zero")
	}

	var body io.Reader
	request := c.Request()
	limit := int64(settings.MaxUploadPieceBytes) + 1
	if strings.HasPrefix(request.Header.Get("Content-Type"), "application/json") {
		reader := http.MaxBytesReader(c.Response(), request.Body, limit*4/3+1024)
		var encoded struct {
			Data string `json:"data"`
		}
		if err := json.NewDecoder(reader).Decode(&encoded); err != nil {
			return echo.NewHTTPError(http.StatusRequestEntityTooLarge,
				fmt.Sprintf("a piece is at most %d bytes", settings.MaxUploadPieceBytes))
		}
		data, err := base64.StdEncoding.DecodeString(encoded.Data)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "a piece in JSON is sent as base64")
		}
		body = strings.NewReader(string(data))
	} else {
		body = http.MaxBytesReader(c.Response(), request.Body, limit)
	}

	upload, err := s.attachments.AddPiece(ctx, id, user.ID, piece, body)
	if err != nil {
		return uploadError(err)
	}
	return c.JSON(http.StatusOK, uploadResponseOf(upload))
}

func (s *Server) handleFinishFileUpload(c *echo.Context) error {
	user, _ := currentUser(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return uploadError(attachments.ErrUploadNotFound)
	}
	attachment, err := s.attachments.FinishUpload(c.Request().Context(), id, user.ID,
		s.rooms.UserAddress(user.Username))
	if err != nil {
		return uploadError(err)
	}
	return c.JSON(http.StatusCreated, attachmentResponseOf(attachment))
}

func (s *Server) handleAbandonUpload(c *echo.Context) error {
	user, _ := currentUser(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return uploadError(attachments.ErrUploadNotFound)
	}
	if err := s.attachments.AbandonUpload(c.Request().Context(), id, user.ID); err != nil {
		return uploadError(err)
	}
	return c.NoContent(http.StatusNoContent)
}

// uploadError says what was wrong with a piece or an upload, in a way a client
// can act on: 409 means ask where to resume, 400 means the piece itself is wrong.
func uploadError(err error) error {
	switch {
	case errors.Is(err, attachments.ErrUploadNotFound):
		return echo.NewHTTPError(http.StatusNotFound, "no such upload")
	case errors.Is(err, attachments.ErrPieceOutOfOrder), errors.Is(err, attachments.ErrUploadIncomplete):
		return echo.NewHTTPError(http.StatusConflict, err.Error())
	case errors.Is(err, attachments.ErrPieceSize):
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	case errors.Is(err, attachments.ErrTooManyUploads):
		return echo.NewHTTPError(http.StatusTooManyRequests,
			fmt.Sprintf("you have %d uploads in progress; finish or abandon some first", attachments.MaxUploadsInProgress))
	default:
		return attachmentError(err)
	}
}

// fileSweepInterval is how often abandoned uploads and unsent files are looked for.
const fileSweepInterval = 10 * time.Minute

// RunFileSweeps releases the storage of abandoned uploads and unsent files,
// until ctx ends. Every node may run it: each removal is idempotent.
func (s *Server) RunFileSweeps(ctx context.Context) {
	ticker := time.NewTicker(fileSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.SweepFiles(ctx)
		}
	}
}

/*
SweepFiles makes one pass. Exported so a test can make a pass when it chooses.

Unsent files were never reclaimed before this: the function existed and nothing
called it, so every file somebody picked and then thought better of stayed on
disk for good.
*/
func (s *Server) SweepFiles(ctx context.Context) {
	// Lapsed direct transfers too. Each client counts down its own offer, so
	// this only has to tidy up, not be prompt.
	if s.queries != nil {
		s.sweepTransfers(ctx)
	}
	if s.attachments == nil {
		return
	}
	hours := int64(24)
	if s.settings != nil {
		hours = max(1, s.settings.Int(ctx, settings.FilesAbandonedAfterHours))
	}
	age := time.Duration(hours) * time.Hour

	if removed, err := s.attachments.ReclaimAbandonedUploads(ctx, age, 200); err != nil && ctx.Err() == nil {
		s.log.Error("sweeping abandoned uploads failed", slog.String("error", err.Error()))
	} else if removed > 0 {
		s.log.Info("removed abandoned uploads", "count", removed)
	}
	if _, err := s.attachments.ReclaimQueuedPieces(ctx, 500); err != nil && ctx.Err() == nil {
		s.log.Error("sweeping the pieces of removed uploads failed", slog.String("error", err.Error()))
	}
	if removed, err := s.attachments.ReclaimOrphans(ctx, age, 200); err != nil && ctx.Err() == nil {
		s.log.Error("sweeping unsent files failed", slog.String("error", err.Error()))
	} else if removed > 0 {
		s.log.Info("removed files that were never sent", "count", removed)
	}
}
