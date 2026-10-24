package attachments

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
Uploads sent in pieces.

The same shape as a backup archive's upload: numbered pieces, each stored as an
object of its own so that any node behind a load balancer can take any piece,
joined on finish into a spool file that then takes exactly the path a file sent
in one request takes. A large file therefore needs neither a proxy that accepts
a body that large nor a connection that stays up for as long as it takes, and an
interruption costs one piece rather than the whole file.
*/

var (
	// ErrUploadNotFound is an upload that does not exist, or is not the
	// caller's — reported alike, so nobody learns of somebody else's.
	ErrUploadNotFound = errors.New("no such upload")
	// ErrPieceOutOfOrder is a piece other than the next one.
	ErrPieceOutOfOrder = errors.New("that is not the next piece")
	// ErrPieceSize is a piece of the wrong size.
	ErrPieceSize = errors.New("that piece is the wrong size")
	// ErrUploadIncomplete is finishing an upload before all of it arrived.
	ErrUploadIncomplete = errors.New("the upload is not complete")
	// ErrTooManyUploads is somebody with too many uploads in progress.
	ErrTooManyUploads = errors.New("too many uploads in progress")
)

// MaxUploadsInProgress bounds one person's unfinished uploads: twice the files
// one message may carry, so attaching a full message never meets it, and few
// enough that reserving quota with abandoned uploads is not a strategy.
const MaxUploadsInProgress = 20

// Upload is a file being received in pieces.
type Upload struct {
	ID         uuid.UUID
	RoomID     uuid.UUID
	UploaderID uuid.UUID
	Filename   string
	Size       int64
	PieceBytes int
	Pieces     int
	Received   int64
	UpdatedAt  time.Time
}

func uploadOf(row dbgen.FileUpload) *Upload {
	return &Upload{
		ID: row.ID, RoomID: row.RoomID, UploaderID: row.UploaderID,
		Filename: row.Filename, Size: row.SizeBytes, PieceBytes: int(row.PieceBytes),
		Pieces: len(row.PieceKeys), Received: row.ReceivedBytes, UpdatedAt: row.UpdatedAt,
	}
}

/*
BeginUpload declares a file about to be sent in pieces.

The file limit is checked here against the declared size, before a byte is
sent; the caller checks the quotas, which need the settings. Whatever arrives is
checked again on finish, so a declaration that understated the size gains
nothing — the pieces cannot exceed it.
*/
func (s *Service) BeginUpload(
	ctx context.Context,
	roomID, uploaderID uuid.UUID,
	filename string,
	size int64,
	pieceBytes int,
) (*Upload, error) {
	if size < 0 {
		return nil, fmt.Errorf("%w: a size cannot be negative", ErrPieceSize)
	}
	if err := s.LimitsNow(ctx).CheckSize(size); err != nil {
		return nil, err
	}

	count, err := s.queries.CountFileUploadsBy(ctx, uploaderID)
	if err != nil {
		return nil, fmt.Errorf("attachments: counting uploads: %w", err)
	}
	if count >= MaxUploadsInProgress {
		return nil, ErrTooManyUploads
	}

	row, err := s.queries.CreateFileUpload(ctx, dbgen.CreateFileUploadParams{
		ID:         uuid.New(),
		UploaderID: uploaderID,
		RoomID:     roomID,
		Filename:   safeFilename(filename),
		SizeBytes:  size,
		PieceBytes: int32(pieceBytes),
	})
	if err != nil {
		return nil, fmt.Errorf("attachments: beginning an upload: %w", err)
	}
	return uploadOf(row), nil
}

// UploadFor returns an upload, if it is the uploader's.
func (s *Service) UploadFor(ctx context.Context, id, uploaderID uuid.UUID) (*Upload, error) {
	row, err := s.queries.GetFileUpload(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.UploaderID != uploaderID) {
		return nil, ErrUploadNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("attachments: reading an upload: %w", err)
	}
	return uploadOf(row), nil
}

/*
AddPiece stores the next piece of an upload.

Only the next piece is accepted, and every piece but the last must be exactly
the agreed size, so the pieces joined in order are the file. The body is read
at most one byte past the piece size, spooled to disk rather than memory, so
many pieces arriving at once cost disk rather than the server's working set.
*/
func (s *Service) AddPiece(
	ctx context.Context,
	id, uploaderID uuid.UUID,
	piece int,
	body io.Reader,
) (*Upload, error) {
	upload, err := s.UploadFor(ctx, id, uploaderID)
	if err != nil {
		return nil, err
	}
	if piece != upload.Pieces {
		return nil, fmt.Errorf("%w: the server holds %d pieces; send piece %d next",
			ErrPieceOutOfOrder, upload.Pieces, upload.Pieces)
	}

	spooled, size, err := s.spool(io.LimitReader(body, int64(upload.PieceBytes)+1))
	if err != nil {
		return nil, err
	}
	defer func() {
		spooled.Close()
		os.Remove(spooled.Name())
	}()

	remaining := upload.Size - upload.Received
	switch {
	case size == 0 || size > int64(upload.PieceBytes) || size > remaining:
		return nil, fmt.Errorf("%w: a piece is 1 to %d bytes, and %d remain",
			ErrPieceSize, upload.PieceBytes, remaining)
	case size < int64(upload.PieceBytes) && size < remaining:
		// Short, and not the last: joined in order, the file would be wrong.
		return nil, fmt.Errorf("%w: every piece but the last is %d bytes", ErrPieceSize, upload.PieceBytes)
	}

	key, err := pieceKey(id, piece)
	if err != nil {
		return nil, err
	}
	if err := s.store.Put(ctx, key, spooled, size, "application/octet-stream"); err != nil {
		return nil, fmt.Errorf("attachments: storing a piece: %w", err)
	}

	row, err := s.queries.AddFileUploadPiece(ctx, dbgen.AddFileUploadPieceParams{
		PieceKey: key, PieceSize: size, ID: id, Piece: int32(piece),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Another attempt at the same piece got there first. Its object is
		// untouched, because this one's key is its own.
		_ = s.store.Delete(ctx, key)
		return nil, fmt.Errorf("%w: that piece has already been received", ErrPieceOutOfOrder)
	}
	if err != nil {
		_ = s.store.Delete(ctx, key)
		return nil, fmt.Errorf("attachments: recording a piece: %w", err)
	}
	return uploadOf(row), nil
}

// pieceKey names a piece's object. Never a content key, so a piece is never
// reachable as an attachment, and never shared between two attempts.
func pieceKey(id uuid.UUID, piece int) (string, error) {
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("attachments: naming a piece: %w", err)
	}
	return fmt.Sprintf("uploads/%s/%06d-%s", id, piece, hex.EncodeToString(nonce)), nil
}

/*
FinishUpload joins the pieces and takes the file through the one path every
file takes.

The upload and its pieces are gone afterwards whether the file was accepted or
refused: a refused file is refused for what it is, and sending it again would
be refused again. Only a failure to read a piece keeps it, so that a store
having a bad moment does not lose somebody's upload.
*/
func (s *Service) FinishUpload(
	ctx context.Context,
	id, uploaderID uuid.UUID,
	uploaderAddress string,
) (*Attachment, error) {
	row, err := s.queries.GetFileUpload(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.UploaderID != uploaderID) {
		return nil, ErrUploadNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("attachments: reading an upload: %w", err)
	}
	if row.ReceivedBytes != row.SizeBytes {
		return nil, fmt.Errorf("%w: %d of %d bytes have arrived", ErrUploadIncomplete, row.ReceivedBytes, row.SizeBytes)
	}

	spooled, size, err := s.spool(&joinedPieces{ctx: ctx, service: s, keys: row.PieceKeys})
	if err != nil {
		return nil, err
	}
	defer func() {
		spooled.Close()
		os.Remove(spooled.Name())
	}()

	attachment, err := s.ingest(ctx, row.RoomID, uploaderID, uploaderAddress, row.Filename, spooled, size)
	s.discardUpload(ctx, row)
	return attachment, err
}

// AbandonUpload removes an upload the person no longer wants.
func (s *Service) AbandonUpload(ctx context.Context, id, uploaderID uuid.UUID) error {
	row, err := s.queries.GetFileUpload(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.UploaderID != uploaderID) {
		return ErrUploadNotFound
	}
	if err != nil {
		return fmt.Errorf("attachments: reading an upload: %w", err)
	}
	s.discardUpload(ctx, row)
	return nil
}

// discardUpload removes an upload's row, then its pieces. The row first: once
// it is gone nothing refers to the pieces, and a piece left behind by a failed
// delete costs disk rather than correctness.
func (s *Service) discardUpload(ctx context.Context, row dbgen.FileUpload) {
	if err := s.queries.DeleteFileUpload(ctx, row.ID); err != nil {
		s.log.Warn("cannot remove a finished upload", "upload", row.ID, "error", err)
		return
	}
	for _, key := range row.PieceKeys {
		_ = s.store.Delete(ctx, key)
	}
}

/*
ReclaimAbandonedUploads removes uploads nobody has added to for a while, with
their pieces. Somebody closing the tab part way through is the ordinary case;
this is what keeps it from costing disk for ever.
*/
func (s *Service) ReclaimAbandonedUploads(ctx context.Context, olderThan time.Duration, limit int32) (int, error) {
	rows, err := s.queries.ListAbandonedFileUploads(ctx, dbgen.ListAbandonedFileUploadsParams{
		UpdatedAt: time.Now().Add(-olderThan),
		Limit:     limit,
	})
	if err != nil {
		return 0, fmt.Errorf("attachments: listing abandoned uploads: %w", err)
	}
	for _, row := range rows {
		s.discardUpload(ctx, row)
	}
	return len(rows), nil
}

// joinedPieces reads an upload's pieces one after another.
type joinedPieces struct {
	ctx     context.Context
	service *Service
	keys    []string
	current io.ReadCloser
}

func (j *joinedPieces) Read(buffer []byte) (int, error) {
	for {
		if j.current == nil {
			if len(j.keys) == 0 {
				return 0, io.EOF
			}
			body, _, err := j.service.store.Get(j.ctx, j.keys[0])
			if err != nil {
				return 0, fmt.Errorf("attachments: reading a piece: %w", err)
			}
			j.current, j.keys = body, j.keys[1:]
		}
		n, err := j.current.Read(buffer)
		if errors.Is(err, io.EOF) {
			_ = j.current.Close()
			j.current = nil
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}
