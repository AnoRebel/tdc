/*
Package gifs searches a third-party GIF library.

Off unless an operator configures it, and deliberately so. Every GIF library
worth searching — Tenor, Giphy — requires an account and an API key, which
conflicts with this server running at no cost and with no external accounts.
Sending a GIF as an attachment needs neither and works without any of this.

When it is on, searches are proxied through this server rather than made by the
browser. The key stays here, and the provider sees one server rather than every
person using it — a client calling the provider directly would hand it an
address and a request per keystroke for everybody on the server.
*/
package gifs

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrDisabled is returned when no provider is configured.
	ErrDisabled = errors.New("gif search is not enabled on this server")
	// ErrUnknownProvider is returned for a provider name that is not one of
	// the two supported.
	ErrUnknownProvider = errors.New("that is not a gif provider this server knows")
)

// Provider names a GIF library.
type Provider string

const (
	// ProviderNone is the default: no search, and no third party involved.
	ProviderNone Provider = ""
	// ProviderTenor is Google's library.
	ProviderTenor Provider = "tenor"
	// ProviderGiphy is Giphy's.
	ProviderGiphy Provider = "giphy"
)

// DialFunc opens a connection, so the caller decides what may be reached.
type DialFunc func(ctx context.Context, network, address string) (net.Conn, error)

// Settings is what an operator configures.
type Settings struct {
	// Provider is "tenor", "giphy", or empty for no search at all.
	Provider Provider
	// APIKey is the operator's own key with that provider.
	APIKey string
	/*
		ContentFilter bounds what a search may return.

		Defaults to the strictest each provider offers, because this is a
		search box in a shared conversation: somebody typing an innocuous word
		should not be shown something they then have to explain.
	*/
	ContentFilter string
	// Timeout bounds a search, so a provider that stops answering does not
	// hold a request open.
	Timeout time.Duration

	// tlsConfig lets this package's own tests present a stand-in provider over
	// TLS. Unexported: an operator has no business relaxing certificate
	// verification on a request that carries their key.
	tlsConfig *tls.Config
}

// WithTestTLS trusts a certificate, for this package's tests only.
//
// Exported for testing rather than made configurable, because the alternative
// — an option an operator could set — would be a way to disable certificate
// verification on requests carrying their API key.
func (s Settings) WithTestTLS(config *tls.Config) Settings {
	s.tlsConfig = config
	return s
}

// GIF is one result, reduced to what a client needs to show and send it.
type GIF struct {
	ID string `json:"id"`
	// Description is the provider's own alt text, which is what makes a result
	// usable by somebody who cannot see it.
	Description string `json:"description"`
	// URL is the full-size animation.
	URL string `json:"url"`
	// PreviewURL is a smaller version for the picker grid, where a dozen
	// full-size animations at once would be megabytes.
	PreviewURL string `json:"previewUrl"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
}

// Service searches whichever library the operator configured.
type Service struct {
	settings Settings
	client   *http.Client
	log      *slog.Logger
}

/*
NewService builds the search service.

The HTTP client uses the same dialer that guards link previews: it refuses to
connect to a private address. A provider hostname should never resolve to one,
and the day one does — through a hijacked record or a misconfigured resolver —
is exactly the day this matters.
*/
func NewService(settings Settings, dial DialFunc, log *slog.Logger) *Service {
	timeout := settings.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	return &Service{
		settings: settings,
		log:      log,
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				DialContext:           dial,
				TLSClientConfig:       settings.tlsConfig,
				TLSHandshakeTimeout:   timeout,
				ResponseHeaderTimeout: timeout,
				// One provider, called repeatedly: a small pool reused beats a
				// fresh connection and TLS handshake per search.
				MaxIdleConnsPerHost: 4,
				IdleConnTimeout:     30 * time.Second,
			},
			// A provider redirecting elsewhere is not something to follow: the
			// destination is not the host whose certificate was checked.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// Enabled reports whether searching is possible, so a client can hide the
// control rather than offering one that always fails.
func (s *Service) Enabled() bool {
	return s.settings.Provider != ProviderNone && s.settings.APIKey != ""
}

// Provider names the configured library, for a client to attribute results.
//
// Attribution is a term of both providers' licences, not a courtesy.
func (s *Service) Provider() Provider { return s.settings.Provider }

/*
maxResults bounds a search.

Both providers cap at 50. This is lower because it is a picker grid: more than
this is scrolling rather than choosing, and every result is an animation the
reader's browser has to load.
*/
const maxResults = 24

// Search returns GIFs matching a query, or the library's own suggestions when
// the query is empty.
func (s *Service) Search(ctx context.Context, query string, limit int) ([]GIF, error) {
	if !s.Enabled() {
		return nil, ErrDisabled
	}

	if limit <= 0 || limit > maxResults {
		limit = maxResults
	}

	switch s.settings.Provider {
	case ProviderTenor:
		return s.searchTenor(ctx, query, limit)
	case ProviderGiphy:
		return s.searchGiphy(ctx, query, limit)
	default:
		return nil, ErrUnknownProvider
	}
}

// tenorResponse is the part of Tenor's answer this uses.
type tenorResponse struct {
	Results []struct {
		ID           string `json:"id"`
		ContentDesc  string `json:"content_description"`
		MediaFormats map[string]struct {
			URL  string `json:"url"`
			Dims []int  `json:"dims"`
		} `json:"media_formats"`
	} `json:"results"`
}

func (s *Service) searchTenor(ctx context.Context, query string, limit int) ([]GIF, error) {
	// An empty query asks for what is popular rather than returning nothing,
	// so opening the picker shows something to choose from.
	endpoint := "https://tenor.googleapis.com/v2/search"
	if strings.TrimSpace(query) == "" {
		endpoint = "https://tenor.googleapis.com/v2/featured"
	}

	filter := s.settings.ContentFilter
	if filter == "" {
		// Tenor's strictest.
		filter = "high"
	}

	parameters := url.Values{
		"key":   {s.settings.APIKey},
		"limit": {strconv.Itoa(limit)},
		// Only the formats used, so the response is not several times larger
		// than what is displayed.
		"media_filter":  {"gif,tinygif"},
		"contentfilter": {filter},
		// Identifies this integration to the provider, which their terms ask
		// for and which keeps one server's usage distinguishable.
		"client_key": {"tdc"},
	}
	if query != "" {
		parameters.Set("q", query)
	}

	var answer tenorResponse
	if err := s.get(ctx, endpoint, parameters, &answer); err != nil {
		return nil, err
	}

	results := make([]GIF, 0, len(answer.Results))
	for _, item := range answer.Results {
		full := item.MediaFormats["gif"]
		preview := item.MediaFormats["tinygif"]

		if full.URL == "" {
			continue
		}
		if preview.URL == "" {
			preview = full
		}

		gif := GIF{
			ID:          item.ID,
			Description: item.ContentDesc,
			URL:         full.URL,
			PreviewURL:  preview.URL,
		}
		if len(full.Dims) == 2 {
			gif.Width, gif.Height = full.Dims[0], full.Dims[1]
		}
		results = append(results, gif)
	}

	return results, nil
}

// giphyResponse is the part of Giphy's answer this uses.
type giphyResponse struct {
	Data []struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		AltTxt string `json:"alt_text"`
		Images struct {
			Original struct {
				URL    string `json:"url"`
				Width  string `json:"width"`
				Height string `json:"height"`
			} `json:"original"`
			FixedHeight struct {
				URL string `json:"url"`
			} `json:"fixed_height"`
		} `json:"images"`
	} `json:"data"`
}

func (s *Service) searchGiphy(ctx context.Context, query string, limit int) ([]GIF, error) {
	endpoint := "https://api.giphy.com/v1/gifs/search"
	if strings.TrimSpace(query) == "" {
		endpoint = "https://api.giphy.com/v1/gifs/trending"
	}

	rating := s.settings.ContentFilter
	if rating == "" {
		// Giphy's strictest.
		rating = "g"
	}

	parameters := url.Values{
		"api_key": {s.settings.APIKey},
		"limit":   {strconv.Itoa(limit)},
		"rating":  {rating},
	}
	if query != "" {
		parameters.Set("q", query)
	}

	var answer giphyResponse
	if err := s.get(ctx, endpoint, parameters, &answer); err != nil {
		return nil, err
	}

	results := make([]GIF, 0, len(answer.Data))
	for _, item := range answer.Data {
		if item.Images.Original.URL == "" {
			continue
		}

		preview := item.Images.FixedHeight.URL
		if preview == "" {
			preview = item.Images.Original.URL
		}

		// Giphy sends dimensions as strings; a value that will not parse is
		// left at zero rather than failing the whole result.
		width, _ := strconv.Atoi(item.Images.Original.Width)
		height, _ := strconv.Atoi(item.Images.Original.Height)

		description := item.AltTxt
		if description == "" {
			description = item.Title
		}

		results = append(results, GIF{
			ID:          item.ID,
			Description: description,
			URL:         item.Images.Original.URL,
			PreviewURL:  preview,
			Width:       width,
			Height:      height,
		})
	}

	return results, nil
}

/*
maxResponseBytes bounds what a provider may return.

A search for two dozen results is tens of kilobytes of JSON. This is far above
that and far below anything that could exhaust memory, so a provider answering
with something unexpected costs a refused request rather than the server.
*/
const maxResponseBytes = 2 << 20

// get performs one request and decodes the answer.
func (s *Service) get(ctx context.Context, endpoint string, parameters url.Values, into any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		endpoint+"?"+parameters.Encode(), nil)
	if err != nil {
		return fmt.Errorf("gifs: building the request: %w", err)
	}
	request.Header.Set("Accept", "application/json")

	response, err := s.client.Do(request)
	if err != nil {
		// The error is not wrapped. Go's own transport errors quote the URL
		// they failed on, and that URL carries the operator's key — wrapping
		// it would put the credential in whatever logs or shows the failure.
		s.log.Warn("cannot reach the gif provider",
			"provider", s.settings.Provider, "error", err)
		return errors.New("gifs: the provider could not be reached")
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		// The provider's own message is not passed on: it may name the key,
		// and it is not something the person searching can act on.
		s.log.Warn("a gif provider refused a search",
			"provider", s.settings.Provider, "status", response.StatusCode)
		return fmt.Errorf("gifs: the provider answered %d", response.StatusCode)
	}

	decoder := json.NewDecoder(newLimitedReader(response.Body, maxResponseBytes))
	if err := decoder.Decode(into); err != nil {
		return fmt.Errorf("gifs: reading the provider's answer: %w", err)
	}

	return nil
}
