package shell

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

/*
The tray icon, which is what makes closing the window different from quitting.

An application somebody keeps open all day should be reachable when it is not
on screen, and should say when something is waiting without being opened. That
is the whole of what a tray is for, and it is why the menu here carries the few
things somebody wants without looking at the application: bring it back, silence
it for a while, go straight to a conversation, or leave.
*/

// Events the tray announces, answered by the interface.
const (
	// EventMuteFor asks the interface to silence notifications, carrying how
	// long in minutes.
	EventMuteFor = "tray:mute-for"
	// EventUnmute asks it to stop.
	EventUnmute = "tray:unmute"
	// EventAddServer opens the screen for adding one.
	EventAddServer = "tray:add-server"
	// EventNewChannelFromTray asks to create a channel.
	EventNewChannelFromTray = "tray:new-channel"
	// EventOpenRoom goes to a conversation, carrying which.
	EventOpenRoom = "tray:open-room"
)

/*
Recent is a conversation the tray offers to jump to.

Supplied by the interface, because what counts as recent is something only it
knows — this holds the last few so the menu can be rebuilt without asking.
*/
type Recent struct {
	// ID is what the interface needs to open it.
	ID string `json:"id"`
	// Name is what somebody reads in the menu.
	Name string `json:"name"`
	// Unread is how many messages are waiting in it.
	Unread int `json:"unread"`
}

/*
Tray owns the icon and the menu on it.

Rebuilt rather than mutated when anything changes: a menu is small, rebuilding
is simple, and keeping item pointers in step with a changing list of
conversations is the kind of state that quietly stops matching.
*/
type Tray struct {
	app  *application.App
	tray *application.SystemTray

	mu      sync.Mutex
	unread  int
	recents []Recent
	mutedTo time.Time
	// Whether the window is currently shown, so the first item can say which
	// of the two things pressing it will do.
	shown bool
}

/*
NewTray puts the icon in the tray.

The icon is passed in rather than embedded here so the same bytes the
application uses everywhere else are the ones in the tray — an icon that drifts
from the application's own is one somebody has to learn twice.
*/
func NewTray(app *application.App, icon []byte) *Tray {
	tray := &Tray{app: app, tray: app.SystemTray.New(), shown: true}

	/*
		A template icon where the platform supports one: a tray sits on a panel
		whose colour this cannot read, and a single-colour icon the desktop
		recolours is the only thing that looks right on both a light and a dark
		one.
	*/
	tray.tray.SetTemplateIcon(icon)
	tray.tray.SetTooltip("TDC")

	// Clicking the icon shows or hides the window, which is what a tray icon
	// does everywhere and what somebody will try first.
	tray.tray.OnClick(func() { tray.ToggleWindow() })

	tray.rebuild()
	tray.tray.Run()

	return tray
}

/*
SetUnread shows how many messages are waiting.

On the tray's own label where a platform has one, and in the menu regardless —
the point of a tray for a chat application is knowing there is something to
read without opening anything.
*/
func (t *Tray) SetUnread(count int) {
	t.mu.Lock()
	changed := t.unread != count
	t.unread = count
	t.mu.Unlock()

	if !changed {
		return
	}

	if count > 0 {
		t.tray.SetLabel(badge(count))
		t.tray.SetTooltip(fmt.Sprintf("TDC — %s waiting", messages(count)))
	} else {
		t.tray.SetLabel("")
		t.tray.SetTooltip("TDC")
	}

	t.rebuild()
}

/*
SetRecents replaces the conversations the menu offers to jump to.

Supplied by the interface rather than read from anywhere here: which
conversations are worth offering is a judgement about what somebody has been
reading, and this package knows nothing about rooms.
*/
func (t *Tray) SetRecents(recents []Recent) {
	t.mu.Lock()
	// A handful. A tray menu somebody has to read is one they stop using, and
	// the application itself is one click away for the rest.
	if len(recents) > 5 {
		recents = recents[:5]
	}
	t.recents = recents
	t.mu.Unlock()

	t.rebuild()
}

/*
AttachWindow tells the tray which window it shows and hides.

Without it the tray's own show and hide have nothing to act on, and pressing
the icon does nothing — which looks like a broken tray rather than a missing
line.
*/
func (t *Tray) AttachWindow(window application.Window) {
	t.tray.AttachWindow(window)
}

// WindowShown tells the tray which of show or hide the first item should offer.
func (t *Tray) WindowShown(shown bool) {
	t.mu.Lock()
	changed := t.shown != shown
	t.shown = shown
	t.mu.Unlock()

	if changed {
		t.rebuild()
	}
}

/*
MutedUntil records that notifications are silenced, so the menu can say so.

The muting itself is the interface's to do — it owns the notifications — and
this only reflects it, so the two cannot disagree about whether it happened.
*/
func (t *Tray) MutedUntil(until time.Time) {
	t.mu.Lock()
	t.mutedTo = until
	t.mu.Unlock()

	t.rebuild()
}

// ToggleWindow shows the window if it is hidden and hides it if it is not.
func (t *Tray) ToggleWindow() {
	t.mu.Lock()
	shown := t.shown
	t.mu.Unlock()

	if shown {
		t.tray.HideWindow()
	} else {
		t.tray.ShowWindow()
	}

	t.WindowShown(!shown)
}

/*
rebuild draws the menu from what the tray currently knows.

Every item does something. A tray menu listing what an application might one
day do teaches people not to open it, which is the same rule the application
menu follows.
*/
func (t *Tray) rebuild() {
	t.mu.Lock()
	recents := append([]Recent(nil), t.recents...)
	mutedTo := t.mutedTo
	shown := t.shown
	t.mu.Unlock()

	menu := application.NewMenu()

	// What somebody came to the tray for: the window back.
	label := "Show TDC"
	if shown {
		label = "Hide TDC"
	}
	menu.Add(label).OnClick(func(*application.Context) { t.ToggleWindow() })

	// Where the messages are, when there are any.
	if len(recents) > 0 {
		menu.AddSeparator()

		for _, recent := range recents {
			room := recent
			item := recent.Name
			if recent.Unread > 0 {
				item = fmt.Sprintf("%s  (%s)", recent.Name, badge(recent.Unread))
			}

			menu.Add(item).OnClick(func(*application.Context) {
				t.tray.ShowWindow()
				t.WindowShown(true)
				t.app.Event.Emit(EventOpenRoom, map[string]string{"room": room.ID})
			})
		}
	}

	menu.AddSeparator()

	/*
		Silence, for a while rather than for ever.

		A meeting is the reason somebody reaches for this, and an application
		that offered only "off" would leave them muted for a week without
		noticing. Each option says when it ends.
	*/
	if muted := time.Until(mutedTo); muted > 0 {
		menu.Add(fmt.Sprintf("Muted for another %s — turn back on", remaining(muted))).
			OnClick(func(*application.Context) { t.app.Event.Emit(EventUnmute) })
	} else {
		/*
			Flat rather than a submenu.

			A submenu built here arrives empty: this menu is rebuilt and set
			again whenever the count or the conversations change, and the
			nested items do not survive that round trip over D-Bus. Three items
			at the top level cost a little height and are actually usable,
			which is the trade worth making.
		*/
		for _, option := range []struct {
			label   string
			minutes int
		}{
			{"Mute for 15 minutes", 15},
			{"Mute for an hour", 60},
			{"Mute until tomorrow", minutesUntilTomorrow()},
		} {
			minutes := option.minutes
			menu.Add(option.label).OnClick(func(*application.Context) {
				t.app.Event.Emit(EventMuteFor, map[string]int{"minutes": minutes})
			})
		}
	}

	menu.AddSeparator()
	menu.Add("New channel").
		OnClick(func(*application.Context) {
			t.tray.ShowWindow()
			t.WindowShown(true)
			t.app.Event.Emit(EventNewChannelFromTray)
		})
	menu.Add("Add a server").
		OnClick(func(*application.Context) {
			t.tray.ShowWindow()
			t.WindowShown(true)
			t.app.Event.Emit(EventAddServer)
		})

	menu.AddSeparator()
	menu.Add("About TDC").OnClick(func(*application.Context) {
		t.tray.ShowWindow()
		t.WindowShown(true)
		t.app.Event.Emit(EventShowAbout)
	})

	/*
		Quit, which asks first where anything would be lost.

		The window is shown before asking: a confirmation nobody can see is a
		quit that silently does nothing, which is exactly what this did when
		the window was hidden in the tray.
	*/
	menu.Add("Quit TDC").OnClick(func(*application.Context) {
		t.tray.ShowWindow()
		t.WindowShown(true)
		t.app.Event.Emit(EventQuitRequested)
	})

	t.tray.SetMenu(menu)
}

/*
badge writes a count small enough to read at a glance.

Past ninety-nine the exact number stops meaning anything and starts crowding
whatever it sits beside.
*/
func badge(count int) string {
	if count > 99 {
		return "99+"
	}
	return strconv.Itoa(count)
}

// messages counts in words, so a tooltip reads as a sentence.
func messages(count int) string {
	if count == 1 {
		return "1 message"
	}
	return strconv.Itoa(count) + " messages"
}

/*
remaining says how much of a mute is left, roughly.

Rounded up to the minute: somebody reading a menu wants to know whether it is
nearly over, not that it has fifty-eight seconds to run.
*/
func remaining(d time.Duration) string {
	minutes := int(d.Round(time.Minute).Minutes())
	if minutes >= 60 {
		hours := minutes / 60
		if hours == 1 {
			return "1 hour"
		}
		return strconv.Itoa(hours) + " hours"
	}
	if minutes <= 1 {
		return "a minute"
	}
	return strconv.Itoa(minutes) + " minutes"
}

// minutesUntilTomorrow is the time until the start of the next day, which is
// what "until tomorrow" means to somebody muting an application in the evening.
func minutesUntilTomorrow() int {
	now := time.Now()
	tomorrow := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).
		AddDate(0, 0, 1)

	return int(time.Until(tomorrow).Round(time.Minute).Minutes())
}

// Label is what the tray currently shows beside its icon, for tests.
func (t *Tray) Label() string { return strings.TrimSpace(t.tray.Label()) }
