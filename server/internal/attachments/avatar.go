package attachments

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"uuid"
)

/*
Pictures people choose for themselves.

The interface crops and scales the picture before it is sent, but nothing a
client sends is trusted to be what it says: the bytes are sniffed for their
type, decoded under the same limits as a preview, and written again by the
server. Writing them again is also what removes whatever the original carried
besides pixels — a phone photograph's location, the camera, the time it was
taken — which somebody choosing a picture of their face did not mean to
publish.
*/

// MaxAvatarBytes is the largest picture accepted, as sent.
const MaxAvatarBytes = 2 << 20

var (
	// ErrAvatarTooLarge is a picture over MaxAvatarBytes.
	ErrAvatarTooLarge = errors.New("attachments: the picture is larger than allowed")
	// ErrNotAnImage is a picture that is not a PNG, JPEG, GIF or WebP image.
	ErrNotAnImage = errors.New("attachments: the picture is not an image this server reads")
)

// Avatar is a stored picture: where it is, and what it is.
type Avatar struct {
	Key         string
	ContentType string
}

/*
PutAvatar checks, rewrites and stores a picture for a person.

The key is new each time, under the person's own prefix, so the address the
picture is served at can be cached for good: a new picture is a new address.
The old file is the caller's to forget, which recording the new key does.
*/
func (s *Service) PutAvatar(ctx context.Context, userID uuid.UUID, body io.Reader) (*Avatar, error) {
	data, err := io.ReadAll(io.LimitReader(body, MaxAvatarBytes+1))
	if err != nil {
		return nil, fmt.Errorf("attachments: reading the picture: %w", err)
	}
	if len(data) > MaxAvatarBytes {
		return nil, ErrAvatarTooLarge
	}

	// What the bytes are, not what the request said they were.
	contentType := http.DetectContentType(data)
	if !CanThumbnail(contentType) {
		return nil, ErrNotAnImage
	}

	rewritten, err := GenerateThumbnail(bytes.NewReader(data), contentType)
	if err != nil {
		if errors.Is(err, ErrSourceTooLarge) {
			return nil, ErrAvatarTooLarge
		}
		return nil, ErrNotAnImage
	}

	token := make([]byte, 12)
	if _, err := rand.Read(token); err != nil {
		return nil, fmt.Errorf("attachments: naming the picture: %w", err)
	}
	key := "avatars/" + userID.String() + "/" + hex.EncodeToString(token)

	if err := s.store.Put(ctx, key, bytes.NewReader(rewritten.Data), int64(len(rewritten.Data)), rewritten.ContentType); err != nil {
		return nil, fmt.Errorf("attachments: storing the picture: %w", err)
	}
	return &Avatar{Key: key, ContentType: rewritten.ContentType}, nil
}

// OpenAvatar reads a stored picture. The caller closes it.
func (s *Service) OpenAvatar(ctx context.Context, key string) (io.ReadCloser, error) {
	body, _, err := s.store.Get(ctx, key)
	return body, err
}
