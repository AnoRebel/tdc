// Command tdc is the TDC desktop client.
//
// It is a webview around the same interface the browser shows, plus the things
// a browser cannot do: a system tray, credentials in the operating system's
// keychain, notifications that survive the window being closed. Everything a
// person reads and types comes from the shared Nuxt layer, so the two clients
// cannot drift apart by accident.
package main

import (
	"embed"
	"log"

	"github.com/AnoRebel/tdc/apps/desktop/internal/api"
	"github.com/AnoRebel/tdc/apps/desktop/internal/shell"
	"github.com/wailsapp/wails/v3/pkg/application"
)

/*
The built frontend, compiled into the binary.

`all:` includes files whose names begin with an underscore or a dot, which
matters here: Nuxt writes its chunks to _nuxt and its translations to
_locales, and without it the application ships an index.html and nothing it
refers to. The failure is a window that opens onto a blank page — a build
that succeeds, and an application that does not run.

Measured rather than assumed: dropping the prefix builds a binary 303 KB
smaller, which is the chunks and the translations going missing. embed_test.go
holds this to it, so the prefix cannot be lost in an edit without something
saying so.
*/
//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := application.New(application.Options{
		Name:        "TDC",
		Description: "Chat, project tracking, and a directory connecting developers with the people who need them.",
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		/*
			What the window can call into.

			One service, holding the one thing that genuinely differs between
			this client and the browser: how a request reaches a server. The
			bindings for it are generated as TypeScript, so a change to the Go
			signature stops the frontend compiling rather than failing in front
			of somebody.
		*/
		Services: []application.Service{
			application.NewService(api.New()),
		},
		Mac: application.MacOptions{
			// Closing the last window quits, which is what somebody means by
			// closing an application — until the tray exists and offers to
			// keep running, which 9.14 decides.
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	/*
		The menu, after the application exists because its items emit events
		through it. Every item does something: an item that is there to look
		complete teaches people the menu is not worth opening.
	*/
	app.Menu.SetApplicationMenu(shell.NewMenu(app))

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "TDC",
		/*
			Large enough for a conversation beside a channel list, and small
			enough to sit on a laptop screen beside something else. Window
			position and size are restored from a previous session in 9.15;
			until then this is where it opens.
		*/
		Width:  1100,
		Height: 720,
		// Below which the sidebar and a message can no longer both be read.
		MinWidth:  640,
		MinHeight: 480,
		/*
			The application's own dark ground, so the window does not flash
			white before the first paint. It matches --background in the
			shared stylesheet; a mismatch here shows as a flicker on every
			launch.
		*/
		BackgroundColour: application.NewRGB(20, 22, 28),
		URL:              "/",
	})

	if err := app.Run(); err != nil {
		log.Fatalf("tdc: %v", err)
	}
}
