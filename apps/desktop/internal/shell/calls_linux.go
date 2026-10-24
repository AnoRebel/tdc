//go:build linux

package shell

import (
	"os"
	"path/filepath"
	"strings"
)

/*
The GStreamer plugins WebKitGTK places calls with: webrtcbin, in the "bad"
set, and the ICE agent from libnice. Looked for where GStreamer looks — the
paths it is told, then the usual system directories of the distributions the
packages are made for.
*/
var callPlugins = []string{"libgstwebrtc.so", "libgstnice.so"}

func pluginDirectories() []string {
	var directories []string
	for _, variable := range []string{"GST_PLUGIN_PATH_1_0", "GST_PLUGIN_PATH", "GST_PLUGIN_SYSTEM_PATH_1_0", "GST_PLUGIN_SYSTEM_PATH"} {
		directories = append(directories, filepath.SplitList(os.Getenv(variable))...)
	}
	return append(directories,
		"/usr/lib/gstreamer-1.0", "/usr/lib64/gstreamer-1.0",
		"/usr/lib/x86_64-linux-gnu/gstreamer-1.0", "/usr/lib/aarch64-linux-gnu/gstreamer-1.0",
		"/usr/local/lib/gstreamer-1.0",
	)
}

func missingForCalls() string {
	return missingPlugins(pluginDirectories())
}

func missingPlugins(directories []string) string {
	for _, plugin := range callPlugins {
		found := false
		for _, directory := range directories {
			if strings.TrimSpace(directory) == "" {
				continue
			}
			if _, err := os.Stat(filepath.Join(directory, plugin)); err == nil {
				found = true
				break
			}
		}
		if !found {
			return "gstreamer-webrtc"
		}
	}
	return ""
}
