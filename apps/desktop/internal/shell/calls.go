package shell

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

/*
Calls in the desktop window.

WebView2 and WKWebView place calls as they come. WebKitGTK does not: its
WebRTC and media capture are switched off unless the application turns them
on, and turning them on takes effect for the next page, not the one already
loaded. So once the window's page is up the settings are switched on, and if
they were off the page is loaded once more — which happens once per launch,
before anybody has done anything on it. Whether that then works depends on
WebKitGTK having been built with WebRTC and on GStreamer's WebRTC plugins
being installed; CallSupport says which is missing, for the window to tell
somebody why they cannot call.
*/
func EnableCalls(window *application.WebviewWindow) {
	window.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
		application.InvokeAsync(func() {
			if enableWebRTC(window.NativeWindow()) == 1 {
				go window.Reload()
			}
		})
	})
}

// CallSupport tells the window what this machine lacks for calls, if anything.
type CallSupport struct{}

// NewCallSupport makes the service.
func NewCallSupport() *CallSupport { return &CallSupport{} }

/*
Missing names what this machine lacks for calls, or is empty when nothing is
known to be missing: "gstreamer-webrtc" where GStreamer's WebRTC or ICE plugin
cannot be found. The window adds what only it can see, such as a web view built
without WebRTC at all.
*/
func (CallSupport) Missing() string {
	return missingForCalls()
}
