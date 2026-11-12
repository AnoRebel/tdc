package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

/*
Saving a file made in the window — a call's recording — to disk.

SaveMedia streams a server file; this is for bytes the window already holds.
Not every platform's webview saves a download it is handed, so the window asks
here instead: where to save, with the system's dialog, then the bytes in
pieces, so a recording of an hour never has to cross the bridge in one go.
Written beside the destination and renamed into place when finished, as
SaveMedia does, so a save that stops part way leaves nothing that looks done.
*/

type localSave struct {
	file        *os.File
	destination string
}

var (
	localSavesMu sync.Mutex
	localSaves   = map[string]*localSave{}
)

// ErrNoLocalSave is a piece for a save that was never begun, or is finished.
var ErrNoLocalSave = errors.New("no such save in progress")

// BeginLocalSave asks where to save a file and opens it, returning a handle
// for its pieces, or "" when somebody cancelled.
func (s *Service) BeginLocalSave(filename string) (string, error) {
	s.mu.RLock()
	chooser := s.chooser
	s.mu.RUnlock()
	if chooser == nil {
		return "", ErrNoChooser
	}
	destination, err := chooser(filepath.Base(filename))
	if err != nil || destination == "" {
		return "", nil
	}
	partial, err := os.CreateTemp(filepath.Dir(destination), ".tdc-save-*")
	if err != nil {
		return "", fmt.Errorf("creating the file: %w", err)
	}
	raw := make([]byte, 16)
	_, _ = rand.Read(raw)
	handle := hex.EncodeToString(raw)
	localSavesMu.Lock()
	localSaves[handle] = &localSave{file: partial, destination: destination}
	localSavesMu.Unlock()
	return handle, nil
}

// WriteLocalSave appends a piece to a save in progress.
func (s *Service) WriteLocalSave(handle string, data []byte) error {
	localSavesMu.Lock()
	save := localSaves[handle]
	localSavesMu.Unlock()
	if save == nil {
		return ErrNoLocalSave
	}
	if _, err := save.file.Write(data); err != nil {
		s.AbandonLocalSave(handle)
		return fmt.Errorf("saving the file: %w", err)
	}
	return nil
}

// FinishLocalSave puts a finished save in place and returns where it is.
func (s *Service) FinishLocalSave(handle string) (string, error) {
	save := takeLocalSave(handle)
	if save == nil {
		return "", ErrNoLocalSave
	}
	if err := save.file.Close(); err != nil {
		_ = os.Remove(save.file.Name())
		return "", fmt.Errorf("saving the file: %w", err)
	}
	if err := os.Rename(save.file.Name(), save.destination); err != nil {
		_ = os.Remove(save.file.Name())
		return "", fmt.Errorf("saving the file: %w", err)
	}
	return save.destination, nil
}

// AbandonLocalSave drops a save in progress and what it had written.
func (s *Service) AbandonLocalSave(handle string) {
	if save := takeLocalSave(handle); save != nil {
		_ = save.file.Close()
		_ = os.Remove(save.file.Name())
	}
}

func takeLocalSave(handle string) *localSave {
	localSavesMu.Lock()
	defer localSavesMu.Unlock()
	save := localSaves[handle]
	delete(localSaves, handle)
	return save
}
