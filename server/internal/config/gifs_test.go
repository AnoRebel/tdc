package config_test

import (
	"strings"
	"testing"

	"github.com/AnoRebel/tdc/server/internal/config"
)

/*
TestHalfAGifConfigurationIsRefused is why validation exists for this at all.

A provider without a key, or a key without a provider, is somebody who meant to
switch searching on. Leaving it silently off would look like a broken feature
rather than an unconfigured one, and the difference is expensive to discover
from an empty picker.
*/
func TestHalfAGifConfigurationIsRefused(t *testing.T) {
	tests := map[string]struct {
		env    map[string]string
		expect string
	}{
		"a key with no provider": {
			env:    map[string]string{"TDC_GIFS_API_KEY": "abc123"},
			expect: "gifs.provider",
		},
		"a provider with no key": {
			env:    map[string]string{"TDC_GIFS_PROVIDER": "tenor"},
			expect: "gifs.api_key",
		},
		"a provider nobody supports": {
			env: map[string]string{
				"TDC_GIFS_PROVIDER": "someotherlibrary",
				"TDC_GIFS_API_KEY":  "abc123",
			},
			expect: "gifs.provider",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			for key, value := range test.env {
				t.Setenv(key, value)
			}
			// The database URL is required, so it is set for every case.
			t.Setenv("TDC_DATABASE_URL", "postgres://tdc@localhost/tdc")

			_, err := config.Load("")
			if err == nil {
				t.Fatalf("%s was accepted", name)
			}
			if !strings.Contains(err.Error(), test.expect) {
				t.Errorf("the refusal is %q; expected it to name %s", err, test.expect)
			}
		})
	}
}

// TestNoGifConfigurationIsFine covers the default, which is the case almost
// every server runs in.
func TestNoGifConfigurationIsFine(t *testing.T) {
	t.Setenv("TDC_DATABASE_URL", "postgres://tdc@localhost/tdc")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("a configuration with no gif settings was refused: %v", err)
	}
	if cfg.GIFs.Provider != "" || cfg.GIFs.APIKey != "" {
		t.Error("gif search is configured by default; it must be opt-in")
	}
}
