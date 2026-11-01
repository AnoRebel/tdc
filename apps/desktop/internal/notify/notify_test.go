package notify

import (
	"errors"
	"sync"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

/*
When a system notification is shown, and where pressing one leads.

The interface has its own notifications — a bell with a count and a panel.
These are the other kind, the ones that arrive when nobody is looking at the
application, which is most of the time and the reason for installing it.
*/

// A stand-in for the operating system's service, which CI has none of.
type fakePlatform struct {
	mu sync.Mutex

	sent     []notifications.NotificationOptions
	allowed  bool
	checkErr error

	asked     int
	grants    bool
	responder func(notifications.NotificationResult)
	removed   []string
}

func (f *fakePlatform) SendNotification(options notifications.NotificationOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, options)
	return nil
}

func (f *fakePlatform) CheckNotificationAuthorization() (bool, error) {
	return f.allowed, f.checkErr
}

func (f *fakePlatform) RequestNotificationAuthorization() (bool, error) {
	f.mu.Lock()
	f.asked++
	f.mu.Unlock()
	return f.grants, nil
}

func (f *fakePlatform) OnNotificationResponse(callback func(notifications.NotificationResult)) {
	f.responder = callback
}

func (f *fakePlatform) RemoveNotification(identifier string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, identifier)
	return nil
}

/** Delivers an activation as the platform would. */
func (f *fakePlatform) activate(info map[string]any) {
	f.responder(notifications.NotificationResult{
		Response: notifications.NotificationResponse{UserInfo: info},
	})
}

func TestANotificationIsShownWhenAllowed(t *testing.T) {
	platform := &fakePlatform{allowed: true}
	notifier := New(platform)

	err := notifier.Show("amina", "Karibu", Target{
		Server: "https://tdc.or.tz", Room: "general", Message: "m1",
	})
	if err != nil {
		t.Fatalf("showing: %v", err)
	}

	if len(platform.sent) != 1 {
		t.Fatalf("%d notifications were sent", len(platform.sent))
	}
	if platform.sent[0].Title != "amina" || platform.sent[0].Body != "Karibu" {
		t.Errorf("what was sent is %+v", platform.sent[0])
	}
}

/*
Permission refused shows nothing, and says nothing about it.

Somebody who turned notifications off has said what they want. An application
that reported an error every time a message arrived would be arguing with them.
*/
func TestNothingIsShownWithoutPermission(t *testing.T) {
	platform := &fakePlatform{allowed: false}
	notifier := New(platform)

	if err := notifier.Show("amina", "Karibu", Target{Room: "general"}); err != nil {
		t.Errorf("a refused notification reported an error: %v", err)
	}
	if len(platform.sent) != 0 {
		t.Error("a notification was shown without permission")
	}
}

/*
A platform that cannot say is treated as refusing.

Showing nothing is recoverable; assuming permission that is not there produces
an error on every message instead.
*/
func TestAnUnansweredPermissionCheckMeansNo(t *testing.T) {
	platform := &fakePlatform{allowed: true, checkErr: errors.New("no session bus")}
	notifier := New(platform)

	_ = notifier.Show("amina", "Karibu", Target{Room: "general"})

	if len(platform.sent) != 0 {
		t.Error("a notification was shown though permission could not be checked")
	}
}

/*
Permission is checked each time rather than cached.

Somebody who turns notifications on in system settings while the application is
running expects that to take effect, not to require a restart.
*/
func TestPermissionIsCheckedEachTime(t *testing.T) {
	platform := &fakePlatform{allowed: false}
	notifier := New(platform)

	_ = notifier.Show("amina", "one", Target{Room: "general"})

	platform.allowed = true
	_ = notifier.Show("amina", "two", Target{Room: "general"})

	if len(platform.sent) != 1 {
		t.Errorf("%d were sent; permission granted mid-run did not take effect", len(platform.sent))
	}
}

/*
Asking happens once.

The prompt is a thing somebody answers, and asking again after they said no
would be nagging — on some platforms it also does nothing, which is worse
because the code would look like it worked.
*/
func TestPermissionIsAskedForOnce(t *testing.T) {
	platform := &fakePlatform{grants: true}
	notifier := New(platform)

	_, _ = notifier.Ask()
	_, _ = notifier.Ask()
	_, _ = notifier.Ask()

	if platform.asked != 1 {
		t.Errorf("permission was asked for %d times", platform.asked)
	}
}

/*
The property that makes a notification useful: pressing it goes there.

An application that merely came to the front would have understood half of what
somebody meant by pressing it.
*/
func TestPressingANotificationOpensTheConversation(t *testing.T) {
	platform := &fakePlatform{allowed: true}
	notifier := New(platform)

	var opened Target
	notifier.OnOpen(func(target Target) { opened = target })

	platform.activate(map[string]any{
		"server": "https://tdc.or.tz", "room": "general", "message": "m1",
	})

	want := Target{Server: "https://tdc.or.tz", Room: "general", Message: "m1"}
	if opened != want {
		t.Errorf("pressing it opened %+v", opened)
	}
}

/*
A notification carrying nowhere to go opens nothing.

One this application did not send, or one whose data did not survive. Guessing
would take somebody somewhere they did not ask for.
*/
func TestAnActivationWithNoRoomOpensNothing(t *testing.T) {
	platform := &fakePlatform{allowed: true}
	notifier := New(platform)

	opened := false
	notifier.OnOpen(func(Target) { opened = true })

	platform.activate(map[string]any{"server": "https://tdc.or.tz"})

	if opened {
		t.Error("a notification with no room opened something")
	}
}

/* An activation before anything is listening must not panic. */
func TestAnActivationWithNoHandlerIsHarmless(t *testing.T) {
	platform := &fakePlatform{allowed: true}
	New(platform)

	platform.activate(map[string]any{"room": "general"})
}

/*
The target travels with the notification.

A notification may be pressed minutes later, by which time whatever produced it
is gone. Carrying the destination inside it is what makes that work without
keeping an index of pending notifications correct.
*/
func TestTheTargetTravelsWithTheNotification(t *testing.T) {
	platform := &fakePlatform{allowed: true}
	notifier := New(platform)

	_ = notifier.Show("amina", "Karibu", Target{
		Server: "https://tdc.or.tz", Room: "general", Message: "m1",
	})

	data := platform.sent[0].Data
	if data["room"] != "general" || data["server"] != "https://tdc.or.tz" || data["message"] != "m1" {
		t.Errorf("the notification carries %+v", data)
	}
}

/*
A second notification for one conversation replaces the first.

Somebody away for an hour should not come back to forty banners for the same
channel.
*/
func TestNotificationsForOneConversationShareAnIdentity(t *testing.T) {
	platform := &fakePlatform{allowed: true}
	notifier := New(platform)

	target := Target{Server: "https://tdc.or.tz", Room: "general"}
	_ = notifier.Show("amina", "one", target)
	_ = notifier.Show("juma", "two", target)

	if platform.sent[0].ID != platform.sent[1].ID {
		t.Errorf("two notifications for one conversation have ids %q and %q",
			platform.sent[0].ID, platform.sent[1].ID)
	}
}

func TestDifferentConversationsDoNotReplaceEachOther(t *testing.T) {
	platform := &fakePlatform{allowed: true}
	notifier := New(platform)

	_ = notifier.Show("amina", "one", Target{Server: "s", Room: "general"})
	_ = notifier.Show("juma", "two", Target{Server: "s", Room: "random"})

	if platform.sent[0].ID == platform.sent[1].ID {
		t.Error("notifications for different conversations replace each other")
	}
}

/*
Ordinary priority, not time-sensitive.

Marking these urgent would override quiet hours, which is a judgement the
person made about their own attention and not this application's to overturn.
*/
func TestNotificationsDoNotOverrideQuietHours(t *testing.T) {
	platform := &fakePlatform{allowed: true}
	notifier := New(platform)

	_ = notifier.Show("amina", "Karibu", Target{Room: "general"})

	if level := platform.sent[0].InterruptionLevel; level != "active" {
		t.Errorf("notifications are sent at %q, which bypasses quiet hours", level)
	}
}

// An empty title would show as a blank banner, which says nothing and cannot
// be acted on.
func TestANotificationNeedsATitle(t *testing.T) {
	platform := &fakePlatform{allowed: true}
	notifier := New(platform)

	if err := notifier.Show("  ", "Karibu", Target{Room: "general"}); err == nil {
		t.Error("a notification with no title was sent")
	}
}

// Withdrawing takes down exactly the banner shown for the same target.
func TestAWithdrawnNotificationIsTheOneShown(t *testing.T) {
	platform := &fakePlatform{allowed: true}
	notifier := New(platform)
	target := Target{Server: "https://tdc.or.tz", Room: "standup"}

	if err := notifier.Show("amina is calling", "In #standup", target); err != nil {
		t.Fatalf("showing: %v", err)
	}
	if err := notifier.Withdraw(target); err != nil {
		t.Fatalf("withdrawing: %v", err)
	}
	if len(platform.removed) != 1 || platform.removed[0] != platform.sent[0].ID {
		t.Errorf("withdrew %v, but the banner shown was %q", platform.removed, platform.sent[0].ID)
	}
}
