package attachments_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AnoRebel/tdc/server/internal/attachments"
)

// TestLocalProcessorIsTheDefault checks that a server with no sidecar
// configured still makes previews.
//
// One container is the default deployment, and it has to work without any of
// this.
func TestLocalProcessorIsTheDefault(t *testing.T) {
	source := encodePNG(t, drawTestImage(900, 600))

	thumbnail, err := attachments.LocalProcessor{}.Process(
		context.Background(), bytes.NewReader(source), "image/png")
	if err != nil {
		t.Fatalf("the local processor failed: %v", err)
	}
	if thumbnail.Width > 512 {
		t.Errorf("the preview is %d wide, beyond the bound", thumbnail.Width)
	}
}

// TestSidecarUnavailableDoesNotLoseTheUpload is what matters operationally.
//
// A configured sidecar that is down must cost a preview, not a file. The
// attachment is already stored by the time this runs, and the caller treats
// any error here as "no preview".
func TestSidecarUnavailableDoesNotLoseTheUpload(t *testing.T) {
	// A port nothing is listening on.
	processor := attachments.NewSidecarProcessor("http://127.0.0.1:1", 2*time.Second)

	source := encodePNG(t, drawTestImage(400, 300))
	_, err := processor.Process(context.Background(), bytes.NewReader(source), "image/png")

	if err == nil {
		t.Fatal("an unreachable sidecar reported success")
	}
	// Distinguishable from "this type has no preview", so an operator can tell
	// a broken sidecar from an ordinary PDF.
	if errors.Is(err, attachments.ErrNotThumbnailable) {
		t.Error("an unreachable sidecar was reported as an unpreviewable type")
	}
}

// TestSidecarRefusalIsNotAFailure checks that a sidecar declining a file is
// treated as "nothing to show" rather than as something being wrong.
func TestSidecarRefusalIsNotAFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(`{"reason":"malformed"}`))
	}))
	defer server.Close()

	processor := attachments.NewSidecarProcessor(server.URL, 5*time.Second)
	source := encodePNG(t, drawTestImage(200, 200))

	_, err := processor.Process(context.Background(), bytes.NewReader(source), "image/png")
	if !errors.Is(err, attachments.ErrNotThumbnailable) {
		t.Errorf("a refusal gave %v, expected ErrNotThumbnailable", err)
	}
}

// TestSidecarOutputIsVerified is the important one.
//
// A preview is served to browsers. Trusting another process's declared type is
// the same mistake as trusting a client's, so what comes back is decoded before
// it is accepted — a compromised sidecar must not be able to place arbitrary
// bytes in front of everyone reading a conversation.
func TestSidecarOutputIsVerified(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Claims to be a PNG, is a script.
		w.Write([]byte(`{
			"data": "PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==",
			"contentType": "image/png",
			"width": 100,
			"height": 100
		}`))
	}))
	defer server.Close()

	processor := attachments.NewSidecarProcessor(server.URL, 5*time.Second)
	source := encodePNG(t, drawTestImage(200, 200))

	_, err := processor.Process(context.Background(), bytes.NewReader(source), "image/png")
	if err == nil {
		t.Fatal("the sidecar returned a script claiming to be a PNG and it was accepted")
	}
	if !strings.Contains(err.Error(), "not an image") {
		t.Errorf("refused with %v, which does not look like the content check", err)
	}
}

// TestSidecarSkipsUnpreviewableTypesLocally checks that a PDF is not sent over
// the network only to be refused.
func TestSidecarSkipsUnpreviewableTypesLocally(t *testing.T) {
	var reached bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	processor := attachments.NewSidecarProcessor(server.URL, 5*time.Second)
	_, err := processor.Process(context.Background(),
		strings.NewReader("%PDF-1.7"), "application/pdf")

	if !errors.Is(err, attachments.ErrNotThumbnailable) {
		t.Errorf("gave %v, expected ErrNotThumbnailable", err)
	}
	if reached {
		t.Error("a PDF was sent to the sidecar to be refused; the round trip is wasted")
	}
}
