//go:build linux

package shell

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// theme is a small icon tree standing in for the embedded one.
var theme = fstest.MapFS{
	"22x22/apps/TDC.png":    {Data: []byte("png-22")},
	"scalable/apps/TDC.svg": {Data: []byte("<svg/>")},
}

// sandbox points the icon search path at temporary directories, so a test can
// neither read nor write the real icon theme of whoever runs it.
func sandbox(t *testing.T) (home, system string) {
	t.Helper()
	home, system = t.TempDir(), t.TempDir()
	t.Setenv("XDG_DATA_HOME", home)
	t.Setenv("XDG_DATA_DIRS", system)
	return home, system
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

/*
With nothing installed, the icon lands in the person's own theme by the name
the tray announces — which is the AppImage and tarball case, where nothing
else would put it there.
*/
func TestTheTrayIconIsInstalledWhereNothingProvidesIt(t *testing.T) {
	home, _ := sandbox(t)

	if err := InstallTrayIcon(theme); err != nil {
		t.Fatalf("InstallTrayIcon: %v", err)
	}

	for path, file := range theme {
		installed := filepath.Join(home, "icons", "hicolor", path)
		if got := read(t, installed); got != string(file.Data) {
			t.Errorf("%s holds %q, want %q", installed, got, file.Data)
		}
	}
}

/*
A package that already installed the same icon is left alone.

Writing a second copy into the person's home would be clutter at best, and a
copy that outlived an upgrade of the package would shadow the newer one.
*/
func TestAPackagedIconIsNotDuplicated(t *testing.T) {
	home, system := sandbox(t)

	for path, file := range theme {
		packaged := filepath.Join(system, "icons", "hicolor", path)
		if err := os.MkdirAll(filepath.Dir(packaged), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(packaged, file.Data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := InstallTrayIcon(theme); err != nil {
		t.Fatalf("InstallTrayIcon: %v", err)
	}

	if _, err := os.Stat(filepath.Join(home, "icons")); !os.IsNotExist(err) {
		t.Fatalf("an icon the package already provides was copied into the home directory too")
	}
}

/*
An outdated copy in the person's theme is replaced.

It is the first one a lookup finds, so leaving an old icon there after the
application changed its own would show the old one indefinitely.
*/
func TestAnOutdatedIconIsReplaced(t *testing.T) {
	home, _ := sandbox(t)

	stale := filepath.Join(home, "icons", "hicolor", "22x22", "apps", "TDC.png")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("an older icon"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := InstallTrayIcon(theme); err != nil {
		t.Fatalf("InstallTrayIcon: %v", err)
	}

	if got := read(t, stale); got != "png-22" {
		t.Fatalf("the outdated icon was left in place: %q", got)
	}
}
