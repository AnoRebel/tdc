// Command tdc is the TDC desktop client.
//
// It is a webview around the same interface the browser shows, plus the things
// a browser cannot do: a system tray, credentials in the operating system's
// keychain, notifications that survive the window being closed. Everything a
// person reads and types comes from the shared Nuxt layer, so the two clients
// cannot drift apart by accident.
package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"github.com/AnoRebel/tdc/apps/desktop/internal/api"
	"github.com/AnoRebel/tdc/apps/desktop/internal/notify"
	"github.com/AnoRebel/tdc/apps/desktop/internal/shell"
	"github.com/AnoRebel/tdc/apps/desktop/internal/updater"
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

/*
The tray icon as a hicolor tree, under the name the tray announces.

The same files the deb and rpm install, embedded so a build installed by
neither — an AppImage, a tarball, a development run — can put them in the
person's own icon theme for panels that resolve tray icons by name.
*/
//go:embed build/linux/icons/hicolor/*/apps/TDC.*
var trayIconTheme embed.FS

/*
updateManifest is the signed list of what the newest release offers each
platform. GitHub's /latest/ skips drafts and pre-releases.
*/
const updateManifest = "https://github.com/AnoRebel/tdc/releases/latest/download/update-manifest.json"

/*
updateKey is the public half of the key every update is signed with. Built
in, so somebody able to change a release still cannot sign one.
*/
//go:embed build/tdc.key.pub
var updateKey []byte

/*
windowSettingsPath is where how-the-window-looks is kept.

Beside the update settings, under the user's own configuration directory: none
of it is secret, and all of it belongs to one person rather than the machine.
*/
/*
placementPath is where the window's last position is kept.

Under the user's cache rather than their configuration: it is something
observed rather than chosen, and losing it costs one badly-placed window.
*/
func placementPath() string {
	cache, err := os.UserCacheDir()
	if err != nil || cache == "" {
		return ""
	}
	return filepath.Join(cache, "tdc", "placement.json")
}

// startupSettingsPath is where opening at login and opening hidden are kept.
func startupSettingsPath() string {
	config, err := os.UserConfigDir()
	if err != nil || config == "" {
		return ""
	}
	return filepath.Join(config, "tdc", "startup.json")
}

func windowSettingsPath() string {
	config, err := os.UserConfigDir()
	if err != nil || config == "" {
		return ""
	}
	return filepath.Join(config, "tdc", "window.json")
}

/*
bannersFor lets the live bridge raise a system notification.

The two packages describe the same thing — where a notification takes somebody
when it is pressed — and neither imports the other, so the translation happens
here, where they meet. Keeping them apart is what lets the bridge be tested
without a notification daemon, and the notifier without a socket.
*/
func bannersFor(notifier *notify.Notifier) api.Notifier {
	return bannerAdapter{notifier}
}

type bannerAdapter struct{ notifier *notify.Notifier }

func (b bannerAdapter) Withdraw(target api.Target) error {
	return b.notifier.Withdraw(notify.Target{Server: target.Server, Room: target.Room})
}

func (b bannerAdapter) Show(title, body string, target api.Target) error {
	return b.notifier.Show(title, body, notify.Target{
		Server:  target.Server,
		Room:    target.Room,
		Message: target.Message,
	})
}

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

	// Updates, which verify or refuse.
	updates := updater.NewService(updateManifest, updateKey, updater.DefaultPath())

	// Held by name because the live bridge, registered once the application
	// exists, needs the same instance the webview's requests go through — the
	// one whose cookie jar holds the session the live socket must carry.
	apiService := api.New()

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
			application.NewService(apiService),
			/*
				The platform's own notifications, and this application's use of
				them. Both are registered: the first is what Wails needs to
				start the service, the second is the small surface the webview
				may call.
			*/
			application.NewService(notifications.New()),
			application.NewService(notify.NewService(notifier)),
			// What this build is, which is the first thing anybody reporting a
			// problem is asked for.
			application.NewService(shell.NewAboutService()),
			// Checking for, fetching and installing a newer version.
			application.NewService(updates),
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
				What this application is called, everywhere the desktop looks.

				One name in three places that all have to agree: the GTK
				application id, the program name the Wayland surface takes its
				app_id from, and the StartupWMClass in the .desktop entry. When
				they disagree, the running window is not matched with the entry
				that launched it — the application shows up as a second,
				nameless icon, and pinning it to a dock pins something that
				never lights up.

				The id used to be org.wails.<Name>, built by the toolkit and not
				exposed, so this and StartupWMClass were both set to what the
				toolkit produced rather than to what this application is called.
				beta.23 lets the id be set, so all three are net.anorebel.tdc
				now, which is also what the icons and the package are named.

				ProgramName is left to follow: Wails defaults it to the
				application id precisely so the two cannot drift.
			*/
			ApplicationID: "net.anorebel.tdc",
		},
		Mac: application.MacOptions{
			/*
				Safe to leave on now that there is a tray, because hiding is
				not closing.

				shell.Closing intercepts the close button and, where somebody
				chose the tray, hides the window instead of letting it close —
				so the last window only ever closes when they meant to quit.
			*/
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	/*
		The menu, after the application exists because its items emit events
		through it. Every item does something: an item that is there to look
		complete teaches people the menu is not worth opening.
	*/
	app.Menu.SetApplicationMenu(shell.NewMenu(app))

	/*
		How solid the window should be, read before it is built.

		WebKitGTK latches its clear colour at the first composite and does not
		refresh it from a later call, so this cannot be changed once the window
		exists — which is why the setting says it applies when TDC is reopened.
	*/
	windowSettings := windowSettingsPath()
	opacity := shell.SavedOpacity(windowSettings)

	/*
		Whether to open to the tray rather than to a window.

		Read before the window is created, because a window created visible and
		then hidden flashes on screen — which is exactly what somebody choosing
		this asked not to happen.
	*/
	startupSettings := startupSettingsPath()
	openHidden := shell.StartsHidden(startupSettings)

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
		BackgroundColour: application.RGBA{
			Red: 20, Green: 22, Blue: 28,
			// The alpha somebody chose, applied at creation because it cannot
			// be applied afterwards.
			Alpha: opacity.Alpha(),
		},
		/*
			Able to be translucent, whether or not it currently is.

			The type is fixed when the window is created, so a window made
			solid could only become translucent after a restart — and a
			preference that needs a restart is one people set once and never
			revisit. What the setting changes is the alpha of the colour above.
		*/
		BackgroundType: application.BackgroundTypeTranslucent,
		// Created hidden where that was chosen, rather than shown and then
		// hidden, which flashes.
		Hidden: openHidden,
		URL:    "/",
	})

	/*
		The tray, which is what makes closing the window different from
		quitting.

		Attached to the window so showing and hiding reach the right one, and
		given the same icon the application uses everywhere else.
	*/
	if theme, err := fs.Sub(trayIconTheme, "build/linux/icons/hicolor"); err == nil {
		// Before the tray exists, so the first lookup by name already finds it.
		if err := shell.InstallTrayIcon(theme); err != nil {
			log.Printf("tdc: the tray icon could not be added to the icon theme: %v", err)
		}
	}

	tray := shell.NewTray(app, trayIcon)
	tray.AttachWindow(window)
	tray.WindowShown(!openHidden)

	/*
		Where the window was last time, if that is still somewhere it can be
		seen.

		Checked against the connected displays: a position remembered from a
		docked laptop is off-screen on a train, and a window restored to it is
		one nobody can reach.
	*/
	shell.RestoreWindowState(window, placementPath(), shell.ScreenBounds(app))

	/*
		Opening at login, and opening to the tray. Registered here because it
		needs the application, which the service list above is declared with.
	*/
	app.RegisterService(application.NewService(shell.NewStartupService(app, startupSettings)))

	// Remote control of a screen shared in a call; see remotecontrol.go.
	app.RegisterService(application.NewService(remoteControlFor(app)))
	// Calls: switched on in a web view that has them off, and what is missing
	// where they still cannot work.
	shell.EnableCalls(window.(*application.WebviewWindow))
	app.RegisterService(application.NewService(shell.NewCallSupport()))

	/*
		The live connection, bridged from Go.

		Registered here for the same reason the others are: it needs the
		application, to push each frame to the webview as an event. It shares the
		api service's cookie jar, so the socket it opens carries the session the
		webview cannot.
	*/
	app.RegisterService(application.NewService(
		api.NewLiveBridge(apiService, app.Event.Emit, bannersFor(notifier)),
	))

	/*
		How a sign-in through a provider leaves this application.

		Wired here because it needs the application. It goes to the system
		browser rather than this window on purpose: there the address bar shows
		whose page is asking for the password and the password manager knows the
		site, and in a webview neither is true.
	*/
	api.UseOpener(apiService, app.Browser)
	// Where a file is saved, asked of the operating system's own dialog.
	api.UseSaveChooser(apiService, func(suggested string) (string, error) {
		return app.Dialog.SaveFile().SetFilename(suggested).PromptForSingleSelection()
	})

	/*
		What the close button does, which is a question rather than an answer.

		For a chat client left open all day, closing usually means "off my
		screen"; for somebody who opens it to read something, it means done.
		Neither is guessable, so the first close asks and every one after does
		what was chosen.
	*/
	/*
		How solid the window is, restored before anything is drawn.

		Applied here rather than from the interface: the window is painted
		before any JavaScript runs, and one that flashed solid before turning
		translucent would be worse than one that never moved.
	*/
	translucency := shell.NewTranslucency(window, windowSettings, 20, 22, 28)
	/*
		Registered after the window exists, because it acts on one. The
		services above are declared with the application; this one cannot be.
	*/
	app.RegisterService(application.NewService(shell.NewTranslucencyService(translucency)))

	closing := shell.NewClosing(app, window, true)
	closing.OnHide(func() { tray.WindowShown(false) })
	app.Event.On(shell.EventSetCloseBehaviour, func(event *application.CustomEvent) {
		closing.SetBehaviour(shell.BehaviourFrom(event.Data))
	})

	/*
		Quitting, once the interface has checked that nothing is lost.

		Go asks and the interface answers, rather than either deciding alone:
		only the interface knows whether a message is unsent, and only Go can
		end the process.
	*/
	app.Event.On(shell.EventQuitConfirmed, func(*application.CustomEvent) {
		app.Quit()
	})

	/*
		Hiding the window, from the menu or its shortcut.

		Through the tray rather than the window directly, so its menu says
		"Show TDC" afterwards — a tray offering to hide a window that is
		already hidden is the kind of small wrongness that makes an
		application feel unmaintained.
	*/
	app.Event.On(shell.EventHideWindow, func(*application.CustomEvent) {
		window.Hide()
		tray.WindowShown(false)
	})

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

	/*
		Whatever checking the settings ask for, once the application is up.

		Started here rather than in NewService so nothing happens during
		construction — an application that made a network request before its
		window existed would be doing so on somebody's behalf without showing
		them anything.
	*/
	if err := updates.Use(app.Updater); err != nil {
		log.Printf("tdc: updates are off: %v", err)
	}
	updateContext, stopUpdates := context.WithCancel(context.Background())
	defer stopUpdates()
	updates.Start(updateContext)

	if err := app.Run(); err != nil {
		log.Fatalf("tdc: %v", err)
	}
}
