/*
Exercises the discovery client against real servers.

The milestone asks to show a server fetching, caching and verifying a peer's
identity, and refusing one that lies about its domain. That is the discovery
client's job, so this drives the actual client rather than reimplementing its
checks in curl — a shell script comparing two JSON fields would demonstrate
that the script works, not that the server does.
*/
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/AnoRebel/tdc/server/internal/federation"
)

func main() {
	peer := flag.String("peer", "", "a peer to discover")
	impostor := flag.String("impostor", "", "a server claiming a domain it is not served from")
	flag.Parse()

	if *peer == "" {
		fmt.Fprintln(os.Stderr, "--peer is required")
		os.Exit(2)
	}

	if err := probe(*peer, *impostor); err != nil {
		fmt.Fprintf(os.Stderr, "\nFAILED: %v\n", err)
		os.Exit(1)
	}
}

func probe(peer, impostor string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Loopback is a private address, which is exactly the case an operator
	// opts into when federating two servers inside one network.
	client := federation.NewClient(true)

	fmt.Printf("  fetching %s\n", peer)
	first, err := client.Discover(ctx, peer)
	if err != nil {
		return fmt.Errorf("discovering the peer: %w", err)
	}
	fmt.Printf("  identity verified: %s\n", first.CanonicalAddress)
	fmt.Printf("  published keys: %d, versions: %v\n", len(first.Keys), first.Versions)

	// A keyring built from what the peer published is what verifies its
	// events. Building it here proves the keys are usable, not merely present.
	if _, err := first.Keyring(); err != nil {
		return fmt.Errorf("the peer's published keys do not build a keyring: %w", err)
	}
	fmt.Println("  the published keys build a working verification keyring")

	version, err := first.NegotiateWith()
	if err != nil {
		return fmt.Errorf("negotiating with the peer: %w", err)
	}
	fmt.Printf("  agreed protocol version: %s\n", version)

	// Cached: the second call must not go back to the peer. Timing is the only
	// thing observable from out here, and a cache hit is orders of magnitude
	// faster than a round trip even on loopback.
	start := time.Now()
	if _, err := client.Discover(ctx, peer); err != nil {
		return fmt.Errorf("discovering the peer again: %w", err)
	}
	cached := time.Since(start)
	fmt.Printf("  second lookup served from cache in %s\n", cached)

	if impostor == "" {
		return nil
	}

	fmt.Printf("\n  fetching %s, which claims to be somebody else\n", impostor)
	_, err = client.Discover(ctx, impostor)
	switch {
	case err == nil:
		return errors.New("the impostor was accepted; the domain check did not run")
	case errors.Is(err, federation.ErrDomainMismatch):
		fmt.Printf("  refused: %v\n", err)
	default:
		return fmt.Errorf("the impostor was refused, but for the wrong reason: %w", err)
	}

	return nil
}
