package attachments

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"io"
	"log/slog"
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
	ID         uuid.UUID
	StorageKey string
	// UploaderID is the uploader's account here, or zero for a file another
	// server's member sent.
	UploaderID      uuid.UUID
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

	// Animated says the image moves, so a client shows the file itself rather
	// than a still preview of its first frame.
	Animated bool

	// Caption is what the sender wrote about this file.
	//
	// Per attachment rather than per message: sending four photographs with a
	// line about each is the ordinary case, and one message body cannot say
	// which line describes which picture.
	Caption string

	// Width and Height are the original's dimensions where they are known, so
	// a client can reserve the right space before the image arrives rather
	// than letting the conversation jump as attachments load.
	Width  int
	Height int

	// PreviewKey is empty when no preview was generated, which is the common
	// case: most attachments are not images.
	PreviewKey    string
	PreviewType   string
	PreviewWidth  int
	PreviewHeight int

	/*
		SensitiveSource says who decided this is sensitive, and is empty when
		nobody has.

		Recorded rather than a bare boolean because who marked it decides who
		may unmark it: an uploader may change their mind about their own file,
		but a moderator's decision is not theirs to undo. A boolean would make
		marking advisory, and the people most likely to post something
		unwelcome would be exactly the people able to unmark it.
	*/
	SensitiveSource SensitiveSource
	// SensitiveBy is who marked it, as an address. Shown to moderators so a
	// disputed mark can be traced; never shown to other viewers.
	SensitiveBy string
}

// Sensitive reports whether this media should be obscured until a viewer
// chooses to reveal it.
func (a *Attachment) Sensitive() bool { return a.SensitiveSource != "" }

// HasPreview reports whether a reduced version of this attachment exists.
func (a *Attachment) HasPreview() bool { return a.PreviewKey != "" }

// Service accepts and serves attachments.
type Service struct {
	pool    *db.Pool
	queries *dbgen.Queries
	store   storage.Store
	limits  Limits
	// limitsNow, when set, supplies the limits per request, so an operator's
	// change applies to the next upload without a restart.
	limitsNow func(context.Context) Limits
	// spoolDir is where an upload is held while it is being measured. Empty
	// uses the operating system's temporary directory.
	spoolDir string

	// processor makes previews. Defaults to doing the work in this process,
	// which is safe because decoding is pure Go; an operator who wants the
	// isolation can point it at a sidecar instead.
	processor Processor

	// scanner is nil when the operator has configured none, which is the
	// default and an ordinary way to run.
	scanner Scanner
	// scannerFailOpen accepts a file the scanner could not decide on. Off by
	// default: a scanner that is down must not become a silent bypass.
	scannerFailOpen bool

	log *slog.Logger
}

// NewService wires the parts an attachment needs.
func NewService(pool *db.Pool, store storage.Store, limits Limits, log *slog.Logger) *Service {
	return &Service{
		pool:      pool,
		queries:   dbgen.New(pool),
		store:     store,
		limits:    limits,
		log:       log,
		processor: LocalProcessor{},
	}
}

// WithProcessor sends preview generation elsewhere.
//
// Separate from the constructor for the same reason as the scanner: doing the
// work in this process is the default, and threading a processor through every
// call site would suggest otherwise.
func (s *Service) WithProcessor(processor Processor) *Service {
	s.processor = processor
	return s
}

// WithScanner attaches a malware scanner and its failure policy.
//
// Separate from the constructor because scanning is optional: a server with
// none configured is the default, and threading a nil through every call site
// would suggest otherwise.
func (s *Service) WithScanner(scanner Scanner, failOpen bool) *Service {
	s.scanner = scanner
	s.scannerFailOpen = failOpen
	return s
}

// Limits returns the operator's configured restrictions, so a client can show
// them before someone picks a file rather than after.
func (s *Service) Limits() Limits { return s.limits }

// WithLimits reads the limits from somewhere that can change while the server
// runs, such as the operator's settings.
func (s *Service) WithLimits(now func(context.Context) Limits) *Service {
	s.limitsNow = now
	return s
}

// LimitsNow returns the limits that apply to a request being made now.
func (s *Service) LimitsNow(ctx context.Context) Limits {
	if s.limitsNow != nil {
		return s.limitsNow(ctx)
	}
	return s.limits
}

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
	limit := s.LimitsNow(ctx).MaxFileSize
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

	return s.ingest(ctx, roomID, uploaderID, uploaderAddress, declaredName, spooled, size)
}

/*
ingest checks, stores and records a file already spooled to disk.

The one path every file takes, whether it arrived in one request or in pieces:
whatever is checked about a file is checked here, so a way of uploading cannot
be the way around a check.
*/
func (s *Service) ingest(
	ctx context.Context,
	roomID uuid.UUID,
	uploaderID uuid.UUID,
	uploaderAddress string,
	declaredName string,
	spooled *os.File,
	size int64,
) (*Attachment, error) {
	limits := s.LimitsNow(ctx)
	if err := limits.CheckSize(size); err != nil {
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

	if err := limits.CheckType(contentType); err != nil {
		return nil, err
	}

	// Scanned before it is stored, so a rejected file never reaches the object
	// store at all — cleaning one up afterwards would leave a window where it
	// was present.
	if err := s.scan(ctx, spooled, size); err != nil {
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

	// A preview is a convenience, so a failure to make one must not fail the
	// upload: the file is still perfectly usable without it, and a malformed
	// image is exactly the case where generation is expected to fail.
	// Decided from the file's own structure, once, rather than on every read.
	animated := false
	if _, err := spooled.Seek(0, io.SeekStart); err == nil {
		animated = IsAnimated(spooled, contentType)
	}

	preview := s.makePreview(ctx, spooled, contentType, animated)

	record, err := s.queries.CreateAttachment(ctx, dbgen.CreateAttachmentParams{
		ID:              uuid.New(),
		StorageKey:      key,
		UploaderID:      &uploaderID,
		UploaderAddress: uploaderAddress,
		RoomID:          roomID,
		Filename:        safeFilename(declaredName),
		ContentType:     contentType,
		SizeBytes:       size,
		PreviewKey:      preview.key,
		PreviewType:     preview.contentType,
		PreviewWidth:    preview.width,
		PreviewHeight:   preview.height,
		Width:           preview.sourceWidth,
		Height:          preview.sourceHeight,
		Animated:        animated,
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
/*
MaxCaption bounds a caption.

Long enough for a sentence or two about a picture, short enough that the field
cannot become a second message body — which matters because a caption is not
searched, not moderated, and not subject to the message length limit.
*/
const MaxCaption = 1000

// ErrCaptionNotYours is returned when captioning somebody else's upload, or
// one already sent.
var ErrCaptionNotYours = errors.New("that attachment is not yours to caption")

/*
Caption records what the sender wrote about one file.

Only before the message is sent, and only the uploader's own: a caption is
written while composing. Editing one after the fact would be editing a message,
which goes through the message-editing path so that it is recorded as an edit.
*/
func (s *Service) Caption(
	ctx context.Context,
	id, uploaderID uuid.UUID,
	caption string,
) error {
	if len([]rune(caption)) > MaxCaption {
		caption = string([]rune(caption)[:MaxCaption])
	}

	affected, err := s.queries.SetAttachmentCaption(ctx, dbgen.SetAttachmentCaptionParams{
		ID:         id,
		UploaderID: &uploaderID,
		Caption:    caption,
	})
	if err != nil {
		return fmt.Errorf("attachments: writing a caption: %w", err)
	}
	if affected == 0 {
		return ErrCaptionNotYours
	}

	return nil
}

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

	if err := s.LimitsNow(ctx).CheckCount(len(ids)); err != nil {
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
	limits := s.LimitsNow(ctx)
	if err := limits.CheckCount(len(ids)); err != nil {
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

	return limits.CheckTotal(total)
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
		Caption:         row.Caption,
		Animated:        row.Animated,
	}
	if row.EventID != nil {
		attachment.EventID = *row.EventID
	}
	if row.UploaderID != nil {
		attachment.UploaderID = *row.UploaderID
	}
	// Both or neither, which the schema enforces.
	if row.SensitiveSource != nil {
		attachment.SensitiveSource = SensitiveSource(*row.SensitiveSource)
	}
	if row.SensitiveBy != nil {
		attachment.SensitiveBy = *row.SensitiveBy
	}
	if row.PreviewKey != nil {
		attachment.PreviewKey = *row.PreviewKey
	}
	if row.PreviewType != nil {
		attachment.PreviewType = *row.PreviewType
	}
	if row.PreviewWidth != nil {
		attachment.PreviewWidth = int(*row.PreviewWidth)
	}
	if row.PreviewHeight != nil {
		attachment.PreviewHeight = int(*row.PreviewHeight)
	}
	if row.Width != nil {
		attachment.Width = int(*row.Width)
	}
	if row.Height != nil {
		attachment.Height = int(*row.Height)
	}
	attachment.CreatedAt = row.CreatedAt
	return attachment
}

// previewResult is what preview generation produced, with every field nil when
// no preview was made.
type previewResult struct {
	key          *string
	contentType  *string
	width        *int32
	height       *int32
	sourceWidth  *int32
	sourceHeight *int32
}

// makePreview generates and stores a reduced version of an image.
//
// Every failure is swallowed deliberately. A preview is a convenience: an
// attachment without one still downloads, still displays once fetched, and
// still shows its name and size. Failing the upload because a reduced copy
// could not be made would turn a cosmetic problem into a lost file — and a
// malformed image, which is precisely where generation fails, is also precisely
// where someone most wants the original preserved for inspection.
func (s *Service) makePreview(
	ctx context.Context,
	spooled *os.File,
	contentType string,
	animated bool,
) previewResult {
	var result previewResult

	if !CanThumbnail(contentType) {
		return result
	}

	if _, err := spooled.Seek(0, io.SeekStart); err != nil {
		return result
	}

	// The source dimensions are worth recording even when the preview itself
	// fails, so a client can still reserve the right space.
	if config, _, err := image.DecodeConfig(spooled); err == nil {
		width, height := int32(config.Width), int32(config.Height)
		result.sourceWidth, result.sourceHeight = &width, &height
	}

	// An animation gets dimensions but no preview. A thumbnail is a still
	// frame, and a timeline showing it in place of the animation removes the
	// one thing about a GIF that matters.
	if animated {
		return result
	}

	if _, err := spooled.Seek(0, io.SeekStart); err != nil {
		return result
	}

	// Bounded, so a sidecar that stops answering cannot hold an upload open.
	// The upload itself is already stored by this point either way: a preview
	// is a convenience, and losing one must never lose the file.
	processCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	thumbnail, err := s.processor.Process(processCtx, spooled, contentType)
	if err != nil {
		if !errors.Is(err, ErrNotThumbnailable) {
			// A configured sidecar that is unreachable is worth an operator
			// seeing, where an ordinary PDF having no preview is not.
			s.log.Debug("cannot generate a preview", "error", err)
		}
		return result
	}

	// Content-addressed like the original, so two identical previews share one
	// object and a key discloses nothing.
	digest := sha256.Sum256(thumbnail.Data)
	key := "preview/" + hex.EncodeToString(digest[:])

	if err := s.store.Put(ctx, key, bytes.NewReader(thumbnail.Data),
		int64(len(thumbnail.Data)), thumbnail.ContentType); err != nil {
		return result
	}

	width, height := int32(thumbnail.Width), int32(thumbnail.Height)
	result.key = &key
	result.contentType = &thumbnail.ContentType
	result.width, result.height = &width, &height

	return result
}

// OpenPreview returns an attachment's reduced version for streaming.
func (s *Service) OpenPreview(ctx context.Context, attachment *Attachment) (io.ReadCloser, error) {
	if !attachment.HasPreview() {
		return nil, ErrNotFound
	}

	reader, _, err := s.store.Get(ctx, attachment.PreviewKey)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("attachments: reading the preview of %s: %w", attachment.ID, err)
	}

	return reader, nil
}

// Delete removes an attachment and, when nothing else needs them, its bytes.
//
// The storage key is content-derived, so two people uploading the same file
// share one stored object. Deleting one person's attachment must therefore not
// remove the object another still points at — the reference count is what makes
// that safe, and it is why this cannot simply delete the key.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	attachment, err := s.Get(ctx, id)
	if err != nil {
		return err
	}

	// The record goes first. Once it is gone the attachment is unreachable,
	// which is what the requirement asks for; the bytes are housekeeping.
	if err := s.queries.DeleteAttachment(ctx, id); err != nil {
		return fmt.Errorf("attachments: deleting %s: %w", id, err)
	}

	s.reclaimIfUnreferenced(ctx, attachment)
	return nil
}

// DeleteForEvent removes every attachment a message carried.
//
// Called when a message is deleted, so that removing a message removes what it
// carried rather than leaving the files reachable by anyone who kept a link.
func (s *Service) DeleteForEvent(ctx context.Context, eventID string) error {
	files, err := s.ForEvent(ctx, eventID)
	if err != nil {
		return err
	}

	for _, attachment := range files {
		if err := s.Delete(ctx, attachment.ID); err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
	}

	return nil
}

// reclaimIfUnreferenced removes stored bytes that nothing points at any more.
//
// Failures are logged rather than returned. The attachment is already gone as
// far as anyone can tell; an object left behind costs disk until the next sweep
// and is far better than an error on a deletion that did in fact happen.
func (s *Service) reclaimIfUnreferenced(ctx context.Context, attachment *Attachment) {
	remaining, err := s.queries.CountAttachmentsWithStorageKey(ctx, attachment.StorageKey)
	if err == nil && remaining == 0 {
		_ = s.store.Delete(ctx, attachment.StorageKey)
	}

	if attachment.PreviewKey == "" {
		return
	}

	// A preview is content-addressed too, and two attachments of the same
	// image share one, so it needs the same count.
	previewKey := attachment.PreviewKey
	remainingPreviews, err := s.queries.CountAttachmentsWithPreviewKey(ctx, &previewKey)
	if err == nil && remainingPreviews == 0 {
		_ = s.store.Delete(ctx, previewKey)
	}
}

// ReclaimOrphans removes uploads that never became part of a message.
//
// An attachment is uploaded before the message that carries it, so one that is
// never sent leaves a row and an object behind. Someone who picks a file and
// then changes their mind is the ordinary case, not an attack, and this is the
// cost of letting them do that.
//
// olderThan should be comfortably longer than a slow upload followed by a
// person taking their time: reclaiming an attachment somebody is about to send
// would lose it. Returns how many were removed.
func (s *Service) ReclaimOrphans(ctx context.Context, olderThan time.Duration, limit int32) (int, error) {
	cutoff := time.Now().Add(-olderThan)

	orphans, err := s.queries.ListOrphanedAttachments(ctx, dbgen.ListOrphanedAttachmentsParams{
		CreatedAt: cutoff,
		Limit:     limit,
	})
	if err != nil {
		return 0, fmt.Errorf("attachments: listing orphans: %w", err)
	}

	removed := 0
	for _, row := range orphans {
		attachment := fromRow(row)

		if err := s.queries.DeleteAttachment(ctx, attachment.ID); err != nil {
			// One failure must not stop the sweep; the rest are still worth
			// reclaiming and this one is retried next time.
			continue
		}

		s.reclaimIfUnreferenced(ctx, attachment)
		removed++
	}

	return removed, nil
}
