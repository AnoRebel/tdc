//go:build linux

package shell

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

/*
Putting the tray's icon where panels that look it up by name will find it.

Wails publishes the tray icon as a picture, which most panels draw. Some —
waybar among them — instead resolve it by name from the icon theme, using the
StatusNotifierItem's Id, and show a placeholder when nothing by that name is
installed. The Id is TrayName, so the icon has to exist as TDC in the theme.

The deb and rpm install it under /usr/share/icons. An AppImage, a tarball or a
development build installs nothing, so the application puts it in the
person's own icon theme when it starts — and only there, and only what is
missing or out of date: a packaged install that already provides the same
icon is left alone.
*/

/*
InstallTrayIcon makes the tray icon resolvable by name in the icon theme.

icons is a hicolor tree — "<size>/apps/TDC.png", "scalable/apps/TDC.svg" —
whose files are written beneath $XDG_DATA_HOME/icons/hicolor wherever the
search path does not already hold an identical copy. Failure is reported and
not fatal: the tray still works on every panel that draws the picture.
*/
func InstallTrayIcon(icons fs.FS) error {
	home := dataHome()
	if home == "" {
		return errors.New("no data directory to install the tray icon into")
	}
	target := filepath.Join(home, "icons", "hicolor")

	wrote := false
	err := fs.WalkDir(icons, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}

		data, err := fs.ReadFile(icons, path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}

		if resolvable(path, data) {
			return nil
		}

		if err := writeAtomically(filepath.Join(target, filepath.FromSlash(path)), data); err != nil {
			return err
		}
		wrote = true
		return nil
	})
	if err != nil {
		return err
	}

	/*
		Touched so a stale icon cache is not believed.

		Where the person's hicolor directory carries an icon-theme.cache, GTK
		trusts it only while it is at least as new as the theme directory
		itself. Writing into a size subdirectory does not change the theme
		directory's time, so without this a cache from before would keep
		reporting the icon missing.
	*/
	if wrote {
		now := time.Now()
		if err := os.Chtimes(target, now, now); err != nil {
			return fmt.Errorf("refresh %s: %w", target, err)
		}
	}
	return nil
}

// dataHome is $XDG_DATA_HOME, or its documented default.
func dataHome() string {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return dir
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "share")
	}
	return ""
}

// dataDirs is the icon search path in the order desktops consult it: the
// person's own directory first, then $XDG_DATA_DIRS or its default.
func dataDirs() []string {
	dirs := []string{}
	if home := dataHome(); home != "" {
		dirs = append(dirs, home)
	}

	system := os.Getenv("XDG_DATA_DIRS")
	if system == "" {
		system = "/usr/local/share:/usr/share"
	}
	for _, dir := range strings.Split(system, ":") {
		if dir != "" {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

/*
resolvable reports whether the search path already holds this exact icon.

The first copy found is the one a lookup would use, so that is the one
compared: an identical file further down the path does not help if a
different one shadows it.
*/
func resolvable(path string, data []byte) bool {
	for _, dir := range dataDirs() {
		existing, err := os.ReadFile(filepath.Join(dir, "icons", "hicolor", filepath.FromSlash(path)))
		if err != nil {
			continue
		}
		return bytes.Equal(existing, data)
	}
	return false
}

// writeAtomically writes through a temporary file and a rename, so a panel
// reading the icon mid-write sees the old one or the new one, never half.
func writeAtomically(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}

	temporary, err := os.CreateTemp(filepath.Dir(path), ".tdc-icon-*")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	defer os.Remove(temporary.Name())

	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	if err := os.Rename(temporary.Name(), path); err != nil {
		return fmt.Errorf("install %s: %w", path, err)
	}
	return nil
}
