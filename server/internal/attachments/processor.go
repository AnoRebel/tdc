package attachments

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

/*
Processor turns an image into a preview.

An interface with two implementations, chosen by configuration. The default does
the work in this process, which is safe here because decoding is pure Go — the
whole server builds with CGO disabled, so there is no image library in C to be
exploited. An operator who wants a decoder failure to take down something other
than the server can point this at a sidecar instead.

Neither is more correct than the other. One container is far easier to
self-host, which is this project's premise; a large or public deployment may
reasonably prefer the isolation.
*/
type Processor interface {
	// Process returns a preview, or ErrNotThumbnailable when the type has none
	// to give.
	Process(ctx context.Context, content io.Reader, contentType string) (*Thumbnail, error)
}

// LocalProcessor decodes in this process.
type LocalProcessor struct{}

func (LocalProcessor) Process(_ context.Context, content io.Reader, contentType string) (*Thumbnail, error) {
	return GenerateThumbnail(content, contentType)
}

// SidecarProcessor sends the work to another container.
type SidecarProcessor struct {
	address string
	client  *http.Client
}

// NewSidecarProcessor talks to a media sidecar at the given address.
func NewSidecarProcessor(address string, timeout time.Duration) *SidecarProcessor {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	return &SidecarProcessor{
		address: address,
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				DialContext:       (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
				DisableKeepAlives: false,
				// The sidecar is on the operator's own network, named in their
				// configuration. A proxy from the environment would send an
				// upload somewhere they did not choose.
				Proxy: nil,
			},
		},
	}
}

// sidecarResponse is what the sidecar returns.
//
// The image comes back base64-encoded inside JSON rather than as a second
// request, so one exchange carries both the preview and its dimensions and
// there is no half-finished state between two calls.
type sidecarResponse struct {
	Data        []byte `json:"data"`
	ContentType string `json:"contentType"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	// Reason is set when the sidecar declined, so the caller can tell "this
	// type has no preview" from "something went wrong".
	Reason string `json:"reason,omitempty"`
}

func (s *SidecarProcessor) Process(ctx context.Context, content io.Reader, contentType string) (*Thumbnail, error) {
	if !CanThumbnail(contentType) {
		// Asked and answered here rather than over the network: sending a PDF
		// to be refused wastes a round trip and the sidecar's time.
		return nil, ErrNotThumbnailable
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.address+"/thumbnail", content)
	if err != nil {
		return nil, fmt.Errorf("attachments: building the sidecar request: %w", err)
	}
	request.Header.Set("Content-Type", contentType)

	response, err := s.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("attachments: reaching the media sidecar: %w", err)
	}
	defer response.Body.Close()

	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
		// The sidecar looked and decided there was nothing to make. Not a
		// failure: a malformed image is exactly where generation is expected
		// to fail, and the attachment is still perfectly usable without one.
		return nil, ErrNotThumbnailable
	default:
		return nil, fmt.Errorf("attachments: the media sidecar answered %d", response.StatusCode)
	}

	// Bounded, because a compromised or confused sidecar must not be able to
	// stream indefinitely into this process. A preview is a few tens of
	// kilobytes; this is generous.
	var decoded sidecarResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("attachments: reading the sidecar's reply: %w", err)
	}

	if len(decoded.Data) == 0 {
		return nil, ErrNotThumbnailable
	}

	// The returned bytes are decoded here to confirm they really are the image
	// the sidecar claims. A preview is served to browsers, and trusting a
	// declared type from another process is the same mistake as trusting one
	// from a client.
	if _, err := GenerateThumbnailBoundsCheck(bytes.NewReader(decoded.Data)); err != nil {
		return nil, fmt.Errorf("attachments: the sidecar returned something that is not an image: %w", err)
	}

	return &Thumbnail{
		Data:        decoded.Data,
		ContentType: decoded.ContentType,
		Width:       decoded.Width,
		Height:      decoded.Height,
	}, nil
}
