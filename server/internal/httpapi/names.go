package httpapi

import (
	"context"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/rooms"
)

/*
nameBook remembers what each person is called for the length of one response.

Readers are shown display names, but events and notifications name people by
address, so every list that shows them has to look the names up. A page of
fifty messages is usually a handful of people, and this reads each of them
once rather than once a message.

Empty for somebody on another server: this server knows them only by address,
and a client calls them by that.
*/
type nameBook struct {
	rooms *rooms.Service
	known map[string]string
}

func newNameBook(service *rooms.Service) *nameBook {
	return &nameBook{rooms: service, known: map[string]string{}}
}

// sender is what to call whoever sent an event.
func (b *nameBook) sender(ctx context.Context, event dbgen.Event) string {
	if event.SenderID == nil {
		return ""
	}
	key := "id:" + event.SenderID.String()
	if name, ok := b.known[key]; ok {
		return name
	}
	name := b.rooms.SenderName(ctx, event)
	b.known[key] = name
	return name
}

// at is what to call the person at an address.
func (b *nameBook) at(ctx context.Context, address string) string {
	key := "address:" + address
	if name, ok := b.known[key]; ok {
		return name
	}
	name := b.rooms.NameAt(ctx, address)
	b.known[key] = name
	return name
}
