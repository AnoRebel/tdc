package webhooks

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
What a subscription receives, by version.

A subscription is made against a version and keeps receiving it. A change that
would break a receiver — renaming a field, changing what one means — is a new
version rather than an edit to this one, so a receiver written against version 1
keeps working unchanged after version 2 exists, until its owner chooses to move.
Adding a field is not a breaking change; receivers ignore what they do not know.
*/

// Input is what a payload is rendered from.
type Input struct {
	// Delivery identifies this delivery, the same across its retries.
	Delivery     string
	Subscription string
	Server       string
	Event        dbgen.Event
	// Kind is what the subscription asked for, when the server named it by
	// reading Event — issue.commented for a message in a discussion. Empty
	// means the event's own type.
	Kind string
	// Roomless marks an event that belongs to no room: a new account.
	Roomless bool
	// Project and Issue say what the room belongs to, when it belongs to one.
	Project *Ref
	Issue   *Ref
}

// Ref names a project or an issue: its identifier, and the key people use.
type Ref struct {
	ID  string `json:"id"`
	Key string `json:"key"`
}

// Renderer turns one event into one version's payload.
type Renderer func(Input) ([]byte, error)

var (
	versionsMu sync.RWMutex
	versions         = map[int32]Renderer{1: renderV1}
	current    int32 = 1
)

// CurrentVersion is what a new subscription is made against.
func CurrentVersion() int32 {
	versionsMu.RLock()
	defer versionsMu.RUnlock()
	return current
}

// Render produces the payload for a version. A version this server no longer
// renders is an error, never a silent substitution of another.
func Render(version int32, input Input) ([]byte, error) {
	versionsMu.RLock()
	render, known := versions[version]
	versionsMu.RUnlock()
	if !known {
		return nil, fmt.Errorf("payload version %d is not one this server renders", version)
	}
	return render(input)
}

/*
AddVersionForTest registers a version and makes it current, returning a
function that undoes both.

For the tests that prove an existing subscription keeps its version after a
new one appears — which needs a new one to appear.
*/
func AddVersionForTest(version int32, render Renderer) func() {
	versionsMu.Lock()
	previous := current
	versions[version] = render
	current = version
	versionsMu.Unlock()

	return func() {
		versionsMu.Lock()
		delete(versions, version)
		current = previous
		versionsMu.Unlock()
	}
}

// payloadV1 is version 1, as documented in docs/webhooks-and-apps.md.
type payloadV1 struct {
	Version      int    `json:"version"`
	ID           string `json:"id"`
	Type         string `json:"type"`
	OccurredAt   string `json:"occurredAt"`
	Subscription string `json:"subscription"`
	Server       string `json:"server"`
	// Room is absent only for an event that belongs to no room.
	Room *roomV1 `json:"room,omitempty"`
	// Project and Issue are present for an event in a project's stream or an
	// issue's discussion. Added within version 1: a receiver ignores what it
	// does not know.
	Project *Ref    `json:"project,omitempty"`
	Issue   *Ref    `json:"issue,omitempty"`
	Event   eventV1 `json:"event"`
}

type roomV1 struct {
	ID      string `json:"id"`
	Address string `json:"address"`
}

type eventV1 struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	// Sequence is absent only for an event that belongs to no room, which has
	// no position in one. A room's first event is at 1.
	Sequence int64  `json:"sequence,omitempty"`
	Sender   string `json:"sender"`
	// Content is the event's own content, as the timeline gives it.
	Content json.RawMessage `json:"content"`
	// CreatedAt is epoch milliseconds, as on the live connection.
	CreatedAt int64 `json:"createdAt"`
}

func renderV1(input Input) ([]byte, error) {
	event := input.Event
	kind := input.Kind
	if kind == "" {
		kind = event.Type
	}
	var room *roomV1
	if !input.Roomless {
		room = &roomV1{ID: event.RoomID.String(), Address: event.RoomAddress}
	}
	return json.Marshal(payloadV1{
		Version: 1,
		ID:      input.Delivery,
		Type:    kind,
		// RFC 3339 in UTC: an absolute instant a receiver in any zone reads
		// the same way.
		OccurredAt:   time.UnixMilli(event.CreatedAt).UTC().Format(time.RFC3339Nano),
		Subscription: input.Subscription,
		Server:       input.Server,
		Room:         room,
		Project:      input.Project,
		Issue:        input.Issue,
		Event: eventV1{
			ID:        event.ID,
			Type:      event.Type,
			Sequence:  event.Sequence,
			Sender:    event.SenderAddress,
			Content:   json.RawMessage(event.Content),
			CreatedAt: event.CreatedAt,
		},
	})
}
