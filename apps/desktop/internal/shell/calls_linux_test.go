//go:build linux

package shell

import (
	"os"
	"path/filepath"
	"testing"
)

// A machine without GStreamer's WebRTC or ICE plugin is told so, and one
// with both is not.
func TestCallsSayWhichPluginIsMissing(t *testing.T) {
	dir := t.TempDir()
	if got := missingPlugins([]string{dir}); got != "gstreamer-webrtc" {
		t.Errorf("with no plugins: %q", got)
	}
	_ = os.WriteFile(filepath.Join(dir, "libgstwebrtc.so"), nil, 0o600)
	if got := missingPlugins([]string{dir}); got != "gstreamer-webrtc" {
		t.Errorf("without the ICE plugin: %q", got)
	}
	_ = os.WriteFile(filepath.Join(dir, "libgstnice.so"), nil, 0o600)
	if got := missingPlugins([]string{"", dir}); got != "" {
		t.Errorf("with both: %q", got)
	}
}
