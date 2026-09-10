/*
Package shell builds the parts of the desktop client that are not the web page:
the application menu, and later the tray.

The dividing line is that this package decides *what* the operating system
offers and the interface decides what happens. A menu item either performs
something only the native side can do — quitting, a window operation — or it
announces what somebody asked for and lets the page act on it. Nothing here
knows what a room is.
*/
package shell

import (
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Whether this is macOS, which puts Quit and About in an application menu
// named after the application rather than in File and Help.
const onMac = runtime.GOOS == "darwin"

/*
Events a menu item announces, which the frontend listens for.

Named as requests rather than actions — "the person asked to search" — because
the interface decides what that means and may reasonably decline, if a dialog
is open or nothing is signed in yet.

The strings are shared with the TypeScript that listens for them. They are
defined once here and generated into the bindings, so a rename that misses one
side is a compile error rather than a menu item that silently stops working.
*/
const (
	EventSearch        = "menu:search"
	EventPreferences   = "menu:preferences"
	EventNewChannel    = "menu:new-channel"
	EventNotifications = "menu:notifications"
	EventKeyboardHelp  = "menu:keyboard-help"

	/*
		Somebody pressed a system notification.

		Not a menu item, and here because this is where the names the frontend
		listens for are declared — one place, generated into TypeScript, so a
		rename cannot leave one side listening for something nothing sends.
	*/
	EventOpenConversation = "notification:open"

	/*
		Somebody asked to quit, from the tray or the menu.

		A request rather than an instruction: the interface knows whether
		anything is unsent and this does not, so it decides whether to ask
		before letting the application end.
	*/
	EventQuitRequested = "app:quit-requested"
)

/*
NewMenu builds the application menu.

Every item does something. A menu that lists what an application might one day
do teaches people not to look at it, so anything not yet built is left out
rather than added disabled — the menu grows as the features land.

The accelerators match the web client's shortcuts, so somebody moving between
the two does not have to learn a second set.
*/
func NewMenu(app *application.App) *application.Menu {
	menu := application.NewMenu()

	/*
		The application menu, which on macOS carries the name and the
		conventional items; elsewhere the platform ignores this role and the
		items below cover the same ground.
	*/
	if onMac {
		menu.AddRole(application.AppMenu)
	}

	file := menu.AddSubmenu("File")
	file.Add("New channel").
		SetAccelerator("CmdOrCtrl+N").
		OnClick(func(*application.Context) { app.Event.Emit(EventNewChannel) })
	file.AddSeparator()
	/*
		Closing the window rather than quitting. They are the same thing today,
		and stop being the same the moment the tray exists — at which point
		this item should keep meaning "close the window" and Quit should keep
		meaning quit.
	*/
	file.AddRole(application.CloseWindow)
	if !onMac {
		// macOS puts Quit in the application menu, which AppMenu above added.
		file.AddSeparator()
		file.AddRole(application.Quit)
	}

	// The clipboard and undo, which a webview does not provide by itself: a
	// text field with no working Ctrl+C is the kind of thing that makes an
	// application feel unfinished before anybody can say why.
	menu.AddRole(application.EditMenu)

	view := menu.AddSubmenu("View")
	view.Add("Search").
		SetAccelerator("CmdOrCtrl+K").
		OnClick(func(*application.Context) { app.Event.Emit(EventSearch) })
	view.Add("Notifications").
		OnClick(func(*application.Context) { app.Event.Emit(EventNotifications) })
	view.AddSeparator()
	view.AddRole(application.Reload)
	view.AddRole(application.ToggleFullscreen)

	menu.AddRole(application.WindowMenu)

	help := menu.AddSubmenu("Help")
	help.Add("Keyboard shortcuts").
		SetAccelerator("CmdOrCtrl+/").
		OnClick(func(*application.Context) { app.Event.Emit(EventKeyboardHelp) })
	help.AddSeparator()
	help.Add("Settings").
		SetAccelerator("CmdOrCtrl+,").
		OnClick(func(*application.Context) { app.Event.Emit(EventPreferences) })

	return menu
}
