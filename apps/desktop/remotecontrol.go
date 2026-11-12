package main

import (
	"html"

	"github.com/AnoRebel/tdc/apps/desktop/internal/remotecontrol"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// eventRemoteControl tells the window who controls the shared screen now.
const eventRemoteControl = "tdc:remote-control"

/*
remoteControlFor makes remote control for this application: input through the
platform's own way in, the displays as the application sees them, and — while
somebody holds control — a small window above every other on the sharer's
screen saying who it is and how to take control back, since the sharer may be
looking at anything but TDC by then.
*/
func remoteControlFor(app *application.App) *remotecontrol.Service {
	probe, open := remotecontrol.Platform()
	service := remotecontrol.New(probe, open, func() []remotecontrol.Screen {
		var screens []remotecontrol.Screen
		for _, screen := range app.Screen.GetAll() {
			screens = append(screens, remotecontrol.Screen{
				Logical:  remotecontrol.Rect{X: screen.Bounds.X, Y: screen.Bounds.Y, Width: screen.Bounds.Width, Height: screen.Bounds.Height},
				Physical: remotecontrol.Rect{X: screen.PhysicalBounds.X, Y: screen.PhysicalBounds.Y, Width: screen.PhysicalBounds.Width, Height: screen.PhysicalBounds.Height},
				Primary:  screen.IsPrimary,
			})
		}
		return screens
	})

	var indicator *application.WebviewWindow
	remotecontrol.OnChange(service, func(state remotecontrol.State) {
		app.Event.Emit(eventRemoteControl, state)
		// Off the lock the service holds while it says so.
		application.InvokeAsync(func() {
			if indicator != nil {
				indicator.Close()
				indicator = nil
			}
			if state.Controller == "" {
				return
			}
			indicator = app.Window.NewWithOptions(application.WebviewWindowOptions{
				Name:            "remote-control",
				Title:           state.Notice,
				Width:           460,
				Height:          52,
				AlwaysOnTop:     true,
				Frameless:       true,
				DisableResize:   true,
				InitialPosition: application.WindowXY,
				X:               24,
				Y:               24,
				HTML: `<!doctype html><meta charset="utf-8"><body style="margin:0;font:13px system-ui,sans-serif;` +
					`background:#b91c1c;color:#fff;display:flex;align-items:center;gap:8px;height:100vh;padding:0 14px;` +
					`box-sizing:border-box"><span style="width:8px;height:8px;border-radius:50%;background:#fff"></span>` +
					html.EscapeString(state.Notice) + `</body>`,
			})
		})
	})
	return service
}
