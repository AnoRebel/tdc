package main

import (
	"testing"

	"github.com/AnoRebel/tdc/server/internal/config"
)

/*
A server's name inside an address is a host, never a URL.

Addresses are room@server and user@server. An operator who configured
"https://tdc.example.org" and got "general@https://tdc.example.org" would see
it in every address the server produces, and in federation it is worse than
cosmetic: two servers computing an address for the same room must reach the
same string, or each rejects the other's events as belonging to a room it has
never heard of. That is how this was found.
*/
func TestTheServerNamesItselfByHost(t *testing.T) {
	cases := map[string]string{
		"https://tdc.example.org":      "tdc.example.org",
		"https://tdc.example.org/":     "tdc.example.org",
		"http://a.localhost:8111":      "a.localhost",
		"tdc.example.org":              "tdc.example.org",
		"https://TDC.Example.ORG:8443": "tdc.example.org",
	}

	for configured, want := range cases {
		cfg := config.Config{}
		cfg.Server.CanonicalAddress = configured

		if got := serverName(cfg); got != want {
			t.Errorf("serverName(%q) = %q, want %q", configured, got, want)
		}
	}
}

// A server with no canonical address still needs a name, or every address it
// produces would end in "@".
func TestAnUnconfiguredServerStillHasAName(t *testing.T) {
	if got := serverName(config.Config{}); got != "localhost" {
		t.Errorf("serverName of an unconfigured server = %q, want localhost", got)
	}
}
