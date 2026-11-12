package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

/*
Saving a file to disk, however large.

FetchMedia hands the bytes to the webview, which is right for a picture to show
and wrong for a file to keep: a 2 GB recording would be held in memory twice
over, and anything past mediaLimit was refused outright. Saving streams from
the server straight into the file somebody chose, so its size costs nothing but
disk.
*/

// SaveChooser asks where to save a file, returning "" when somebody cancels.
type SaveChooser func(suggested string) (string, error)

// ErrNoChooser reports that nothing was wired to ask where to save.
var ErrNoChooser = errors.New("this client cannot ask where to save a file")

/*
UseSaveChooser wires how this client asks where to save. A function rather
than a method for the reason UseOpener is: it must not be offered to the
webview.
*/
func UseSaveChooser(service *Service, chooser SaveChooser) {
	service.mu.Lock()
	defer service.mu.Unlock()
	service.chooser = chooser
}

/*
SaveMedia asks where to save a server file and streams it there.

Returns where it was saved, or "" when somebody cancelled. Written to a
temporary file beside the destination and renamed into place at the end, so a
download that fails part way leaves nothing that looks like the finished file.
*/
func (s *Service) SaveMedia(ctx context.Context, path, filename string) (string, error) {
	s.mu.RLock()
	base, client, chooser := s.current, s.clients[s.current], s.chooser
	s.mu.RUnlock()

	if base == "" || client == nil {
		return "", ErrNoServer
	}
	if chooser == nil {
		return "", ErrNoChooser
	}

	destination, err := chooser(filepath.Base(filename))
	if err != nil || destination == "" {
		// A cancelled dialog is somebody changing their mind, not a failure.
		return "", nil
	}

	url := base + "/" + strings.TrimPrefix(path, "/")
	outgoing, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("building the request: %w", err)
	}
	incoming, err := client.Do(outgoing)
	if err != nil {
		return "", fmt.Errorf("reaching %s: %w", base, err)
	}
	defer incoming.Body.Close()
	if incoming.StatusCode < 200 || incoming.StatusCode >= 300 {
		return "", fmt.Errorf("the server answered %d", incoming.StatusCode)
	}

	partial, err := os.CreateTemp(filepath.Dir(destination), ".tdc-download-*")
	if err != nil {
		return "", fmt.Errorf("creating the file: %w", err)
	}
	_, copyErr := io.Copy(partial, incoming.Body)
	closeErr := partial.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(partial.Name())
		return "", fmt.Errorf("saving the file: %w", errors.Join(copyErr, closeErr))
	}
	if err := os.Rename(partial.Name(), destination); err != nil {
		_ = os.Remove(partial.Name())
		return "", fmt.Errorf("saving the file: %w", err)
	}
	return destination, nil
}
