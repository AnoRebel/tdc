//go:build linux && cgo && !gtk3

package shell

/*
#cgo linux pkg-config: gtk4 webkitgtk-6.0
#include <gtk/gtk.h>
#include <webkit/webkit.h>

// The window's web view, found among its widgets.
static WebKitWebView *tdcFindWebView(GtkWidget *widget) {
	if (widget == NULL) return NULL;
	if (WEBKIT_IS_WEB_VIEW(widget)) return WEBKIT_WEB_VIEW(widget);
	for (GtkWidget *child = gtk_widget_get_first_child(widget); child != NULL; child = gtk_widget_get_next_sibling(child)) {
		WebKitWebView *found = tdcFindWebView(child);
		if (found != NULL) return found;
	}
	return NULL;
}

// Switches calls on, returning 1 if they were off, 0 if already on, -1 if no web view.
static int tdcEnableWebRTC(void *window) {
	WebKitWebView *view = tdcFindWebView(GTK_WIDGET(window));
	if (view == NULL) return -1;
	WebKitSettings *settings = webkit_web_view_get_settings(view);
	int was = webkit_settings_get_enable_webrtc(settings) && webkit_settings_get_enable_media_stream(settings);
	webkit_settings_set_enable_media_stream(settings, TRUE);
	webkit_settings_set_enable_webrtc(settings, TRUE);
	return was ? 0 : 1;
}
*/
import "C"

import "unsafe"

func enableWebRTC(window unsafe.Pointer) int {
	return int(C.tdcEnableWebRTC(window))
}
