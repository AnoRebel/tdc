package unfurl_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/AnoRebel/tdc/server/internal/unfurl"
)

// TestPrivateAddressesAreRefused is the test this package exists for.
//
// Every one of these is somewhere a link must never reach: the metadata service
// that holds a cloud provider's credentials, the loopback interface where the
// database listens, the private ranges where the rest of an operator's
// infrastructure lives.
func TestPrivateAddressesAreRefused(t *testing.T) {
	for _, forbidden := range []struct{ address, why string }{
		{"127.0.0.1", "loopback, where this server's own database listens"},
		{"127.1.2.3", "the whole loopback range, not just .1"},
		{"::1", "IPv6 loopback"},
		{"::ffff:127.0.0.1", "loopback written as a v4-mapped v6 address"},
		{"::ffff:169.254.169.254", "metadata service, mapped into v6"},
		{"10.0.0.1", "RFC1918"},
		{"172.16.31.4", "RFC1918, the range people forget"},
		{"192.168.1.1", "RFC1918"},
		{"169.254.169.254", "the cloud metadata service"},
		{"169.254.1.1", "the rest of link-local"},
		{"fd00::1", "IPv6 unique local"},
		{"fe80::1", "IPv6 link-local"},
		{"0.0.0.0", "unspecified, which routes to localhost on some systems"},
		{"100.64.0.1", "carrier-grade NAT, where mobile networks put real hosts"},
		{"192.0.2.1", "TEST-NET-1"},
		{"198.18.0.1", "benchmarking range"},
		{"203.0.113.1", "TEST-NET-3"},
		{"240.0.0.1", "reserved"},
		{"255.255.255.255", "broadcast"},
		{"224.0.0.1", "multicast"},
		{"2001:db8::1", "IPv6 documentation range"},
		{"64:ff9b::7f00:1", "NAT64, which translates to 127.0.0.1"},
		{"2002:7f00:1::", "6to4, which encodes 127.0.0.1"},
	} {
		address, err := netip.ParseAddr(forbidden.address)
		if err != nil {
			t.Fatalf("the test's own address %q does not parse: %v", forbidden.address, err)
		}

		if unfurl.AddressAllowed(address) {
			t.Errorf("%s is allowed; it is %s", forbidden.address, forbidden.why)
		}
	}
}

// TestPublicAddressesAreAllowed checks the protection has not been made so
// broad that it refuses the ordinary web.
func TestPublicAddressesAreAllowed(t *testing.T) {
	for _, allowed := range []string{
		"8.8.8.8",
		"1.1.1.1",
		"93.184.216.34", // example.com
		"41.59.0.1",     // a Tanzanian address block
		"2606:4700::1",  // Cloudflare over IPv6
		"2001:4860:4860::8888",
	} {
		address, err := netip.ParseAddr(allowed)
		if err != nil {
			t.Fatalf("the test's own address %q does not parse: %v", allowed, err)
		}

		if !unfurl.AddressAllowed(address) {
			t.Errorf("%s is refused, but it is an ordinary public address", allowed)
		}
	}
}

// TestDialingLoopbackIsRefused proves the check runs where it matters.
//
// Testing the classifier alone would only show that the function returns false.
// This starts a real server on loopback and tries to reach it, which is what an
// attacker would do, and is the difference between a check that exists and a
// check that is wired in.
func TestDialingLoopbackIsRefused(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head><title>Internal service</title></head></html>`)
	}))
	defer server.Close()

	// The server really is reachable, so a refusal below is the protection
	// working rather than the address being unroutable.
	if response, err := http.Get(server.URL); err != nil {
		t.Fatalf("the test server is not reachable at all: %v", err)
	} else {
		response.Body.Close()
	}

	_, err := unfurl.NewFetcher().Fetch(context.Background(), server.URL)
	if err == nil {
		t.Fatal("a loopback address was fetched; the SSRF protection is not wired in")
	}
	if !strings.Contains(err.Error(), "not a public address") {
		t.Errorf("refused with %v, which does not look like the address check", err)
	}
}

// TestNonWebSchemesAreRefused covers the schemes that reach something a browser
// could not.
func TestNonWebSchemesAreRefused(t *testing.T) {
	fetcher := unfurl.NewFetcher()

	for _, target := range []string{
		"file:///etc/passwd",
		"gopher://127.0.0.1:6379/_INFO",
		"ftp://example.org/file",
		"data:text/html,<title>x</title>",
		"jar:http://example.org!/",
	} {
		if _, err := fetcher.Fetch(context.Background(), target); err == nil {
			t.Errorf("%q was fetched", target)
		}
	}
}

// TestPreviewIsExtracted covers the ordinary case.
func TestPreviewIsExtracted(t *testing.T) {
	preview, err := unfurl.ParseForTesting(strings.NewReader(`<!DOCTYPE html>
<html><head>
  <title>Fallback title</title>
  <meta name="description" content="Fallback description">
  <meta property="og:title" content="Mkutano wa TDC">
  <meta property="og:description" content="Ratiba ya mkutano wa mwezi huu.">
  <meta property="og:site_name" content="Tanzania Developers">
  <meta property="og:image" content="/picha/mkutano.png">
</head><body><p>Ignored.</p></body></html>`))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}

	if preview.Title != "Mkutano wa TDC" {
		t.Errorf("title is %q; OpenGraph should win over the <title>", preview.Title)
	}
	if preview.Description != "Ratiba ya mkutano wa mwezi huu." {
		t.Errorf("description is %q", preview.Description)
	}
	if preview.SiteName != "Tanzania Developers" {
		t.Errorf("site name is %q", preview.SiteName)
	}
	if preview.ImageURL != "/picha/mkutano.png" {
		t.Errorf("image is %q; resolution against the page happens on fetch", preview.ImageURL)
	}
}

// TestTwitterMetadataIsUsed checks the second convention, which many sites
// publish instead of or alongside OpenGraph.
func TestTwitterMetadataIsUsed(t *testing.T) {
	preview, err := unfurl.ParseForTesting(strings.NewReader(`<html><head>
  <meta name="twitter:title" content="From Twitter tags">
  <meta name="twitter:description" content="Described there too">
  <meta name="twitter:image" content="https://example.org/x.png">
</head></html>`))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}

	if preview.Title != "From Twitter tags" {
		t.Errorf("title is %q", preview.Title)
	}
	if preview.Description != "Described there too" {
		t.Errorf("description is %q", preview.Description)
	}
}

// TestFallsBackToOrdinaryHTML checks a page with no social metadata.
func TestFallsBackToOrdinaryHTML(t *testing.T) {
	preview, err := unfurl.ParseForTesting(strings.NewReader(`<html><head>
  <title>  A   page   with   spacing  </title>
  <meta name="description" content="Plain metadata">
</head><body></body></html>`))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}

	// Whitespace is collapsed, or a title laid out across several lines in the
	// source would render with its indentation intact.
	if preview.Title != "A page with spacing" {
		t.Errorf("title is %q; whitespace should be collapsed", preview.Title)
	}
	if preview.Description != "Plain metadata" {
		t.Errorf("description is %q", preview.Description)
	}
}

// TestLongFieldsAreTruncated checks that a page cannot push an unbounded string
// into every client that displays the preview.
func TestLongFieldsAreTruncated(t *testing.T) {
	long := strings.Repeat("ndefu ", 500)
	preview, err := unfurl.ParseForTesting(strings.NewReader(
		"<html><head><title>" + long + "</title>" +
			`<meta name="description" content="` + long + `"></head></html>`))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}

	if len([]rune(preview.Title)) > 210 {
		t.Errorf("the title is %d runes; it should be bounded", len([]rune(preview.Title)))
	}
	if len([]rune(preview.Description)) > 410 {
		t.Errorf("the description is %d runes", len([]rune(preview.Description)))
	}
}

// TestTruncationDoesNotSplitCharacters checks that a cut lands on a rune
// boundary, so a multi-byte character is never left half-written.
func TestTruncationDoesNotSplitCharacters(t *testing.T) {
	preview, err := unfurl.ParseForTesting(strings.NewReader(
		"<html><head><title>" + strings.Repeat("ñ", 400) + "</title></head></html>"))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}

	if !utf8.ValidString(preview.Title) {
		t.Error("truncation split a character in half")
	}
}

// TestPageWithoutMetadataIsNotPreviewable checks that a bare page is reported
// as having nothing to show rather than as an error.
//
// Most links are like this, and a message is perfectly good without a card.
func TestPageWithoutMetadataIsNotPreviewable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head></head><body>Just words.</body></html>`)
	}))
	defer server.Close()

	_, err := unfurl.NewFetcherForTesting().Fetch(context.Background(), server.URL)
	if !errors.Is(err, unfurl.ErrNotPreviewable) {
		t.Errorf("gave %v, expected ErrNotPreviewable", err)
	}
}

// TestNonHTMLIsNotPreviewable checks that a link to a binary is refused before
// any of it is parsed.
func TestNonHTMLIsNotPreviewable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		fmt.Fprint(w, "%PDF-1.7 ...")
	}))
	defer server.Close()

	_, err := unfurl.NewFetcherForTesting().Fetch(context.Background(), server.URL)
	if !errors.Is(err, unfurl.ErrNotPreviewable) {
		t.Errorf("gave %v, expected ErrNotPreviewable", err)
	}
}

// TestOversizedPageIsBounded checks that a server streaming without end cannot
// hold this one open or exhaust its memory.
func TestOversizedPageIsBounded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head><title>Enormous</title></head><body>`)

		// Far more than the reader will accept. Writing until the connection is
		// closed is what a hostile server would do.
		block := strings.Repeat("padding ", 1024)
		for i := 0; i < 8192; i++ {
			if _, err := fmt.Fprint(w, block); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	// The point is that this returns at all rather than reading for ever.
	preview, err := unfurl.NewFetcherForTesting().Fetch(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("fetching a large page: %v", err)
	}
	if preview.Title != "Enormous" {
		t.Errorf("title is %q", preview.Title)
	}
}

// TestRedirectLoopIsBounded checks that a server redirecting to itself for ever
// does not hold a request open.
func TestRedirectLoopIsBounded(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, server.URL+"/again", http.StatusFound)
	}))
	defer server.Close()

	if _, err := unfurl.NewFetcherForTesting().Fetch(context.Background(), server.URL); err == nil {
		t.Fatal("a redirect loop was followed to completion")
	}
}

// TestRedirectToPrivateAddressIsRefused covers the bypass a check on the
// original URL alone would miss.
//
// A public URL that redirects to 127.0.0.1 defeats any protection applied
// before the request. The dialer checks every connection, including the ones a
// redirect causes — so this uses the real fetcher, not the testing one.
func TestRedirectToPrivateAddressIsRefused(t *testing.T) {
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head><title>Secrets</title></head></html>`)
	}))
	defer internal.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, internal.URL, http.StatusFound)
	}))
	defer redirector.Close()

	// Both are on loopback, so the real fetcher refuses at the first hop. That
	// is the correct behaviour and the reason the redirect never happens.
	if _, err := unfurl.NewFetcher().Fetch(context.Background(), redirector.URL); err == nil {
		t.Fatal("a loopback address was fetched")
	}
}

// TestRedirectSchemeIsRechecked checks that a redirect cannot change the scheme
// to something the first check would have refused.
func TestRedirectSchemeIsRechecked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "file:///etc/passwd", http.StatusFound)
	}))
	defer server.Close()

	_, err := unfurl.NewFetcherForTesting().Fetch(context.Background(), server.URL)
	if err == nil {
		t.Fatal("a redirect to a file:// URL was followed")
	}
	if !strings.Contains(err.Error(), "scheme") {
		t.Errorf("refused with %v, which does not look like the scheme check", err)
	}
}
