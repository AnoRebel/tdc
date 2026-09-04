package attachments

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/storage"
)

// ErrNotFound is returned when no attachment has the given identifier.
var ErrNotFound = errors.New("attachments: not found")

// Attachment is a stored file and what is known about it.
type Attachment struct {
	ID              uuid.UUID
	StorageKey      string
	UploaderAddress string
	RoomID          uuid.UUID
	// EventID is empty until the message carrying this file is sent.
	EventID string
	// Filename is what the uploader called it, used only to name a download.
	Filename string
	// ContentType was detected from the content, never declared by the client.
	ContentType string
	Size        int64
	CreatedAt   time.Time
}

// Service accepts and serves attachments.
type Service struct {
	pool    *db.Pool
	queries *dbgen.Queries
	store   storage.Store
	limits  Limits
	// spoolDir is where an upload is held while it is being measured. Empty
	// uses the operating system's temporary directory.
	spoolDir string
}

// NewService wires the parts an attachment needs.
func NewService(pool *db.Pool, store storage.Store, limits Limits) *Service {
	return &Service{
		pool:    pool,
		queries: dbgen.New(pool),
		store:   store,
		limits:  limits,
	}
}

// Limits returns the operator's configured restrictions, so a client can show
// them before someone picks a file rather than after.
func (s *Service) Limits() Limits { return s.limits }

// Upload stores a file and records it against a room.
//
// The reader is not trusted to be honest about its length, so the file is
// spooled and measured rather than believed. declaredName is used only to name
// a download; anything it implies about the file's type is ignored in favour of
// what the content says.
func (s *Service) Upload(
	ctx context.Context,
	roomID uuid.UUID,
	uploaderID uuid.UUID,
	uploaderAddress string,
	declaredName string,
	body io.Reader,
) (*Attachment, error) {
	// One byte beyond the limit is enough to know the limit was exceeded, and
	// stops a client streaming an unbounded body into the spool.
	limit := s.limits.MaxFileSize
	if limit <= 0 {
		limit = DefaultLimits().MaxFileSize
	}

	spooled, size, err := s.spool(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	defer func() {
		spooled.Close()
		os.Remove(spooled.Name())
	}()

	if err := s.limits.CheckSize(size); err != nil {
		return nil, err
	}

	// The type is decided from the file's opening bytes, whatever the client
	// called it.
	head := make([]byte, detectionWindow)
	read, err := io.ReadFull(spooled, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, fmt.Errorf("attachments: reading the uploaded file: %w", err)
	}
	contentType := DetectType(head[:read])

	if err := s.limits.CheckType(contentType); err != nil {
		return nil, err
	}

	// Content-addressed, so the same file uploaded twice occupies one place and
	// an identifier cannot be guessed from another.
	key, err := storageKey(spooled)
	if err != nil {
		return nil, err
	}

	if _, err := spooled.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("attachments: rewinding the uploaded file: %w", err)
	}
	if err := s.store.Put(ctx, key, spooled, size, contentType); err != nil {
		return nil, fmt.Errorf("attachments: storing the uploaded file: %w", err)
	}

	record, err := s.queries.CreateAttachment(ctx, dbgen.CreateAttachmentParams{
		ID:              uuid.New(),
		StorageKey:      key,
		UploaderID:      &uploaderID,
		UploaderAddress: uploaderAddress,
		RoomID:          roomID,
		Filename:        safeFilename(declaredName),
		ContentType:     contentType,
		SizeBytes:       size,
	})
	if err != nil {
		// The bytes are already stored. Leaving them costs a little disk until
		// orphan reclamation runs, which is better than deleting an object a
		// concurrent upload of identical content is also using.
		return nil, fmt.Errorf("attachments: recording the upload: %w", err)
	}

	return fromRow(record), nil
}

// spool writes an upload to a temporary file and reports its true length.
//
// Object stores need the length before the body, and the length a client claims
// is not evidence. Spooling to disk rather than memory means a large upload
// costs disk rather than the server's working set, and many concurrent uploads
// cannot exhaust memory between them.
func (s *Service) spool(body io.Reader) (*os.File, int64, error) {
	file, err := os.CreateTemp(s.spoolDir, "tdc-upload-*")
	if err != nil {
		return nil, 0, fmt.Errorf("attachments: creating a temporary file: %w", err)
	}

	size, err := io.Copy(file, body)
	if err != nil {
		file.Close()
		os.Remove(file.Name())
		return nil, 0, fmt.Errorf("attachments: receiving the upload: %w", err)
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		file.Close()
		os.Remove(file.Name())
		return nil, 0, fmt.Errorf("attachments: rewinding the upload: %w", err)
	}

	return file, size, nil
}

// storageKey derives a key from the file's contents.
//
// Content-addressed so identical uploads share one object, and so a key says
// nothing about who uploaded what or when. Reading the whole file again is the
// cost; it is already on local disk at this point.
func storageKey(file *os.File) (string, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("attachments: rewinding to hash: %w", err)
	}

	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", fmt.Errorf("attachments: hashing the upload: %w", err)
	}

	return hex.EncodeToString(digest.Sum(nil)), nil
}

// safeFilename reduces an uploader's filename to something safe to store and
// hand back.
//
// The name is only ever shown and used to name a download, so nothing here
// affects where the file goes. It still cannot be trusted: a name carrying path
// separators or control characters is a problem for whatever displays it.
func safeFilename(name string) string {
	// Take the last segment under either separator, so "../../etc/passwd" and
	// `..\..\config` both reduce to their final component.
	name = strings.ReplaceAll(name, `\`, "/")
	name = filepath.Base(name)

	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)

	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "file"
	}

	// Long enough for any real name, short enough not to break a layout.
	if len(name) > 200 {
		name = name[:200]
	}

	return name
}

// Get returns one attachment.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Attachment, error) {
	record, err := s.queries.GetAttachment(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("attachments: reading %s: %w", id, err)
	}
	return fromRow(record), nil
}

// Open returns an attachment's content for streaming.
func (s *Service) Open(ctx context.Context, attachment *Attachment) (io.ReadCloser, error) {
	reader, _, err := s.store.Get(ctx, attachment.StorageKey)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("attachments: reading %s: %w", attachment.ID, err)
	}
	return reader, nil
}

// PresignedURL returns a URL the browser can follow straight to the object
// store, or "" when this store cannot issue one and the server must stream it.
func (s *Service) PresignedURL(ctx context.Context, attachment *Attachment, ttl time.Duration) (string, error) {
	return s.store.PresignGet(ctx, attachment.StorageKey, ttl, attachment.Filename)
}

// ForEvent lists the attachments carried by a message.
func (s *Service) ForEvent(ctx context.Context, eventID string) ([]*Attachment, error) {
	records, err := s.queries.ListAttachmentsForEvent(ctx, &eventID)
	if err != nil {
		return nil, fmt.Errorf("attachments: listing for %s: %w", eventID, err)
	}

	list := make([]*Attachment, 0, len(records))
	for _, record := range records {
		list = append(list, fromRow(record))
	}
	return list, nil
}

// Bind attaches uploads to the message that carries them.
//
// Scoped to the uploader and the room, so one person cannot attach another's
// upload to their message, and an attachment cannot be moved once bound. It
// reports how many were bound: a caller that asked for three and bound two has
// been given an identifier that was not theirs to use.
func (s *Service) Bind(
	ctx context.Context,
	eventID string,
	roomID uuid.UUID,
	uploaderID uuid.UUID,
	ids []uuid.UUID,
) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	if err := s.limits.CheckCount(len(ids)); err != nil {
		return 0, err
	}

	bound, err := s.queries.AttachToEvent(ctx, dbgen.AttachToEventParams{
		EventID:       &eventID,
		AttachmentIds: ids,
		UploaderID:    &uploaderID,
		RoomID:        roomID,
	})
	if err != nil {
		return 0, fmt.Errorf("attachments: attaching to %s: %w", eventID, err)
	}

	return bound, nil
}

// CheckBatch reports whether a set of uploads may be carried by one message.
//
// Called before the message is accepted, so a refusal names the limit rather
// than leaving a message posted with files missing from it.
func (s *Service) CheckBatch(ctx context.Context, ids []uuid.UUID) error {
	if err := s.limits.CheckCount(len(ids)); err != nil {
		return err
	}

	var total int64
	for _, id := range ids {
		attachment, err := s.Get(ctx, id)
		if err != nil {
			return err
		}
		total += attachment.Size
	}

	return s.limits.CheckTotal(total)
}

func fromRow(row dbgen.Attachment) *Attachment {
	attachment := &Attachment{
		ID:              row.ID,
		StorageKey:      row.StorageKey,
		UploaderAddress: row.UploaderAddress,
		RoomID:          row.RoomID,
		Filename:        row.Filename,
		ContentType:     row.ContentType,
		Size:            row.SizeBytes,
	}
	if row.EventID != nil {
		attachment.EventID = *row.EventID
	}
	attachment.CreatedAt = row.CreatedAt
	return attachment
}
