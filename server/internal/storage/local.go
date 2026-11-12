package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Local keeps objects as files under a directory.
//
// The default, and the reason a small deployment needs no object store at all.
// Keys are laid out as a shallow tree rather than one flat directory, because a
// directory holding a hundred thousand entries is slow to walk on most
// filesystems and unpleasant for an operator to go through by hand.
type Local struct {
	root string
}

// NewLocal opens a store rooted at dir, creating it if necessary.
func NewLocal(dir string) (*Local, error) {
	if dir == "" {
		return nil, errors.New("storage: local storage needs a directory")
	}

	absolute, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("storage: resolving %q: %w", dir, err)
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("storage: creating %q: %w", absolute, err)
	}

	return &Local{root: absolute}, nil
}

// validKey reports whether a key is safe to turn into a path.
//
// Checked on the key itself, before any joining. filepath.Join cleans its
// result, so a ".." in the key is resolved away before a check on the joined
// path could ever see it: "nested/../../escaped" becomes a path that looks
// perfectly clean and points somewhere else entirely. The only reliable place
// to refuse traversal is here, on the text the caller supplied.
func validKey(key string) error {
	switch {
	case key == "":
		return errors.New("storage: empty key")
	case strings.ContainsRune(key, 0):
		return errors.New("storage: key contains a null byte")
	case strings.HasPrefix(key, "/"), strings.HasPrefix(key, `\`):
		return fmt.Errorf("storage: key %q is absolute", key)
	case filepath.IsAbs(key), filepath.VolumeName(key) != "":
		return fmt.Errorf("storage: key %q is absolute", key)
	}

	// Backslashes are separators on Windows and ordinary characters elsewhere,
	// so a key is rejected if either spelling contains a traversal segment.
	for _, segment := range strings.FieldsFunc(key, func(r rune) bool {
		return r == '/' || r == '\\'
	}) {
		if segment == ".." {
			return fmt.Errorf("storage: key %q escapes the storage root", key)
		}
	}

	return nil
}

// pathFor maps a key to a file path under the root.
func (l *Local) pathFor(key string) (string, error) {
	if err := validKey(key); err != nil {
		return "", err
	}

	// A shallow fan-out on the first characters, which are random for the
	// content-addressed keys used here and so spread evenly.
	prefix := key
	if len(prefix) > 2 {
		prefix = prefix[:2]
	}

	path := filepath.Join(l.root, prefix, key)

	// Belt and braces: the key has already been checked, and this asserts that
	// the check held rather than relying on it alone.
	if !strings.HasPrefix(path, l.root+string(os.PathSeparator)) {
		return "", fmt.Errorf("storage: key %q escapes the storage root", key)
	}

	return path, nil
}

// sidecarPath names the file holding what the filesystem cannot record itself.
//
// Kept beside the object rather than only in the database, so that a copied
// storage directory still describes its own contents.
func sidecarPath(objectPath string) string { return objectPath + ".meta" }

type sidecar struct {
	ContentType string `json:"contentType"`
}

func (l *Local) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	path, err := l.pathFor(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("storage: creating directory for %q: %w", key, err)
	}

	// Written to a temporary file and renamed, so a crash or a full disk leaves
	// no half-written object for a reader to find. Rename within a directory is
	// atomic on every filesystem this runs on.
	temporary, err := os.CreateTemp(filepath.Dir(path), ".upload-*")
	if err != nil {
		return fmt.Errorf("storage: creating temporary file for %q: %w", key, err)
	}
	temporaryName := temporary.Name()
	defer func() {
		temporary.Close()
		os.Remove(temporaryName)
	}()

	// Bounded by the declared size: a body claiming one length and sending more
	// must not be able to fill the disk.
	written, err := io.Copy(temporary, io.LimitReader(r, size))
	if err != nil {
		return fmt.Errorf("storage: writing %q: %w", key, err)
	}
	if written != size {
		return fmt.Errorf("storage: %q declared %d bytes but supplied %d", key, size, written)
	}

	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("storage: flushing %q: %w", key, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("storage: closing %q: %w", key, err)
	}
	if err := os.Chmod(temporaryName, 0o600); err != nil {
		return fmt.Errorf("storage: setting permissions on %q: %w", key, err)
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return fmt.Errorf("storage: storing %q: %w", key, err)
	}

	meta, err := json.Marshal(sidecar{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("storage: encoding metadata for %q: %w", key, err)
	}
	if err := os.WriteFile(sidecarPath(path), meta, 0o600); err != nil {
		return fmt.Errorf("storage: writing metadata for %q: %w", key, err)
	}

	return nil
}

func (l *Local) Get(ctx context.Context, key string) (io.ReadCloser, *Object, error) {
	path, err := l.pathFor(key)
	if err != nil {
		return nil, nil, err
	}

	object, err := l.statPath(path, key)
	if err != nil {
		return nil, nil, err
	}

	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, fmt.Errorf("storage: opening %q: %w", key, err)
	}

	return file, object, nil
}

func (l *Local) Stat(ctx context.Context, key string) (*Object, error) {
	path, err := l.pathFor(key)
	if err != nil {
		return nil, err
	}
	return l.statPath(path, key)
}

func (l *Local) statPath(path, key string) (*Object, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("storage: reading %q: %w", key, err)
	}

	object := &Object{
		Key:        key,
		Size:       info.Size(),
		ModifiedAt: info.ModTime(),
	}

	// A missing or unreadable sidecar leaves the type empty rather than failing
	// the read: the bytes are still there, and the caller decides what to do
	// without a declared type.
	if raw, err := os.ReadFile(sidecarPath(path)); err == nil {
		var meta sidecar
		if json.Unmarshal(raw, &meta) == nil {
			object.ContentType = meta.ContentType
		}
	}

	return object, nil
}

func (l *Local) Delete(ctx context.Context, key string) error {
	path, err := l.pathFor(key)
	if err != nil {
		return err
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("storage: deleting %q: %w", key, err)
	}
	if err := os.Remove(sidecarPath(path)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("storage: deleting metadata for %q: %w", key, err)
	}

	return nil
}

// PresignGet returns "", because a directory on disk has no URL to hand out.
//
// The server streams these downloads itself, which is why the interface permits
// an empty answer rather than requiring every store to invent one.
func (l *Local) PresignGet(context.Context, string, time.Duration, string) (string, error) {
	return "", nil
}

func (l *Local) Close() error { return nil }
