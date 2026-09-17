package api

import (
	"errors"
	"testing"
)

// recordingOpener remembers what it was asked to open, and opens nothing.
type recordingOpener struct{ opened []string }

func (r *recordingOpener) OpenURL(url string) error {
	r.opened = append(r.opened, url)
	return nil
}

/*
TestOnlyAnHTTPSSignInIsOpened.

The URL comes from this client's own server, but it has crossed the network and
an operator sets the endpoint it is built from. Handing an unchecked string to
the thing that opens whatever it is given is not a place to be trusting: a
file:// or a javascript: would open something local rather than a provider.
*/
func TestOnlyAnHTTPSSignInIsOpened(t *testing.T) {
	refused := map[string]string{
		"a local file":        "file:///etc/passwd",
		"script in a url":     "javascript:alert(1)",
		"plain http":          "http://provider.test/authorize",
		"no host":             "https://",
		"not a url at all":    "authorize",
		"nothing":             "",
		"a scheme of its own": "tdc://open",
	}

	for name, address := range refused {
		t.Run(name, func(t *testing.T) {
			opener := &recordingOpener{}
			service := New()
			UseOpener(service, opener)

			if err := service.OpenSignIn(address); !errors.Is(err, ErrUnsafeSignInURL) {
				t.Errorf("opening %q gave %v, want a refusal", address, err)
			}
			if len(opener.opened) != 0 {
				t.Errorf("%q reached the browser", address)
			}
		})
	}
}

// TestAnHTTPSSignInReachesTheBrowser, which is the whole point of the above.
func TestAnHTTPSSignInReachesTheBrowser(t *testing.T) {
	opener := &recordingOpener{}
	service := New()
	UseOpener(service, opener)

	const address = "https://provider.test/authorize?client_id=abc&state=xyz"
	if err := service.OpenSignIn(address); err != nil {
		t.Fatalf("opening a provider: %v", err)
	}
	if len(opener.opened) != 1 || opener.opened[0] != address {
		t.Errorf("the browser was given %v, want the url as issued", opener.opened)
	}
}

/*
TestWithoutABrowserTheSignInIsRefused.

Rather than falling back to this window, which is the thing the system browser
exists to avoid here. A client that cannot open one says so.
*/
func TestWithoutABrowserTheSignInIsRefused(t *testing.T) {
	if err := New().OpenSignIn("https://provider.test/authorize"); !errors.Is(err, ErrNoBrowser) {
		t.Errorf("opening with no browser wired gave %v, want a refusal", err)
	}
}
