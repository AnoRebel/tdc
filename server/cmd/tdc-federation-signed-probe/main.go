/*
Proves what the signed transport refuses, against a real peer.

The happy path is demonstrated by the two servers pinging each other. What they
cannot easily show from the outside is the refusals, because a correctly built
server never produces a replayed or mis-addressed request. This stands up a
server of its own — a real key, a real discovery document — and deliberately
sends the requests a well-behaved peer never would.
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
	peer := flag.String("peer", "", "the peer to send signed requests to")
	other := flag.String("other", "", "a different server, to prove a signature cannot be redirected")
	self := flag.String("self", "", "unused; kept so the caller's flags stay stable")
	flag.Parse()
	_ = self

	if *peer == "" {
		fmt.Fprintln(os.Stderr, "--peer is required")
		os.Exit(2)
	}

	if err := run(*peer, *other); err != nil {
		fmt.Fprintf(os.Stderr, "\nFAILED: %v\n", err)
		os.Exit(1)
	}
}

func run(peer, other string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	key, err := events.NewKeyPair("probe-key")
	if err != nil {
		return err
	}
	keyring := events.NewKeyring(key)

	// A real server, so the peer can fetch our keys and verify what we send.
	// Without this the peer would refuse everything for the uninteresting
	// reason that it has never heard of us.
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

	fmt.Printf("  the probe is a server too, at %s\n", origin)

	// A signed ping, to establish that this probe is accepted at all. Without
	// it, every refusal below could be the peer refusing the probe itself.
	if err := expect(ctx, keyring, origin, peer, peer, time.Now(), http.StatusOK,
		"a correctly signed ping"); err != nil {
		return err
	}

	// Replayed after its window: signed far enough in the past that it has
	// already expired by the time it arrives.
	stale := time.Now().Add(-2 * federation.SignatureLifetime)
	if err := expect(ctx, keyring, origin, peer, peer, stale, http.StatusUnauthorized,
		"a ping replayed after its window"); err != nil {
		return err
	}

	// Signed naming a different destination, delivered here.
	if other != "" && other != peer {
		if err := expect(ctx, keyring, origin, peer, other, time.Now(), http.StatusUnauthorized,
			"a ping signed for another server"); err != nil {
			return err
		}
	}

	// And with no signature at all.
	if err := expectUnsigned(ctx, origin, peer); err != nil {
		return err
	}

	return nil
}

/*
expect sends one ping and requires a particular answer.

signedFor is the address the signature names as its destination; deliveredTo is
where the request actually goes. They are the same for an honest request and
differ for the redirection case, which is the whole point of separating them.
*/
func expect(
	ctx context.Context,
	keyring *events.Keyring,
	origin, deliveredTo, signedFor string,
	at time.Time,
	want int,
	describe string,
) error {
	body, _ := json.Marshal(map[string]any{"ping": true})

	// Built against the address the signature will name...
	signed, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(signedFor, "/")+"/api/federation/ping", bytes.NewReader(body))
	if err != nil {
		return err
	}
	signed.Header.Set("Content-Type", "application/json")
	signed.Header.Set("TDC-Origin-Server", origin)
	if err := federation.Sign(signed, body, keyring, at); err != nil {
		return err
	}

	// ...then delivered wherever we actually mean to send it, carrying those
	// headers unchanged.
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(deliveredTo, "/")+"/api/federation/ping", bytes.NewReader(body))
	if err != nil {
		return err
	}
	for name, values := range signed.Header {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("%s: %w", describe, err)
	}
	defer response.Body.Close()

	reason, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
	if response.StatusCode != want {
		return fmt.Errorf("%s: got %d, expected %d: %s",
			describe, response.StatusCode, want, strings.TrimSpace(string(reason)))
	}

	fmt.Printf("  %-42s %s %s\n", describe, response.Status, strings.TrimSpace(string(reason)))
	return nil
}

func expectUnsigned(ctx context.Context, origin, peer string) error {
	body, _ := json.Marshal(map[string]any{"ping": true})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(peer, "/")+"/api/federation/ping", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("TDC-Origin-Server", origin)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	reason, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
	if response.StatusCode != http.StatusUnauthorized {
		return fmt.Errorf("an unsigned ping: got %d, expected 401", response.StatusCode)
	}

	fmt.Printf("  %-42s %s %s\n", "an unsigned ping", response.Status, strings.TrimSpace(string(reason)))
	return nil
}
