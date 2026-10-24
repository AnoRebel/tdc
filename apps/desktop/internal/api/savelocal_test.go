package api

import (
	"os"
	"path/filepath"
	"testing"
)

// A file saved in pieces lands whole where somebody chose, and nothing is
// left there by a save abandoned part way.
func TestALocalSaveLandsWholeOrNotAtAll(t *testing.T) {
	dir := t.TempDir()
	service := New()
	UseSaveChooser(service, func(suggested string) (string, error) { return filepath.Join(dir, suggested), nil })

	handle, err := service.BeginLocalSave("call.webm")
	if err != nil || handle == "" {
		t.Fatalf("beginning: %q %v", handle, err)
	}
	for _, piece := range []string{"one ", "two"} {
		if err := service.WriteLocalSave(handle, []byte(piece)); err != nil {
			t.Fatal(err)
		}
	}
	where, err := service.FinishLocalSave(handle)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(where); string(got) != "one two" {
		t.Errorf("saved %q", got)
	}

	abandoned, _ := service.BeginLocalSave("other.webm")
	_ = service.WriteLocalSave(abandoned, []byte("half"))
	service.AbandonLocalSave(abandoned)
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("after abandoning, the folder holds %d files, want 1", len(entries))
	}
	if err := service.WriteLocalSave(abandoned, []byte("more")); err == nil {
		t.Error("a piece for an abandoned save was taken")
	}
}
