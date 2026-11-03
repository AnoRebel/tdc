//go:build linux && cgo && gtk3

package shell

/*
#cgo linux pkg-config: gtk+-3.0 webkit2gtk-4.1
#include <stdlib.h>
#include <gtk/gtk.h>
#include <webkit2/webkit2.h>

static WebKitWebView *tdcFindWebView(GtkWidget *widget) {
	if (widget == NULL) return NULL;
	if (WEBKIT_IS_WEB_VIEW(widget)) return WEBKIT_WEB_VIEW(widget);
	if (!GTK_IS_CONTAINER(widget)) return NULL;
	GList *children = gtk_container_get_children(GTK_CONTAINER(widget));
	WebKitWebView *found = NULL;
	for (GList *each = children; each != NULL && found == NULL; each = each->next) {
		found = tdcFindWebView(GTK_WIDGET(each->data));
	}
	g_list_free(children);
	return found;
}

static int tdcEnableWebRTC(void *window) {
	WebKitWebView *view = tdcFindWebView(GTK_WIDGET(window));
	if (view == NULL) return -1;
	WebKitSettings *settings = webkit_web_view_get_settings(view);
	int was = webkit_settings_get_enable_webrtc(settings) && webkit_settings_get_enable_media_stream(settings);
	webkit_settings_set_enable_media_stream(settings, TRUE);
	webkit_settings_set_enable_webrtc(settings, TRUE);
	// A camera and microphone that are WebKit's own, for demonstrating calls
	// on a machine whose real ones should not be used.
	if (getenv("TDC_MOCK_MEDIA") != NULL) webkit_settings_set_enable_mock_capture_devices(settings, TRUE);
	return was ? 0 : 1;
}
*/
import "C"

import "unsafe"

func enableWebRTC(window unsafe.Pointer) int {
	return int(C.tdcEnableWebRTC(window))
}
