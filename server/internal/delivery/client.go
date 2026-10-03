package delivery

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/AnoRebel/tdc/server/internal/unfurl"
)

/*
Sending a delivery to somebody else's endpoint.

The address is chosen by whoever set up the webhook or registered the program,
which makes this — like link previews — an outbound request somebody outside
the server steers. The same connect-time guard link previews use applies here,
reused rather than written again: the address is judged after DNS has resolved
it and immediately before the connection is made, so a name that resolves to a
public address for a check and a private one a moment later gains nothing.

Redirects are never followed. A receiver that answers 3xx has answered, and
following it would let a public endpoint point this server anywhere — the
guard would still refuse a private address, but a redirect is a second
destination nobody approved, and there is no receiver that needs one.
*/

// ErrPrivateDestination reports an endpoint resolving to an address this
// server will not reach.
var ErrPrivateDestination = errors.New("that address is on a private or reserved network")

// ErrUnusableURL reports an endpoint that is not an http(s) URL naming a host.
var ErrUnusableURL = errors.New("an endpoint must be an http or https URL naming a host")

/*
MaxResponseBytes bounds how much of a receiver's answer is read.

Only the status matters; a little of the body is kept for the delivery history
so an owner can see what their receiver said when it refused. Reading more
would let a receiver make this server hold whatever it chose to send.
*/
const MaxResponseBytes = 1024

// Outcome is what happened when a delivery was attempted.
type Outcome struct {
	// Status is the receiver's HTTP status, or zero when none was received.
	Status int
	// Delivered is true for a 2xx answer, which is the only kind that counts.
	Delivered bool
	// Error describes a failure to get an answer at all — a refused
	// connection, a timeout, a refused destination.
	Error string
	// Response is the start of what the receiver said.
	Response string
	Duration time.Duration
}

// Client sends deliveries.
type Client struct {
	http *http.Client
}

/*
NewClient builds a client whose every request is bounded by timeout.

allowPrivate is asked on every connection rather than once, so an operator
changing the setting takes effect without a restart. Off is the default and
the safe reading; an operator whose receiver genuinely lives on the same
private network turns it on deliberately.
*/
func NewClient(timeout time.Duration, allowPrivate func() bool) *Client {
	plain := &net.Dialer{Timeout: timeout}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if allowPrivate != nil && allowPrivate() {
				return plain.DialContext(ctx, network, address)
			}
			return unfurl.DialContext(ctx, network, address)
		},
		// No connection held open between deliveries: a receiver that has gone
		// quiet should not keep a socket of this server's, and deliveries to
		// one receiver are seconds apart at the busiest, not milliseconds.
		DisableKeepAlives:     true,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		// A proxy from the environment would be a way around the guard: the
		// connection would be to the proxy, and the destination its business.
		Proxy: nil,
	}

	return &Client{http: &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

// Send posts a delivery and reports what happened. It never returns an error:
// every way a delivery can fail is an outcome to record, not a fault.
func (c *Client) Send(ctx context.Context, endpoint string, headers http.Header, body []byte) Outcome {
	started := time.Now()
	outcome := func(result Outcome) Outcome {
		result.Duration = time.Since(started)
		return result
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return outcome(Outcome{Error: fmt.Sprintf("the endpoint cannot be used: %v", err)})
	}
	for name, values := range headers {
		request.Header[name] = values
	}
	request.Header.Set("User-Agent", "TDC-Delivery/1")

	response, err := c.http.Do(request)
	if err != nil {
		return outcome(Outcome{Error: describe(err)})
	}
	defer func() { _ = response.Body.Close() }()

	said, _ := io.ReadAll(io.LimitReader(response.Body, MaxResponseBytes))
	// Drained up to a bound and no further, so a receiver cannot keep this
	// connection busy by streaming.
	_, _ = io.CopyN(io.Discard, response.Body, 64*1024)

	return outcome(Outcome{
		Status:    response.StatusCode,
		Delivered: response.StatusCode >= 200 && response.StatusCode < 300,
		Response:  strings.ToValidUTF8(string(said), "?"),
	})
}

// describe turns a transport failure into something an owner can act on.
func describe(err error) string {
	var timeout interface{ Timeout() bool }
	switch {
	case errors.As(err, &timeout) && timeout.Timeout():
		return "the endpoint did not answer in time"
	case strings.Contains(err.Error(), "not a public address"):
		return "the endpoint resolves to a private or reserved address, which deliveries do not reach"
	case strings.Contains(err.Error(), "connection refused"):
		return "the endpoint refused the connection"
	default:
		return err.Error()
	}
}

/*
CheckDestination refuses an endpoint before anything is stored.

The connect-time guard is what actually protects the server; this is what lets
somebody setting up a webhook be told at once that the address will never
work, rather than finding out from a delivery history full of failures. Every
address the name resolves to must be allowed, since the one a later connection
uses is not ours to choose.
*/
func CheckDestination(ctx context.Context, raw string, allowPrivate bool) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return ErrUnusableURL
	}
	if parsed.User != nil {
		// A credential in the URL would be stored and shown in every listing.
		return fmt.Errorf("%w: put credentials in a header, not the address", ErrUnusableURL)
	}
	if allowPrivate {
		return nil
	}

	host := parsed.Hostname()
	if literal, err := netip.ParseAddr(host); err == nil {
		if !unfurl.AddressAllowed(literal) {
			return fmt.Errorf("%w: %s", ErrPrivateDestination, literal)
		}
		return nil
	}

	lookup, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupNetIP(lookup, "ip", host)
	if err != nil {
		return fmt.Errorf("%w: %s does not resolve", ErrUnusableURL, host)
	}
	for _, address := range addresses {
		if !unfurl.AddressAllowed(address) {
			return fmt.Errorf("%w: %s resolves to %s", ErrPrivateDestination, host, address)
		}
	}
	return nil
}
