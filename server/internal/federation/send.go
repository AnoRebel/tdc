package federation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/AnoRebel/tdc/server/internal/events"
)

/*
Sending a signed request to a peer.

The other half of signing.go: that file can put a signature on a request, this
one actually delivers it, and having both in one place means a caller cannot
accidentally send something unsigned by forgetting a step.

Every federated request this server makes goes through here, so the things that
must always happen — signing, naming ourselves as the origin, refusing to reach
private addresses unless permitted — happen once rather than at each call site.
*/

// Sender delivers signed requests to peers.
type Sender struct {
	client *http.Client
	// files carries a file's bytes, which take longer than any request.
	files *http.Client
	// origin is this server's own canonical address, sent so a peer knows
	// whose keys to fetch. It is a claim, and the signature is what makes it
	// worth anything.
	origin  string
	keyring *events.Keyring
	// trust is the operator's answer to "which peers?", consulted before
	// dialing. The same answer the receiving side uses, from the same lists:
	// a block that stopped arriving requests but not departing ones would
	// leave this server still reaching out to a peer its operator refused.
	trust Trust
	now   func() time.Time
}

// NewSender builds a sender for this server.
func NewSender(origin string, keyring *events.Keyring, trust Trust, allowPrivate bool) *Sender {
	return &Sender{
		client:  newPeerHTTPClient(allowPrivate),
		files:   newPeerFileClient(allowPrivate),
		origin:  origin,
		keyring: keyring,
		trust:   trust,
		now:     time.Now,
	}
}

/*
Send signs a request to a peer and delivers it.

The body is marshalled here rather than taken as a reader, because the
signature covers a digest of the exact bytes sent — and a reader could only be
read once, so signing from it would leave nothing to deliver.
*/
func (s *Sender) Send(ctx context.Context, peer, path string, payload any) (*http.Response, error) {
	// Before anything else, including resolving the address: a refused peer
	// should not even be looked up, let alone dialed.
	if allowed, why := s.trust.Allows(peer); !allowed {
		return nil, fmt.Errorf("refusing to reach %s: %w", peer, why)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode the request: %w", err)
	}

	endpoint, err := peerURL(peer, path)
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build the request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("TDC-Origin-Server", s.origin)

	if err := Sign(request, body, s.keyring, s.now()); err != nil {
		return nil, fmt.Errorf("sign the request: %w", err)
	}

	response, err := s.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("reach %s: %w", peer, err)
	}
	return response, nil
}

/*
Ping asks a peer whether the signed transport works between us.

No room, no event, no membership — which is the point. When something real
fails later, a working ping says the transport is fine and the fault is in what
rides on it; a failing ping says stop looking at the feature.
*/
func (s *Sender) Ping(ctx context.Context, peer string) error {
	response, err := s.Send(ctx, peer, "/api/federation/ping", map[string]any{"ping": true})
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		reason, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return fmt.Errorf("peer %s refused the ping: %s: %s",
			peer, response.Status, strings.TrimSpace(string(reason)))
	}
	return nil
}

func peerURL(peer, path string) (string, error) {
	trimmed := strings.TrimSpace(peer)
	if trimmed == "" {
		return "", ErrNoAddress
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	return strings.TrimRight(trimmed, "/") + path, nil
}

/*
DeliverEvents pushes a room's events to a peer.

Returns how many the peer stored, which is not how many were sent: a peer that
already held some of them stores fewer, and that is a successful delivery
rather than a partial one.
*/
func (s *Sender) DeliverEvents(ctx context.Context, peer, slug string, batch any) (int, error) {
	response, err := s.Send(ctx, peer, "/api/federation/rooms/"+slug+"/events",
		map[string]any{"events": batch})
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		reason, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return 0, fmt.Errorf("peer %s refused the delivery: %s: %s",
			peer, response.Status, strings.TrimSpace(string(reason)))
	}

	var accepted struct {
		Stored int `json:"stored"`
	}
	if err := json.NewDecoder(response.Body).Decode(&accepted); err != nil {
		return 0, fmt.Errorf("peer %s gave an unreadable answer: %w", peer, err)
	}
	return accepted.Stored, nil
}

/*
FetchEvents asks a peer for a room's events after a cursor.

The pull half of catch-up. Push and pull ask the same underlying question —
what has this server not seen yet — so a peer returning from an outage asks
once rather than waiting out a retry queue.
*/
func (s *Sender) FetchEvents(
	ctx context.Context,
	peer, slug string,
	since int64,
) ([]events.Event, error) {
	if allowed, why := s.trust.Allows(peer); !allowed {
		return nil, fmt.Errorf("refusing to reach %s: %w", peer, why)
	}

	endpoint, err := peerURL(peer, fmt.Sprintf("/api/federation/rooms/%s/events?since=%d", slug, since))
	if err != nil {
		return nil, err
	}

	// A GET still carries a signature, over an empty body: the destination and
	// path are covered, which is what stops the request being replayed
	// elsewhere.
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build the request: %w", err)
	}
	request.Header.Set("TDC-Origin-Server", s.origin)
	if err := Sign(request, nil, s.keyring, s.now()); err != nil {
		return nil, fmt.Errorf("sign the request: %w", err)
	}

	response, err := s.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("reach %s: %w", peer, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		reason, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return nil, fmt.Errorf("peer %s refused the request: %s: %s",
			peer, response.Status, strings.TrimSpace(string(reason)))
	}

	var answer struct {
		Events []events.Event `json:"events"`
	}
	if err := json.NewDecoder(response.Body).Decode(&answer); err != nil {
		return nil, fmt.Errorf("peer %s gave an unreadable answer: %w", peer, err)
	}
	return answer.Events, nil
}

/*
FetchFile asks a peer for the bytes of a file a message in a room carries.

The body is returned unread, for the caller to bound and check: the peer is
trusted to deliver bytes, never to say what they are — that is the digest the
author's server signed, which the caller compares them against.
*/
func (s *Sender) FetchFile(ctx context.Context, peer, slug, digest string) (io.ReadCloser, error) {
	if allowed, why := s.trust.Allows(peer); !allowed {
		return nil, fmt.Errorf("refusing to reach %s: %w", peer, why)
	}

	endpoint, err := peerURL(peer, fmt.Sprintf("/api/federation/rooms/%s/files/%s", slug, digest))
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build the request: %w", err)
	}
	request.Header.Set("TDC-Origin-Server", s.origin)
	if err := Sign(request, nil, s.keyring, s.now()); err != nil {
		return nil, fmt.Errorf("sign the request: %w", err)
	}

	response, err := s.files.Do(request)
	if err != nil {
		return nil, fmt.Errorf("reach %s: %w", peer, err)
	}
	if response.StatusCode != http.StatusOK {
		reason, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		_ = response.Body.Close()
		return nil, fmt.Errorf("peer %s refused the file: %s: %s",
			peer, response.Status, strings.TrimSpace(string(reason)))
	}
	return response.Body, nil
}

/*
JoinRoom asks a room's home server to admit one of our users.

An ask, not a statement. The home server applies its own rules and may refuse;
that refusal is the answer, and forwarding it to the user unchanged is more
useful than this server guessing why.
*/
func (s *Sender) JoinRoom(ctx context.Context, peer, slug, member string) error {
	return s.memberCall(ctx, peer, "/api/federation/rooms/"+slug+"/join", member)
}

// LeaveRoom tells a room's home server that one of our users has left.
func (s *Sender) LeaveRoom(ctx context.Context, peer, slug, member string) error {
	return s.memberCall(ctx, peer, "/api/federation/rooms/"+slug+"/leave", member)
}

func (s *Sender) memberCall(ctx context.Context, peer, path, member string) error {
	response, err := s.Send(ctx, peer, path, map[string]any{"member": member})
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		reason, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return fmt.Errorf("%s refused: %s: %s",
			peer, response.Status, strings.TrimSpace(string(reason)))
	}
	return nil
}

// PostedEvent is what a home server says it did with a forwarded message.
type PostedEvent struct {
	ID       string `json:"id"`
	Sequence int64  `json:"sequence"`
	Sender   string `json:"sender"`
}

/*
PostMessage forwards one of our users' messages to a room's home server.

The home server assigns the sequence and signs the event, and returns both —
this server never numbers anything in a room it does not own, which is the
whole reason the room has one order everybody agrees on.
*/
func (s *Sender) PostMessage(
	ctx context.Context,
	peer, slug, roomAddress, sender string,
	content json.RawMessage,
) (PostedEvent, error) {
	/*
		Signed here, by the sender's own server, before it goes anywhere.

		This is what stops the room's home server inventing messages from our
		users. It orders the room and could drop or reorder anything, which we
		accept — but authorship is ours to attest, and an event it did not
		sign is one it cannot fabricate.

		No sequence: the home server assigns that, and it is deliberately not
		part of what is signed, so it can be added without invalidating this.
	*/
	nonce, err := events.NewNonce()
	if err != nil {
		return PostedEvent{}, err
	}

	event := events.Event{
		Room:      roomAddress,
		Sender:    sender,
		Origin:    s.originHost(),
		Type:      events.TypeMessageSent,
		CreatedAt: events.Now(),
		Nonce:     nonce,
		Content:   content,
	}
	if err := s.keyring.Sign(&event); err != nil {
		return PostedEvent{}, fmt.Errorf("sign the event: %w", err)
	}

	response, err := s.Send(ctx, peer, "/api/federation/rooms/"+slug+"/post",
		map[string]any{"event": event})
	if err != nil {
		return PostedEvent{}, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusCreated {
		reason, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return PostedEvent{}, fmt.Errorf("%s refused the message: %s: %s",
			peer, response.Status, strings.TrimSpace(string(reason)))
	}

	var posted PostedEvent
	if err := json.NewDecoder(response.Body).Decode(&posted); err != nil {
		return PostedEvent{}, fmt.Errorf("%s gave an unreadable answer: %w", peer, err)
	}
	return posted, nil
}

// originHost is this server's identity as events record it: a host, the same
// form a room's home_server and a member's address use.
func (s *Sender) originHost() string {
	host, err := Host(s.origin)
	if err != nil {
		return s.origin
	}
	return host
}

// PublishedRoom is a room a peer offers to servers browsing it.
type PublishedRoom struct {
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	Topic     string `json:"topic"`
	Address   string `json:"address"`
	Federated bool   `json:"federated"`
}

/*
BrowseRooms asks a peer what rooms it publishes to us.

Signed like every other federated request, which is what lets the peer apply
its own visibility rules to a server it has actually identified. A peer that
does not list us answers with an empty list rather than a refusal, so an empty
result means "nothing for you" without saying whether that is because there is
nothing or because we are not admitted.
*/
func (s *Sender) BrowseRooms(ctx context.Context, peer string) ([]PublishedRoom, error) {
	if allowed, why := s.trust.Allows(peer); !allowed {
		return nil, fmt.Errorf("refusing to reach %s: %w", peer, why)
	}

	endpoint, err := peerURL(peer, "/api/federation/rooms")
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build the request: %w", err)
	}
	request.Header.Set("TDC-Origin-Server", s.origin)
	if err := Sign(request, nil, s.keyring, s.now()); err != nil {
		return nil, fmt.Errorf("sign the request: %w", err)
	}

	response, err := s.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("reach %s: %w", peer, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		reason, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return nil, fmt.Errorf("peer %s refused the request: %s: %s",
			peer, response.Status, strings.TrimSpace(string(reason)))
	}

	var answer struct {
		Rooms []PublishedRoom `json:"rooms"`
	}
	if err := json.NewDecoder(response.Body).Decode(&answer); err != nil {
		return nil, fmt.Errorf("peer %s gave an unreadable answer: %w", peer, err)
	}
	return answer.Rooms, nil
}
