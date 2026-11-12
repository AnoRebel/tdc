// Package storage keeps the bytes of uploaded files.
//
// Where those bytes live is an operator's choice, not the application's: a
// community running one server on one machine should not have to stand up an
// object store, and one running several should not be forced to share a
// filesystem between them. Everything above this package addresses a file by
// its key and never learns which it is talking to.
package storage

import (
	"context"
	"errors"
	"io"
	"time"
)

// ErrNotFound is returned when no object exists at a key.
//
// Callers distinguish this from a transport failure, because the two mean
// different things to the person waiting: one is a file that is gone, the
// other is a file that may still be there.
var ErrNotFound = errors.New("storage: object not found")

// Object is what a store knows about a stored file without reading it.
type Object struct {
	Key string
	// Size in bytes, as stored.
	Size int64
	// ContentType as detected at upload, never as the client declared it.
	ContentType string
	ModifiedAt  time.Time
}

// Store keeps and returns file contents.
//
// Deliberately small. Attachments need to be written once, read back, handed
// to a browser, and eventually removed; anything richer would be a feature of
// one backend leaking into the interface and out into callers.
type Store interface {
	// Put stores size bytes read from r at key, replacing anything already
	// there. contentType is the detected type, which the store records so that
	// it can be returned on read without re-detecting.
	//
	// size must be accurate: object stores require the length up front, so a
	// caller that does not know it must buffer or spool to disk first.
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error

	// Get opens the object at key for reading. The caller closes it.
	Get(ctx context.Context, key string) (io.ReadCloser, *Object, error)

	// Stat returns what is known about the object without reading it.
	Stat(ctx context.Context, key string) (*Object, error)

	// Delete removes the object at key. Deleting a key that does not exist is
	// not an error: the caller wanted it gone, and it is gone.
	Delete(ctx context.Context, key string) error

	// PresignGet returns a URL a browser can follow directly to download the
	// object, valid for ttl, or "" when this store cannot issue one.
	//
	// An empty return is not a failure. The local filesystem has no URL to
	// give, so the server streams those downloads itself; a store that can
	// hand the transfer to the object store instead says so here, and the
	// server redirects. Callers must handle both.
	PresignGet(ctx context.Context, key string, ttl time.Duration, downloadName string) (string, error)

	// Close releases whatever the store holds open.
	Close() error
}
