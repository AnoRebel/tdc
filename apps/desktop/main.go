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
	"github.com/AnoRebel/tdc/apps/desktop/internal/notify"
	"github.com/AnoRebel/tdc/apps/desktop/internal/shell"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
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

/*
The tray icon, in one colour.

A tray sits on a panel whose colour this application cannot read — light on
some desktops, dark on others. A single-colour icon the desktop recolours is
the only thing that looks right on both, which is what a template icon means.
*/
//go:embed build/icons/tray-32.png
var trayIcon []byte

func main() {
	/*
		Declared before the options because the second-instance callback
		refers to it, and assigned below once the application exists.
	*/
	var window application.Window

	/*
		System notifications, built before the application so the response
		handler is attached before any could arrive.
	*/
	notifier := notify.New(notifications.New())

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
			/*
				The platform's own notifications, and this application's use of
				them. Both are registered: the first is what Wails needs to
				start the service, the second is the small surface the webview
				may call.
			*/
			application.NewService(notifications.New()),
			application.NewService(notify.NewService(notifier)),
		},
		/*
			One instance, because this application is a place rather than a
			document.

			Launching it again — from a launcher, a notification, a desktop
			file — means "show me TDC", not "give me a second copy". Two copies
			would hold two sessions, two trays and two sets of notifications
			for the same person, and the unread counts would disagree.

			The second launch raises the first's window and exits, so pressing
			the launcher icon while it is minimised does what somebody meant.
		*/
		SingleInstance: &application.SingleInstanceOptions{
			// The application id, so the lock is this application's rather
			// than anything else that picked the same word.
			UniqueID: "net.anorebel.tdc",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				if window != nil {
					window.Show()
					window.Focus()
				}
			},
		},
		Linux: application.LinuxOptions{
			/*
				What the window manager calls this program.

				Set so a desktop environment can match the running window to
				the .desktop entry that launched it — without which the
				application appears as a second, nameless icon and pinning it
				to a dock pins something that never lights up.

				The GTK application id is org.wails.<Name>, which Wails builds
				itself and does not expose. So this and StartupWMClass are both
				set to what the toolkit actually produces; when Wails allows
				the id to be configured, all three become net.anorebel.tdc
				together.
			*/
			ProgramName: "net.anorebel.tdc",
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

	window = app.Window.NewWithOptions(application.WebviewWindowOptions{
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

	/*
		The tray, which is what makes closing the window different from
		quitting.

		Attached to the window so showing and hiding reach the right one, and
		given the same icon the application uses everywhere else.
	*/
	tray := shell.NewTray(app, trayIcon)
	tray.AttachWindow(window)

	/*
		Pressing a notification goes to what it was about.

		Emitted rather than navigated directly: the interface knows how to
		reach a conversation and this does not, and an event is the same path
		the menu already uses.
	*/
	notifier.OnOpen(func(target notify.Target) {
		app.Event.Emit(shell.EventOpenConversation, map[string]string{
			"server":  target.Server,
			"room":    target.Room,
			"message": target.Message,
		})
	})

	if err := app.Run(); err != nil {
		log.Fatalf("tdc: %v", err)
	}
}
