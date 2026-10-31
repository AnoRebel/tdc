package attachments

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/storage"
)

/*
Files a member of another server sent.

A federated message describes its files — name, type, size and SHA-256 — in
content its author's server signed. The receiving server records each as a row
with origin_server set and no bytes, and copies the bytes the first time one of
its members opens it. Whatever the bytes came through, they are checked against
the signed digest, so the server they were copied from is trusted for nothing
but availability.
*/

var (
	// ErrCopyRefused is a copy that was not stored, with the reason.
	ErrCopyRefused = errors.New("the file could not be copied")
	// ErrTooLargeToCopy is a file over the size this server copies.
	ErrTooLargeToCopy = errors.New("that file is too large to copy to this server")
)

// RemoteFile is a description another server's message carried.
type RemoteFile struct {
	Filename    string
	ContentType string
	Size        int64
	SHA256      string
	Width       int
	Height      int
	Caption     string
}

/*
RecordRemote records the files a message from another server describes.

Once per message: a delivery that arrives again finds the rows already there.
Each row gets an identifier of this server's own, never the one the far side
used, so a peer cannot choose one that collides with a file uploaded here.
*/
func (s *Service) RecordRemote(
	ctx context.Context,
	roomID uuid.UUID,
	eventID, senderAddress, origin string,
	files []RemoteFile,
) error {
	existing, err := s.queries.ListAttachmentsForEvent(ctx, &eventID)
	if err != nil {
		return fmt.Errorf("attachments: checking a message's files: %w", err)
	}
	if len(existing) > 0 {
		return nil
	}

	for _, file := range files {
		_, err := s.queries.CreateRemoteAttachment(ctx, dbgen.CreateRemoteAttachmentParams{
			ID:              uuid.New(),
			StorageKey:      file.SHA256,
			UploaderAddress: senderAddress,
			RoomID:          roomID,
			EventID:         &eventID,
			Filename:        safeFilename(file.Filename),
			// The sender's claim, shown until the bytes arrive and decided
			// again from them when they do.
			ContentType:  file.ContentType,
			SizeBytes:    file.Size,
			Width:        optionalDimension(file.Width),
			Height:       optionalDimension(file.Height),
			Caption:      truncateCaption(file.Caption),
			OriginServer: &origin,
		})
		if err != nil {
			return fmt.Errorf("attachments: recording a file from %s: %w", origin, err)
		}
	}
	return nil
}

func optionalDimension(value int) *int32 {
	if value <= 0 {
		return nil
	}
	dimension := int32(value)
	return &dimension
}

func truncateCaption(caption string) string {
	if len([]rune(caption)) > MaxCaption {
		return string([]rune(caption)[:MaxCaption])
	}
	return caption
}

// RoomFile finds the file a sent message in a room carries, by its digest.
func (s *Service) RoomFile(ctx context.Context, roomID uuid.UUID, digest string) (*Attachment, error) {
	row, err := s.queries.RoomFileByKey(ctx, dbgen.RoomFileByKeyParams{RoomID: roomID, StorageKey: digest})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("attachments: finding a file in a room: %w", err)
	}
	return fromRow(row), nil
}

/*
HeldHere reports whether a file's bytes are in this server's store.

Asked of the store rather than of fetched_at, so a copy lost since — a restore
that did not carry it, which by design it does not — is fetched again rather
than served as missing.
*/
func (s *Service) HeldHere(ctx context.Context, attachment *Attachment) bool {
	_, err := s.store.Stat(ctx, attachment.StorageKey)
	return err == nil
}

// copying makes concurrent views of one file wait for one copy.
var copying sync.Map // digest → *sync.Mutex

/*
CopyRemote fetches a file from another server's description into this one.

fetch opens the bytes from wherever the caller decided they come from. At most
the described size plus one byte is read; the length and the SHA-256 must both
match what the author's server signed; the type is detected from the bytes and
held to this server's own type limits; and the scanner, where there is one,
sees it before it is stored. Anything else is refused and nothing is kept.
*/
func (s *Service) CopyRemote(
	ctx context.Context,
	attachment *Attachment,
	maxBytes int64,
	fetch func(context.Context) (io.ReadCloser, error),
) error {
	if attachment.OriginServer == "" {
		return nil
	}
	if maxBytes > 0 && attachment.Size > maxBytes {
		return fmt.Errorf("%w: it is %d bytes and this server copies at most %d",
			ErrTooLargeToCopy, attachment.Size, maxBytes)
	}

	lock, _ := copying.LoadOrStore(attachment.StorageKey, &sync.Mutex{})
	lock.(*sync.Mutex).Lock()
	defer lock.(*sync.Mutex).Unlock()

	// Somebody else's view may have copied it while this one waited, or the
	// same bytes may already be here as some other file.
	if !s.HeldHere(ctx, attachment) {
		if err := s.copyBytes(ctx, attachment, fetch); err != nil {
			return err
		}
	}
	return s.markCopied(ctx, attachment)
}

func (s *Service) copyBytes(
	ctx context.Context,
	attachment *Attachment,
	fetch func(context.Context) (io.ReadCloser, error),
) error {
	body, err := fetch(ctx)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrCopyRefused, err)
	}
	defer body.Close()

	spooled, size, err := s.spool(io.LimitReader(body, attachment.Size+1))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrCopyRefused, err)
	}
	defer func() {
		spooled.Close()
		os.Remove(spooled.Name())
	}()

	if size != attachment.Size {
		return fmt.Errorf("%w: %d bytes arrived where %d were described", ErrCopyRefused, size, attachment.Size)
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, spooled); err != nil {
		return fmt.Errorf("%w: %w", ErrCopyRefused, err)
	}
	if hex.EncodeToString(digest.Sum(nil)) != attachment.StorageKey {
		return fmt.Errorf("%w: the bytes do not match the digest their author signed", ErrCopyRefused)
	}

	contentType, err := detectSpooled(spooled)
	if err != nil {
		return err
	}
	if err := s.LimitsNow(ctx).CheckType(contentType); err != nil {
		return err
	}
	if err := s.scan(ctx, spooled, size); err != nil {
		return err
	}
	if _, err := spooled.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return s.store.Put(ctx, attachment.StorageKey, spooled, size, contentType)
}

// markCopied records what the bytes turned out to be, on every description of
// them, with a preview where one can be made.
func (s *Service) markCopied(ctx context.Context, attachment *Attachment) error {
	reader, object, err := s.store.Get(ctx, attachment.StorageKey)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	defer reader.Close()

	spooled, _, err := s.spool(reader)
	if err != nil {
		return err
	}
	defer func() {
		spooled.Close()
		os.Remove(spooled.Name())
	}()

	contentType := object.ContentType
	if contentType == "" {
		if contentType, err = detectSpooled(spooled); err != nil {
			return err
		}
	}
	animated := false
	if _, err := spooled.Seek(0, io.SeekStart); err == nil {
		animated = IsAnimated(spooled, contentType)
	}
	preview := s.makePreview(ctx, spooled, contentType, animated)

	return s.queries.MarkRemoteAttachmentsFetched(ctx, dbgen.MarkRemoteAttachmentsFetchedParams{
		ContentType:   contentType,
		Animated:      animated,
		Width:         preview.sourceWidth,
		Height:        preview.sourceHeight,
		PreviewKey:    preview.key,
		PreviewType:   preview.contentType,
		PreviewWidth:  preview.width,
		PreviewHeight: preview.height,
		StorageKey:    attachment.StorageKey,
	})
}

// detectSpooled types a spooled file from its opening bytes.
func detectSpooled(spooled *os.File) (string, error) {
	if _, err := spooled.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	head := make([]byte, detectionWindow)
	read, err := io.ReadFull(spooled, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", fmt.Errorf("attachments: reading a copied file: %w", err)
	}
	return DetectType(head[:read]), nil
}
