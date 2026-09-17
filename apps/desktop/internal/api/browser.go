package api

import (
	"errors"
	"net/url"
	"strings"
)

/*
Handing a sign-in to the system browser.

A sign-in through a provider does not happen in this window. It happens in the
browser somebody already trusts, where the address bar shows whose page is
asking for the password and the password manager knows the site. Inside a
webview none of that is true: the address is not shown, so a page that looks
like the provider is indistinguishable from the provider, and this application
would be asking somebody to type a credential it could read.

So the URL goes out to the system browser and this client waits. What it holds
meanwhile is the state the server issued, which is how it collects the outcome
of its own sign-in afterwards — the callback lands on the server, not here.
*/

// Opener opens a URL outside this application.
//
// An interface rather than the Wails browser manager directly, so this package
// stays testable without a desktop session and without a browser installed.
type Opener interface {
	OpenURL(url string) error
}

// ErrNoBrowser reports that nothing was wired to open a URL. It should not
// happen in the built application, where main provides one.
var ErrNoBrowser = errors.New("this client cannot open a browser")

// ErrUnsafeSignInURL reports a sign-in URL that is not one to hand a browser.
var ErrUnsafeSignInURL = errors.New("that sign-in address cannot be opened")

/*
UseOpener wires how URLs leave this application.

A function rather than a method, so the binding generator does not export it to
the frontend: it is wiring that main does once at startup, and an interface
parameter is not something the webview could pass anyway. Every method on
Service is offered to the webview, and this must not be one of them.
*/
func UseOpener(service *Service, opener Opener) {
	service.mu.Lock()
	defer service.mu.Unlock()
	service.opener = opener
}

/*
OpenSignIn hands a provider's authorization URL to the system browser.

The URL is checked before it is opened even though it came from this client's
own server: it is a value that has crossed the network, an operator sets the
endpoint it is built from, and handing an unchecked string to the thing that
opens whatever it is given is not a place to be trusting. Only https, and only
a URL — a file:// or a javascript: would otherwise open something local.
*/
func (s *Service) OpenSignIn(address string) error {
	s.mu.RLock()
	opener := s.opener
	s.mu.RUnlock()

	if opener == nil {
		return ErrNoBrowser
	}

	parsed, err := url.Parse(strings.TrimSpace(address))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return ErrUnsafeSignInURL
	}

	return opener.OpenURL(parsed.String())
}
