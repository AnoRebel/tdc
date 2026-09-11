package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
)

/*
The bytes of attachments, which do not travel as JSON.

Send carries the JSON API, and deliberately caps and discards anything that is
not JSON. Media is the other half: a file somebody sent, fetched to show it, or
one they are sending, uploaded. Both go through the same per-server client as
everything else, so both carry the session in its cookie jar — which is the
whole reason they cannot be done from the webview, where that cookie is not.

The limit here is higher than Send's, because a photograph is legitimately
larger than any JSON reply, and lower than nothing, because a server this client
does not control is still on the other end.
*/

// The most an attachment may be, fetched or sent. Well above a photograph or a
// log and well below exhausting memory on the machine this runs on.
const mediaLimit = 64 << 20

// MediaResponse is a fetched file: its bytes and what kind it is, so the webview
// can show it without guessing from the path.
type MediaResponse struct {
	// The bytes, as the runtime marshals a []byte — base64 in the JSON that
	// crosses to the webview, decoded back to a Blob there.
	Data []byte `json:"data"`
	// The server's Content-Type, passed through so the webview labels the Blob
	// correctly rather than sniffing.
	ContentType string `json:"contentType"`
	Status      int    `json:"status"`
	OK          bool   `json:"ok"`
}

/*
FetchMedia retrieves one attachment's bytes from the current server.

The path is server-relative, as the API hands them out. The session travels in
the jar, so this reaches things a webview fetch would be refused. A refusal is a
MediaResponse with OK false and the status, not an error — the same shape Send
uses, and for the same reason: the interface needs the status to say why.
*/
func (s *Service) FetchMedia(ctx context.Context, path string) (MediaResponse, error) {
	s.mu.RLock()
	base, client := s.current, s.clients[s.current]
	s.mu.RUnlock()

	if base == "" || client == nil {
		return MediaResponse{}, ErrNoServer
	}

	url := base + "/" + strings.TrimPrefix(path, "/")
	outgoing, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return MediaResponse{}, fmt.Errorf("building the request: %w", err)
	}

	incoming, err := client.Do(outgoing)
	if err != nil {
		return MediaResponse{}, fmt.Errorf("reaching %s: %w", base, err)
	}
	defer incoming.Body.Close()

	data, err := io.ReadAll(io.LimitReader(incoming.Body, mediaLimit))
	if err != nil {
		return MediaResponse{}, fmt.Errorf("reading the file: %w", err)
	}

	return MediaResponse{
		Data:        data,
		ContentType: incoming.Header.Get("Content-Type"),
		Status:      incoming.StatusCode,
		OK:          incoming.StatusCode >= 200 && incoming.StatusCode < 300,
	}, nil
}

/*
Upload sends one file to a room and returns what the server made of it.

The bytes arrive from the webview as a []byte — the file the person picked, read
there and handed over whole. Progress is not reported: the runtime call is one
round trip, and the honest thing on the desktop is to show that a send is in
progress rather than invent a fraction. The webview's uploader treats this as a
transfer with no progress events, which its progress bar already tolerates.

The reply is the attachment as JSON, in the same Response shape Send returns, so
the transport above reads it the same way.
*/
func (s *Service) Upload(ctx context.Context, roomID, filename string, file []byte) (Response, error) {
	s.mu.RLock()
	base, client := s.current, s.clients[s.current]
	s.mu.RUnlock()

	if base == "" || client == nil {
		return Response{}, ErrNoServer
	}

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", filename)
	if err != nil {
		return Response{}, fmt.Errorf("building the upload: %w", err)
	}
	if _, err := part.Write(file); err != nil {
		return Response{}, fmt.Errorf("building the upload: %w", err)
	}
	if err := form.Close(); err != nil {
		return Response{}, fmt.Errorf("building the upload: %w", err)
	}

	url := base + "/api/rooms/" + roomID + "/attachments"
	outgoing, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	if err != nil {
		return Response{}, fmt.Errorf("building the request: %w", err)
	}
	outgoing.Header.Set("Content-Type", form.FormDataContentType())
	outgoing.Header.Set("Accept", "application/json")

	incoming, err := client.Do(outgoing)
	if err != nil {
		return Response{}, fmt.Errorf("reaching %s: %w", base, err)
	}
	defer incoming.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(incoming.Body, 10<<20))
	if err != nil {
		return Response{}, fmt.Errorf("reading the response: %w", err)
	}

	response := Response{
		Status: incoming.StatusCode,
		OK:     incoming.StatusCode >= 200 && incoming.StatusCode < 300,
	}
	if len(raw) > 0 {
		response.Body = raw
	}
	return response, nil
}
