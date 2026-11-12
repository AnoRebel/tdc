package storage_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/AnoRebel/tdc/server/internal/storage"
)

// RunConformance exercises the behaviour every Store must share.
//
// Three implementations sit behind one interface, and the point of the
// interface is that callers cannot tell them apart. That only holds if the
// implementations are held to the same tests, so each one runs this suite
// rather than writing its own — a divergence then fails here instead of
// surfacing as an attachment that works on one operator's server and not
// another's.
func RunConformance(t *testing.T, open func(t *testing.T) storage.Store) {
	t.Helper()

	t.Run("round trips content and type", func(t *testing.T) {
		store := open(t)
		ctx := context.Background()

		body := []byte("habari ya asubuhi")
		if err := store.Put(ctx, "greeting.txt", bytes.NewReader(body), int64(len(body)), "text/plain"); err != nil {
			t.Fatalf("put: %v", err)
		}

		reader, object, err := store.Get(ctx, "greeting.txt")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		defer reader.Close()

		read, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if !bytes.Equal(read, body) {
			t.Errorf("read back %q, stored %q", read, body)
		}
		if object.Size != int64(len(body)) {
			t.Errorf("size is %d, stored %d bytes", object.Size, len(body))
		}
		if object.ContentType != "text/plain" {
			t.Errorf("content type is %q, stored %q", object.ContentType, "text/plain")
		}
	})

	t.Run("reports a missing object as not found", func(t *testing.T) {
		store := open(t)
		ctx := context.Background()

		if _, _, err := store.Get(ctx, "never-written"); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("get of a missing object gave %v, want ErrNotFound", err)
		}
		if _, err := store.Stat(ctx, "never-written"); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("stat of a missing object gave %v, want ErrNotFound", err)
		}
	})

	t.Run("stat does not require reading the object", func(t *testing.T) {
		store := open(t)
		ctx := context.Background()

		body := []byte("some bytes")
		if err := store.Put(ctx, "stat-me", bytes.NewReader(body), int64(len(body)), "application/octet-stream"); err != nil {
			t.Fatalf("put: %v", err)
		}

		object, err := store.Stat(ctx, "stat-me")
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if object.Key != "stat-me" {
			t.Errorf("key is %q, asked for %q", object.Key, "stat-me")
		}
		if object.Size != int64(len(body)) {
			t.Errorf("size is %d, stored %d bytes", object.Size, len(body))
		}
	})

	t.Run("replaces an existing object", func(t *testing.T) {
		store := open(t)
		ctx := context.Background()

		first := []byte("first version")
		second := []byte("the second, longer version")

		if err := store.Put(ctx, "replaced", bytes.NewReader(first), int64(len(first)), "text/plain"); err != nil {
			t.Fatalf("first put: %v", err)
		}
		if err := store.Put(ctx, "replaced", bytes.NewReader(second), int64(len(second)), "text/plain"); err != nil {
			t.Fatalf("second put: %v", err)
		}

		reader, _, err := store.Get(ctx, "replaced")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		defer reader.Close()

		read, _ := io.ReadAll(reader)
		if !bytes.Equal(read, second) {
			t.Errorf("read back %q, expected the replacement %q", read, second)
		}
	})

	t.Run("deletes, and deleting again is not an error", func(t *testing.T) {
		store := open(t)
		ctx := context.Background()

		body := []byte("temporary")
		if err := store.Put(ctx, "doomed", bytes.NewReader(body), int64(len(body)), "text/plain"); err != nil {
			t.Fatalf("put: %v", err)
		}
		if err := store.Delete(ctx, "doomed"); err != nil {
			t.Fatalf("delete: %v", err)
		}
		if _, _, err := store.Get(ctx, "doomed"); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("after delete, get gave %v, want ErrNotFound", err)
		}

		// The caller wanted it gone, and it is gone.
		if err := store.Delete(ctx, "doomed"); err != nil {
			t.Errorf("deleting an already-deleted object failed: %v", err)
		}
	})

	t.Run("stores an empty object", func(t *testing.T) {
		store := open(t)
		ctx := context.Background()

		if err := store.Put(ctx, "empty", bytes.NewReader(nil), 0, "application/octet-stream"); err != nil {
			t.Fatalf("put: %v", err)
		}

		object, err := store.Stat(ctx, "empty")
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if object.Size != 0 {
			t.Errorf("size is %d, stored nothing", object.Size)
		}
	})

	t.Run("refuses a body longer than it declared", func(t *testing.T) {
		store := open(t)
		ctx := context.Background()

		// A client that declares ten bytes and sends far more must not be able
		// to write them: the declared size is what the operator's limits were
		// checked against.
		body := strings.Repeat("x", 5_000)
		err := store.Put(ctx, "liar", strings.NewReader(body), 10, "text/plain")

		if err == nil {
			object, statErr := store.Stat(ctx, "liar")
			if statErr != nil {
				t.Fatalf("stat after put: %v", statErr)
			}
			if object.Size > 10 {
				t.Errorf("stored %d bytes for a declared 10", object.Size)
			}
			return
		}
	})

	t.Run("refuses a key that escapes the store", func(t *testing.T) {
		store := open(t)
		ctx := context.Background()

		for _, key := range []string{
			"../escaped",
			"../../etc/passwd",
			"nested/../../escaped",
			"",
		} {
			body := []byte("should not be written")
			if err := store.Put(ctx, key, bytes.NewReader(body), int64(len(body)), "text/plain"); err == nil {
				t.Errorf("key %q was accepted; it must be refused", key)
			}
		}
	})

	t.Run("keeps objects separate", func(t *testing.T) {
		store := open(t)
		ctx := context.Background()

		for _, item := range []struct{ key, body string }{
			{"alpha", "first"},
			{"beta", "second"},
			{"nested/gamma", "third"},
		} {
			if err := store.Put(ctx, item.key, strings.NewReader(item.body), int64(len(item.body)), "text/plain"); err != nil {
				t.Fatalf("put %q: %v", item.key, err)
			}
		}

		for _, item := range []struct{ key, body string }{
			{"alpha", "first"},
			{"beta", "second"},
			{"nested/gamma", "third"},
		} {
			reader, _, err := store.Get(ctx, item.key)
			if err != nil {
				t.Fatalf("get %q: %v", item.key, err)
			}
			read, _ := io.ReadAll(reader)
			reader.Close()

			if string(read) != item.body {
				t.Errorf("%q holds %q, stored %q", item.key, read, item.body)
			}
		}
	})

	t.Run("presigning either returns a usable URL or declines", func(t *testing.T) {
		store := open(t)
		ctx := context.Background()

		body := []byte("downloadable")
		if err := store.Put(ctx, "download-me", bytes.NewReader(body), int64(len(body)), "text/plain"); err != nil {
			t.Fatalf("put: %v", err)
		}

		url, err := store.PresignGet(ctx, "download-me", 5*time.Minute, "download-me.txt")
		if err != nil {
			t.Fatalf("presign: %v", err)
		}

		// An empty URL is a valid answer: it means this store cannot hand the
		// transfer to anyone else, and the server streams the download itself.
		if url != "" && !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			t.Errorf("presigned URL %q is neither empty nor an HTTP URL", url)
		}
	})
}
