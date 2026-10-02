//go:build !linux

package shell

import "io/fs"

// InstallTrayIcon does nothing outside Linux: macOS and Windows draw the tray
// icon from the picture the application supplies and never look it up by name.
func InstallTrayIcon(fs.FS) error { return nil }
