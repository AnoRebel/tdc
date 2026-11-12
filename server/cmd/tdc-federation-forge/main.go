/*
Submits forged events to a peer's federation endpoint, and requires refusal.

The demonstration's adversary. A correctly built server never produces these,
so the only way to show they are refused is to build one that does — and to
sign the *request* properly while forging the *event* inside it, because a
request that failed at the transport would prove nothing about content
verification.

Four forgeries, each aimed at one check:

  - a broken signature, for the cryptography;
  - an event signed by us claiming to be from somebody on another server,
    for the impersonation check;
  - an event about a room this delivery is not for;
  - an event claiming a home server that is not the room's.
*/
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/federation"
)

func main() {
	peer := flag.String("peer", "", "the server to submit forged events to")
	room := flag.String("room", "", "the room slug to claim")
	home := flag.String("home", "", "the room's real home server")
	flag.Parse()

	if *peer == "" || *room == "" || *home == "" {
		fmt.Fprintln(os.Stderr, "--peer, --room and --home are required")
		os.Exit(2)
	}

	if err := run(*peer, *room, *home); err != nil {
		fmt.Fprintf(os.Stderr, "\nFAILED: %v\n", err)
		os.Exit(1)
	}
}

func run(peer, room, home string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	key, err := events.NewKeyPair("forge-key")
	if err != nil {
		return err
	}
	keyring := events.NewKeyring(key)

	// A real server, so the peer can fetch our keys: the request signature has
	// to verify or every refusal below would be a transport refusal instead.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	origin := "http://" + listener.Addr().String()

	mux := http.NewServeMux()
	mux.HandleFunc(federation.WellKnownPath, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(federation.Document{
			CanonicalAddress: origin,
			Keys:             keyring.Published(),
			Versions:         federation.SupportedVersions(),
			Enabled:          true,
		})
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(listener) }()
	defer func() { _ = server.Close() }()

	roomAddress := room + "@" + home

	forgeries := []struct {
		describe string
		event    events.Event
		breakIt  func(*events.Event)
	}{
		{
			describe: "a broken signature",
			event: forged("somebody@"+home, home, roomAddress,
				"A message nobody ever sent, with a forged signature."),
			// Altered after signing, so the signature covers different bytes.
			breakIt: func(e *events.Event) {
				e.Content = json.RawMessage(`{"body":"A message nobody ever sent, tampered after signing."}`)
			},
		},
		{
			describe: "signed for another server's user",
			event: forged("victim@"+home, originHost(origin), roomAddress,
				"A forged message attributed to somebody else."),
		},
		{
			describe: "about a different room",
			event: forged("somebody@"+home, home, "somewhere-else@"+home,
				"A forged message aimed at another room."),
		},
		{
			describe: "claiming the wrong home server",
			event: forged("somebody@"+originHost(origin), originHost(origin), roomAddress,
				"A forged message from a server that does not own this room."),
		},
	}

	for _, forgery := range forgeries {
		event := forgery.event
		if err := keyring.Sign(&event); err != nil {
			return fmt.Errorf("%s: %w", forgery.describe, err)
		}
		if forgery.breakIt != nil {
			forgery.breakIt(&event)
		}

		status, reason, err := submit(ctx, keyring, origin, peer, room, event)
		if err != nil {
			return fmt.Errorf("%s: %w", forgery.describe, err)
		}
		if status == http.StatusOK {
			return fmt.Errorf("%s was ACCEPTED, which it must not be", forgery.describe)
		}
		fmt.Printf("  %-38s refused: %s\n", forgery.describe, reason)
	}

	return nil
}

// forged builds an event with a sequence far ahead of anything real, so that
// if it were ever stored it would be conspicuous rather than blending in.
func forged(sender, origin, room, body string) events.Event {
	nonce, _ := events.NewNonce()
	return events.Event{
		Room:      room,
		Sender:    sender,
		Origin:    origin,
		Type:      events.TypeMessageSent,
		Sequence:  9001,
		CreatedAt: events.Now(),
		Nonce:     nonce,
		Content:   json.RawMessage(`{"body":` + quote(body) + `}`),
	}
}

func quote(s string) string {
	encoded, _ := json.Marshal(s)
	return string(encoded)
}

func originHost(origin string) string {
	host, _ := federation.Host(origin)
	return host
}

func submit(
	ctx context.Context,
	keyring *events.Keyring,
	origin, peer, room string,
	event events.Event,
) (int, string, error) {
	body, err := json.Marshal(map[string]any{"events": []events.Event{event}})
	if err != nil {
		return 0, "", err
	}

	endpoint := strings.TrimRight(peer, "/") + "/api/federation/rooms/" + room + "/events"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, "", err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("TDC-Origin-Server", origin)

	// The request itself is signed correctly. Only the event inside is forged,
	// so a refusal is content verification rather than transport.
	if err := federation.Sign(request, body, keyring, time.Now()); err != nil {
		return 0, "", err
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return 0, "", err
	}
	defer response.Body.Close()

	reason, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
	return response.StatusCode, strings.TrimSpace(string(reason)), nil
}
