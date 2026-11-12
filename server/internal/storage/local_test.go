package storage_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/AnoRebel/tdc/server/internal/storage"
)

// The local store is the default, so it runs the whole conformance suite.
func TestLocal(t *testing.T) {
	RunConformance(t, func(t *testing.T) storage.Store {
		t.Helper()

		// A fresh directory per subtest, removed by the test framework, so no
		// test can see another's objects.
		store, err := storage.NewLocal(t.TempDir())
		if err != nil {
			t.Fatalf("opening the store: %v", err)
		}
		t.Cleanup(func() { store.Close() })

		return store
	})
}

// TestLocalRefusesTraversal pins the specific shape that got through an earlier
// version of the key check.
//
// filepath.Join cleans its result, so a check on the joined path cannot see a
// ".." that has already been resolved away: "nested/../../escaped" produced a
// path that looked clean and pointed elsewhere. The key itself is what must be
// checked, and these cases say so.
func TestLocalRefusesTraversal(t *testing.T) {
	root := t.TempDir()
	store, err := storage.NewLocal(root)
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	keys := []string{
		"..",
		"../escaped",
		"../../etc/passwd",
		"nested/../../escaped",
		"a/b/../../../outside",
		`..\escaped`,
		"/absolute",
		"",
	}

	ctx := context.Background()
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			body := []byte("must not be written")
			if err := store.Put(ctx, key, bytes.NewReader(body), int64(len(body)), "text/plain"); err == nil {
				t.Errorf("Put accepted %q", key)
			}
			if _, _, err := store.Get(ctx, key); err == nil {
				t.Errorf("Get accepted %q", key)
			}
			if err := store.Delete(ctx, key); err == nil {
				t.Errorf("Delete accepted %q", key)
			}
		})
	}

	// Nothing may have been created outside the root.
	if entries, err := os.ReadDir(filepath.Dir(root)); err == nil {
		for _, entry := range entries {
			if entry.Name() == "escaped" || entry.Name() == "outside" {
				t.Errorf("a traversal key created %q beside the root", entry.Name())
			}
		}
	}
}
