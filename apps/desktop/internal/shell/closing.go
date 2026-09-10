package shell

import (
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

/*
What closing the window means.

Two reasonable answers, and which one somebody wants is not something an
application can know: for a chat client left open all day, closing usually
means "get this off my screen", and quitting means quitting. For somebody who
opens it to read something and closes it when done, the opposite.

So it is a setting, and the first time it happens the application asks rather
than deciding — after which it does what was chosen without asking again.
*/

// Events the closing behaviour announces.
const (
	/*
		EventAskCloseBehaviour asks the interface what closing should do.

		Sent the first time somebody closes the window with a tray available
		and no preference recorded. The interface asks and answers with
		SetCloseBehaviour; until it does, the window stays open, because
		guessing and being wrong loses either the window or the application.
	*/
	EventAskCloseBehaviour = "window:ask-close-behaviour"

	/*
		EventConfirmQuit asks whether quitting is all right.

		The interface knows whether anything is unsent and this does not, so it
		decides whether to ask. It answers by quitting or doing nothing.
	*/
	EventConfirmQuit = "window:confirm-quit"

	/*
		EventSetCloseBehaviour is the interface's answer, and any later change
		from settings.
	*/
	EventSetCloseBehaviour = "window:set-close-behaviour"
)

/*
BehaviourFrom reads what the interface sent.

Anything unrecognised leaves the application asking again rather than picking
one — a value nothing here knows is not a choice somebody made.
*/
func BehaviourFrom(data any) CloseBehaviour {
	values, ok := data.([]any)
	if !ok || len(values) == 0 {
		return CloseAsks
	}

	payload, ok := values[0].(map[string]any)
	if !ok {
		return CloseAsks
	}

	switch payload["behaviour"] {
	case string(CloseToTray):
		return CloseToTray
	case string(CloseQuits):
		return CloseQuits
	default:
		return CloseAsks
	}
}

// CloseBehaviour is what closing the window does.
type CloseBehaviour string

const (
	// CloseAsks is the state before anybody has chosen.
	CloseAsks CloseBehaviour = ""
	// CloseToTray hides the window and leaves the application running.
	CloseToTray CloseBehaviour = "tray"
	// CloseQuits ends the application, which is what closing means without a
	// tray.
	CloseQuits CloseBehaviour = "quit"
)

/*
Closing decides what the window's close button does.

Held here rather than in the interface because the decision has to be made
while the window is closing, which is too late to ask across a boundary and
wait for an answer.
*/
type Closing struct {
	app    *application.App
	window application.Window

	mu        sync.RWMutex
	behaviour CloseBehaviour
	// Whether a tray exists at all. Without one, hiding the window would leave
	// the application running with no way back to it.
	hasTray bool
	onHide  func()
}

// NewClosing takes over the window's close button.
func NewClosing(app *application.App, window application.Window, hasTray bool) *Closing {
	closing := &Closing{app: app, window: window, hasTray: hasTray}

	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		closing.handle(event)
	})

	return closing
}

/*
SetBehaviour records what closing should do from now on.

Called by the interface once somebody has answered, and again if they change
it in settings.
*/
func (c *Closing) SetBehaviour(behaviour CloseBehaviour) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.behaviour = behaviour
}

// Behaviour reports what closing currently does.
func (c *Closing) Behaviour() CloseBehaviour {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.behaviour
}

// OnHide is called when the window is hidden rather than closed, so the tray
// can say "Show" rather than "Hide".
func (c *Closing) OnHide(hide func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onHide = hide
}

/*
Decision is what a close should do.

Separated from acting on it so the rule can be tested: the event Wails hands a
close hook keeps its cancellation private, so a test that called handle could
not see what was decided.
*/
type Decision string

const (
	// LetItClose allows the close, which ends the application.
	LetItClose Decision = "close"
	// HideIt keeps the application running behind its tray icon.
	HideIt Decision = "hide"
	// AskFirst leaves the window open while the interface asks.
	AskFirst Decision = "ask"
)

/*
decide reports what this close should do.

The whole rule, in one place and with no side effects.
*/
func decide(behaviour CloseBehaviour, hasTray bool) Decision {
	/*
		No tray means closing quits, whatever was chosen. Hiding a window with
		no icon to bring it back leaves an application running that somebody
		can neither see nor stop — which is worse than either answer to the
		question, and a preference carried from a machine that had a tray must
		not strand them.
	*/
	if !hasTray {
		return LetItClose
	}

	switch behaviour {
	case CloseToTray:
		return HideIt
	case CloseQuits:
		return LetItClose
	default:
		return AskFirst
	}
}

/*
handle carries out what decide decided.

Cancelling the event is what keeps the application alive: a close that is not
cancelled ends it, so hiding has to be paired with a cancellation or the window
disappears and everything behind it with it.
*/
func (c *Closing) handle(event *application.WindowEvent) {
	c.mu.RLock()
	behaviour, hasTray, onHide := c.behaviour, c.hasTray, c.onHide
	c.mu.RUnlock()

	switch decide(behaviour, hasTray) {
	case HideIt:
		event.Cancel()
		c.window.Hide()
		if onHide != nil {
			onHide()
		}

	case AskFirst:
		event.Cancel()
		c.app.Event.Emit(EventAskCloseBehaviour)

	case LetItClose:
		// Not cancelled, so the close proceeds and the application ends.
	}
}
