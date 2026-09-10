/*
Package notify shows the notifications only a desktop application can.

The interface already has its own: a bell with a count, and a panel listing
what happened. Those are what somebody sees while looking at the application.
These are the other kind — the ones that arrive when they are not, which is
most of the time and the whole reason for installing this rather than keeping a
tab open.

Both are shown. A message that raised a system notification is also in the
panel, because somebody who dismissed a banner without reading it has not read
it, and a count that failed to include it would be lying.
*/
package notify

import (
	"fmt"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

/*
Notifier shows system notifications, where this machine allows any.

Permission is a property of the machine and the person, not of this
application: a desktop environment with notifications switched off, or one
where somebody declined, is a machine this must work on quietly rather than
insist to.
*/
/*
Platform is the part of the operating system's notification service this uses.

An interface rather than the concrete service so the decisions here — when to
show, what to do when one is pressed, what happens without permission — can be
exercised without a desktop session. The real one talks to a notification
daemon that CI does not have, and logic that could only be tested by hand would
not be tested.
*/
type Platform interface {
	SendNotification(options notifications.NotificationOptions) error
	CheckNotificationAuthorization() (bool, error)
	RequestNotificationAuthorization() (bool, error)
	OnNotificationResponse(callback func(result notifications.NotificationResult))
}

type Notifier struct {
	service Platform

	mu sync.RWMutex
	/*
		Whether the last check said this application may notify.

		Cached rather than asked each time: on some platforms asking prompts,
		and prompting on every incoming message would be intolerable.
	*/
	allowed bool
	// Whether permission has been asked for at all this run.
	asked bool

	/*
		Where an activated notification should take somebody.

		Set by the application, called from whichever goroutine the platform
		delivers the response on.
	*/
	open func(Target)
}

/*
Target is the conversation a notification was about.

Carried through the notification itself rather than looked up when it comes
back: a notification may be activated minutes later, by which time whatever
produced it is long gone, and the alternative — an index of pending
notifications — is state to keep correct for no gain.
*/
type Target struct {
	// Server the conversation is on, since this client may hold several.
	Server string `json:"server"`
	// Room to open.
	Room string `json:"room"`
	// Message to scroll to, where the notification was about one.
	Message string `json:"message,omitempty"`
}

/*
Service is the notifier as the interface calls it.

Wails binds exported methods, so what the webview can reach is exactly what is
declared here: asking for permission, and reporting whether it was given. The
showing itself is done by Go when a message arrives, not asked for by the page.
*/
type Service struct{ notifier *Notifier }

// NewService wraps a notifier for binding.
func NewService(notifier *Notifier) *Service { return &Service{notifier: notifier} }

/*
MayNotify reports whether this machine will show notifications.

So the interface can offer to ask rather than presenting a switch that would do
nothing.
*/
func (s *Service) MayNotify() bool { return s.notifier.Allowed() }

/*
AskToNotify requests permission, once.

Called when somebody turns notifications on rather than at startup: an
application that asks before showing anybody anything is asking about something
they have no opinion on yet, and the usual answer to that is no.
*/
func (s *Service) AskToNotify() (bool, error) { return s.notifier.Ask() }

// New builds a notifier over the platform's own service.
func New(service Platform) *Notifier {
	notifier := &Notifier{service: service}

	/*
		Registered once, at construction. A response may arrive at any time
		after a notification is shown — including after the window was closed
		and reopened — so there is no later moment at which this could be
		attached without losing some.
	*/
	service.OnNotificationResponse(notifier.handle)

	return notifier
}

/*
OnOpen says where an activated notification should lead.

Called by the application once. Pressing a notification is somebody saying
"show me this", and an application that merely came to the front without going
there would have understood half of it.
*/
func (n *Notifier) OnOpen(open func(Target)) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.open = open
}

/*
Allowed reports whether this machine will show notifications.

Asks the platform rather than trusting the cache, because somebody may have
changed it in system settings while the application was running — which is
exactly when they would expect it to take effect.
*/
func (n *Notifier) Allowed() bool {
	allowed, err := n.service.CheckNotificationAuthorization()
	if err != nil {
		// A platform that cannot say is treated as refusing. Showing nothing is
		// recoverable; assuming permission that is not there produces errors on
		// every message.
		allowed = false
	}

	n.mu.Lock()
	n.allowed = allowed
	n.mu.Unlock()

	return allowed
}

/*
Ask requests permission, once.

Deliberately not called at startup. An application that asks before it has
shown anybody anything is asking about a thing they have no opinion on yet, and
the usual answer to that is no — which is difficult to undo. The right moment
is after somebody has signed in and would actually receive something.
*/
func (n *Notifier) Ask() (bool, error) {
	n.mu.Lock()
	if n.asked {
		allowed := n.allowed
		n.mu.Unlock()
		return allowed, nil
	}
	n.asked = true
	n.mu.Unlock()

	allowed, err := n.service.RequestNotificationAuthorization()

	n.mu.Lock()
	n.allowed = allowed
	n.mu.Unlock()

	return allowed, err
}

/*
Show displays one notification.

Silently does nothing where permission was refused. That is not an error to
report: somebody who turned notifications off has said what they want, and an
application complaining about it every time would be arguing with them.
*/
func (n *Notifier) Show(title, body string, target Target) error {
	if !n.Allowed() {
		return nil
	}

	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("a notification with no title would show as an empty banner")
	}

	return n.service.SendNotification(notifications.NotificationOptions{
		/*
			Identified by what it is about, so a second notification for the
			same conversation replaces the first rather than stacking. Somebody
			away for an hour should not return to forty banners.
		*/
		ID:    identify(target),
		Title: title,
		Body:  body,

		/*
			Grouped by conversation, so a desktop environment that stacks
			related notifications stacks these correctly.
		*/
		ThreadID: target.Server + "|" + target.Room,

		/*
			The target travels with the notification and comes back on
			activation. A notification pressed an hour later still knows where
			it was going.
		*/
		Data: map[string]any{
			"server":  target.Server,
			"room":    target.Room,
			"message": target.Message,
		},

		/*
			Ordinary priority. A message is worth interrupting for and is not
			an emergency, and an application that marked its notifications
			time-sensitive would be overriding a judgement that is not its to
			make — which is what quiet hours exist to express.
		*/
		InterruptionLevel: "active",
	})
}

/*
handle takes an activated notification back to what it was about.

Runs on whatever goroutine the platform delivers on, which is why the callback
is read under the lock rather than captured.
*/
func (n *Notifier) handle(result notifications.NotificationResult) {
	if result.Error != nil {
		return
	}

	n.mu.RLock()
	open := n.open
	n.mu.RUnlock()

	if open == nil {
		return
	}

	info := result.Response.UserInfo
	target := Target{
		Server:  text(info["server"]),
		Room:    text(info["room"]),
		Message: text(info["message"]),
	}

	// Nothing to open. A notification with no room is one this did not send,
	// and guessing where to go would be worse than staying put.
	if target.Room == "" {
		return
	}

	open(target)
}

// identify names a notification by what it is about, so a later one for the
// same conversation replaces it.
func identify(target Target) string {
	return "tdc:" + target.Server + ":" + target.Room
}

// text reads a value the platform round-tripped through JSON, where a string
// may arrive as any type.
func text(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}
