/*
Command tdc-media generates image previews in a container of its own.

Optional. The server does this work itself by default, and that is safe: image
decoding is pure Go, the binary builds with CGO disabled, and a decompression
bomb is refused from its header before a pixel is allocated. There is no C
image library here to be exploited, which is what a sidecar was originally for.

What isolation still buys is what happens when a decoder is wrong anyway. A
panic or a runaway allocation in this process kills a container that holds
nothing and serves nobody, rather than the process carrying everyone's
conversation. An operator running a large or public server may want that; one
running a server for thirty people should not have to.

It is deliberately incapable of much. It makes no outbound connections, holds
no state, needs no filesystem, and knows nothing about rooms, people or
permissions — it is handed bytes and returns a smaller picture.
*/
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/AnoRebel/tdc/server/internal/attachments"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "tdc-media:", err)
		os.Exit(1)
	}
}

func run() error {
	address := flag.String("address", ":8081", "address to listen on")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	mux := http.NewServeMux()
	mux.HandleFunc("POST /thumbnail", handleThumbnail(log))
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	server := &http.Server{
		Addr:    *address,
		Handler: mux,
		// A slow or stalled upload must not hold a worker. These are shorter
		// than the server's own, because the server is talking to this over a
		// local network rather than to a phone on mobile data.
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       90 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Shut down on a signal rather than being killed, so a request in flight
	// finishes rather than a preview being lost mid-generation.
	stopping := make(chan os.Signal, 1)
	signal.Notify(stopping, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-stopping
		log.Info("shutting down")
		server.Close()
	}()

	log.Info("media processor listening", "address", *address)

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// maxUploadBytes bounds what this will read from one request.
//
// The server has already applied the operator's own size limit before sending
// anything here; this is the second, independent bound that stops a confused or
// compromised caller streaming without end.
const maxUploadBytes = 64 << 20

type thumbnailResponse struct {
	Data        []byte `json:"data"`
	ContentType string `json:"contentType"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Reason      string `json:"reason,omitempty"`
}

func handleThumbnail(log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		contentType, _, _ := strings.Cut(r.Header.Get("Content-Type"), ";")
		contentType = strings.ToLower(strings.TrimSpace(contentType))

		if !attachments.CanThumbnail(contentType) {
			// 415 rather than an error: there is nothing wrong, this type
			// simply has no preview to give.
			respond(w, http.StatusUnsupportedMediaType,
				thumbnailResponse{Reason: "no preview for this type"})
			return
		}

		body := io.LimitReader(r.Body, maxUploadBytes)
		defer r.Body.Close()

		// Recovered rather than allowed to crash the process. A decoder panic
		// on a malformed file is exactly the case this container exists to
		// contain, and one bad upload must not take the others down with it.
		thumbnail, err := generateSafely(body, contentType)
		if err != nil {
			if errors.Is(err, attachments.ErrNotThumbnailable) ||
				errors.Is(err, attachments.ErrSourceTooLarge) {
				respond(w, http.StatusUnprocessableEntity,
					thumbnailResponse{Reason: err.Error()})
				return
			}

			log.Debug("cannot generate a preview", "type", contentType, "error", err)
			respond(w, http.StatusUnprocessableEntity,
				thumbnailResponse{Reason: "the image could not be processed"})
			return
		}

		respond(w, http.StatusOK, thumbnailResponse{
			Data:        thumbnail.Data,
			ContentType: thumbnail.ContentType,
			Width:       thumbnail.Width,
			Height:      thumbnail.Height,
		})
	}
}

// generateSafely turns a panic into an error.
//
// The whole point of running this in its own container is that a decoder bug
// takes down something disposable — but taking down the container on every
// malformed file would make it useless. Recovering here means one bad upload
// fails and the next is still served.
func generateSafely(body io.Reader, contentType string) (thumbnail *attachments.Thumbnail, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("the image decoder failed: %v", recovered)
		}
	}()

	return attachments.GenerateThumbnail(body, contentType)
}

func respond(w http.ResponseWriter, status int, body thumbnailResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
