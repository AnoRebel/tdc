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
