package push

import (
	"context"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/notify"
)

/*
Delivering connects raised notifications to subscribed devices.

An adapter rather than a method on Service, so that the notification package
depends on an interface it defines and this one depends on nothing new. Which
of the two imports the other is not an accident: deciding who hears about a
message is the older and more general concern, and it should not have to know
what a VAPID key is.
*/
type Delivering struct {
	service *Service
}

// Deliveries builds the adapter, or nothing when push is not configured.
func Deliveries(service *Service) *Delivering {
	if !service.Enabled() {
		return nil
	}
	return &Delivering{service: service}
}

/*
Deliver sends one notification to one person's devices.

Errors are logged and dropped rather than returned. This runs behind a message
that has already been accepted, and a delivery that failed must not become a
message that appears not to have sent.
*/
func (d *Delivering) Deliver(ctx context.Context, userID uuid.UUID, about notify.Delivery) {
	if d == nil {
		return
	}

	/*
		The recipient's own choice, read here rather than passed in.

		One query per person actually being notified, which is a small set by
		the time this is called — and the alternative is carrying a preference
		through a package that has no reason to know it exists.
	*/
	hide := false
	if user, err := d.service.queries.GetUserByID(ctx, userID); err == nil {
		hide = user.HidePushContent
	} else {
		/*
			Failing towards hiding.

			If the account cannot be read, whether they asked for content to
			be kept off their lock screen is unknown — and the harm is not
			symmetric. Withholding a message from somebody who wanted to see
			it costs them a tap; showing one they asked to hide cannot be
			undone.
		*/
		d.service.log.Error("cannot read a notification preference; hiding content",
			"userId", userID, "error", err)
		hide = true
	}

	payload := Describe(Describing{
		Sender:      about.Sender,
		Room:        about.Room,
		Body:        about.Body,
		Private:     about.Private,
		HideContent: hide,
	})
	payload.Room = about.RoomID.String()
	payload.Event = about.EventID

	if err := d.service.Send(ctx, userID, payload); err != nil {
		d.service.log.Error("cannot deliver a notification", "userId", userID, "error", err)
	}
}
