package unfurl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// Limits bound what fetching a link may cost.
//
// Every one of these is a defence against a hostile page rather than a tuning
// knob: a link can point at a server that answers slowly for ever, returns a
// gigabyte, or redirects in a loop, and none of that may affect the people
// using this server.
const (
	// requestTimeout bounds the whole fetch, including redirects. A preview is
	// a nicety; nobody should wait long for one.
	requestTimeout = 8 * time.Second

	// maxRedirects is enough for the http-to-https and www-prefix hops real
	// sites use, and short enough that a redirect loop ends quickly.
	maxRedirects = 3

	// maxBodyBytes is how much of a page is read. Metadata lives in the head,
	// so this is generous; the cap is what stops a hostile server streaming
	// indefinitely.
	maxBodyBytes = 512 << 10
)

// ErrNotPreviewable means a link has no preview worth showing.
//
// Not a failure: most links are to pages with no metadata, and the message is
// perfectly good without a card under it.
var ErrNotPreviewable = errors.New("unfurl: nothing to preview")

// Preview is what a link turned out to be.
type Preview struct {
	// URL is the address finally fetched, after redirects, so a client shows
	// where a shortener actually led.
	URL         string
	Title       string
	Description string
	// SiteName is the publication's own name, where it declares one.
	SiteName string
	// ImageURL is a picture the page nominated. Never fetched by the server —
	// see the note on Fetch — so a client must decide whether to load it.
	ImageURL string
}

// Fetcher retrieves previews.
type Fetcher struct {
	client *http.Client
}

// NewFetcher builds a fetcher that cannot reach private infrastructure.
func NewFetcher() *Fetcher {
	return newFetcher(safeDialer(requestTimeout).DialContext)
}

// newFetcherForTesting builds a fetcher that will reach loopback.
//
// Only for tests, and named so that is obvious at every call site. The address
// protection is tested separately and directly; this exists so that parsing,
// redirect and size behaviour can be tested against a real HTTP server without
// each of those tests silently skipping.
func newFetcherForTesting() *Fetcher {
	return newFetcher((&net.Dialer{Timeout: requestTimeout}).DialContext)
}

func newFetcher(dial func(context.Context, string, string) (net.Conn, error)) *Fetcher {
	transport := &http.Transport{
		DialContext:           dial,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
		DisableKeepAlives:     true,
		// A proxy configured in the environment would bypass the dialer
		// entirely, and with it every address check.
		Proxy: nil,
	}

	return &Fetcher{
		client: &http.Client{
			Transport: transport,
			Timeout:   requestTimeout,
			CheckRedirect: func(request *http.Request, previous []*http.Request) error {
				if len(previous) >= maxRedirects {
					return fmt.Errorf("unfurl: more than %d redirects", maxRedirects)
				}
				// Each hop is re-checked, because a redirect is a fresh URL
				// chosen by the same untrusted party. The dialer checks the
				// address; this checks the scheme, so a redirect to file:// or
				// gopher:// is refused before it is dialled at all.
				return checkScheme(request.URL)
			},
		},
	}
}

/*
Fetch retrieves a preview of a link.

The image a page nominates is deliberately not fetched here. Doing so would
turn every posted link into an outbound request to an address of the poster's
choosing — the same problem this package exists to solve, one layer down, and
with the added cost of storing whatever came back. The client is given the URL
and decides, under its own content policy, whether to load it.
*/
func (f *Fetcher) Fetch(ctx context.Context, rawURL string) (*Preview, error) {
	target, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, fmt.Errorf("unfurl: %q is not a URL: %w", rawURL, err)
	}
	if err := checkScheme(target); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("unfurl: building the request: %w", err)
	}

	// Identified honestly, so an operator who does not want their site
	// unfurled can refuse by user agent rather than having to guess.
	request.Header.Set("User-Agent", "TDC-LinkPreview/1.0 (+https://github.com/AnoRebel/tdc)")
	request.Header.Set("Accept", "text/html,application/xhtml+xml")
	// Asking for the language the server runs in, so a preview reads in the
	// same language as the conversation where it appears.
	request.Header.Set("Accept-Language", "en, sw;q=0.9")

	response, err := f.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("unfurl: fetching %q: %w", target.Redacted(), err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unfurl: %q answered %d", target.Redacted(), response.StatusCode)
	}

	// Only HTML carries the metadata a preview is made from. Anything else is
	// refused before it is parsed, rather than after.
	contentType := response.Header.Get("Content-Type")
	if mediaType, _, _ := strings.Cut(contentType, ";"); !isHTML(strings.TrimSpace(mediaType)) {
		return nil, ErrNotPreviewable
	}

	preview, err := parse(io.LimitReader(response.Body, maxBodyBytes))
	if err != nil {
		return nil, err
	}

	// The URL after redirects, so a shortener resolves to where it led.
	preview.URL = response.Request.URL.String()

	// Relative image paths are resolved against the page, since that is what a
	// browser would do and a client cannot do it without knowing the page.
	if preview.ImageURL != "" {
		if resolved, err := response.Request.URL.Parse(preview.ImageURL); err == nil {
			if checkScheme(resolved) == nil {
				preview.ImageURL = resolved.String()
			} else {
				preview.ImageURL = ""
			}
		} else {
			preview.ImageURL = ""
		}
	}

	if preview.Title == "" && preview.Description == "" {
		return nil, ErrNotPreviewable
	}

	return preview, nil
}

// checkScheme refuses anything that is not ordinary web traffic.
//
// file://, gopher://, ftp:// and the rest are all ways to reach something the
// server can read and a browser cannot; none of them belong in a link preview.
func checkScheme(target *url.URL) error {
	switch target.Scheme {
	case "http", "https":
		if target.Host == "" {
			return errors.New("unfurl: the URL names no host")
		}
		return nil
	default:
		return fmt.Errorf("unfurl: refusing the %q scheme", target.Scheme)
	}
}

func isHTML(mediaType string) bool {
	return mediaType == "text/html" || mediaType == "application/xhtml+xml"
}

/*
parse pulls the metadata a page declares about itself.

OpenGraph first, then Twitter's equivalents, then the ordinary HTML elements —
which is the order of specificity: a page that publishes OpenGraph has said what
it wants a preview to look like, and a page with only a <title> has not.
*/
func parse(body io.Reader) (*Preview, error) {
	document, err := html.Parse(body)
	if err != nil {
		return nil, fmt.Errorf("unfurl: reading the page: %w", err)
	}

	preview := &Preview{}
	// Filled only from the fallbacks, so a page's own OpenGraph always wins.
	var htmlTitle, metaDescription string

	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			switch node.Data {
			case "title":
				if htmlTitle == "" && node.FirstChild != nil {
					htmlTitle = node.FirstChild.Data
				}
			case "meta":
				property, name, content := metaAttributes(node)

				switch property {
				case "og:title":
					preview.Title = firstNonEmpty(preview.Title, content)
				case "og:description":
					preview.Description = firstNonEmpty(preview.Description, content)
				case "og:site_name":
					preview.SiteName = firstNonEmpty(preview.SiteName, content)
				case "og:image", "og:image:secure_url":
					preview.ImageURL = firstNonEmpty(preview.ImageURL, content)
				}

				switch name {
				case "twitter:title":
					preview.Title = firstNonEmpty(preview.Title, content)
				case "twitter:description":
					preview.Description = firstNonEmpty(preview.Description, content)
				case "twitter:image":
					preview.ImageURL = firstNonEmpty(preview.ImageURL, content)
				case "description":
					metaDescription = firstNonEmpty(metaDescription, content)
				}
			case "body":
				// Everything worth having is in the head; walking the whole
				// body of a large page would cost far more for nothing.
				return
			}
		}

		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)

	preview.Title = truncate(firstNonEmpty(preview.Title, htmlTitle), 200)
	preview.Description = truncate(firstNonEmpty(preview.Description, metaDescription), 400)
	preview.SiteName = truncate(preview.SiteName, 100)

	return preview, nil
}

// metaAttributes reads the three attributes a meta element uses to declare
// metadata, whichever convention it follows.
func metaAttributes(node *html.Node) (property, name, content string) {
	for _, attribute := range node.Attr {
		switch strings.ToLower(attribute.Key) {
		case "property":
			property = strings.ToLower(strings.TrimSpace(attribute.Val))
		case "name":
			name = strings.ToLower(strings.TrimSpace(attribute.Val))
		case "content":
			content = strings.TrimSpace(attribute.Val)
		}
	}
	return property, name, content
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// truncate bounds a field, cutting on a rune boundary so a multi-byte
// character is never split in half.
func truncate(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= limit {
		return value
	}

	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit])) + "…"
}

// ParseForTesting exposes the metadata extraction for tests.
//
// Parsing is the part with the most cases — competing conventions, missing
// fields, whitespace — and testing it through an HTTP server would make those
// cases slow and indirect for no gain.
func ParseForTesting(body io.Reader) (*Preview, error) { return parse(body) }

// NewFetcherForTesting exposes a fetcher that will reach loopback, so the
// redirect and size limits can be tested against a real server.
func NewFetcherForTesting() *Fetcher { return newFetcherForTesting() }
