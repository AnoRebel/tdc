/*
Package translation translates messages through a provider somebody chose.

Five providers, one shape. The browser's on-device translator runs in the
reader's own client and never reaches this package; the other four —
LibreTranslate, Google Cloud Translation, Microsoft Translator and DeepL — are
called from here, so the keys that pay for them stay on the server and are
never handed to a page.

Message text passes through this package and is never logged: not on success,
not on failure, and not in a provider's error, which can quote the request.
*/
package translation

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// Provider names a translation service.
type Provider string

const (
	// Device is the browser's own translator. Listed so it can be allowed,
	// denied and chosen like the others; nothing here ever calls it.
	Device Provider = "device"
	// LibreTranslate is free, open source and self-hostable.
	LibreTranslate Provider = "libretranslate"
	// Google is Google Cloud Translation, v2.
	Google Provider = "google"
	// Microsoft is Microsoft Translator, v3 — what Bing uses.
	Microsoft Provider = "microsoft"
	// DeepL is the DeepL API, v2, Free or Pro.
	DeepL Provider = "deepl"
)

// Providers lists every provider, in the order a client offers them: free and
// private first.
var Providers = []Provider{Device, LibreTranslate, Google, Microsoft, DeepL}

// Known reports whether a name is a provider this server knows.
func Known(name string) bool {
	for _, provider := range Providers {
		if string(provider) == name {
			return true
		}
	}
	return false
}

// NeedsKey reports whether a provider cannot be used without a person's own
// key. LibreTranslate can: the operator's instance, or a keyless one.
func (p Provider) NeedsKey() bool {
	return p == Google || p == Microsoft || p == DeepL
}

var (
	// ErrNotConnected is returned when a provider needs credentials nobody
	// has given it.
	ErrNotConnected = errors.New("that translation provider is not connected")
	// ErrProviderFailed is returned when a provider could not be reached or
	// refused. Deliberately vague: the details are logged without the text.
	ErrProviderFailed = errors.New("the translation provider could not be reached")
	// ErrInvalidEndpoint is returned for a personal endpoint that is not an
	// https address on the public internet.
	ErrInvalidEndpoint = errors.New("the address must be an https address on the public internet")
)

// DialFunc opens a connection, so the caller decides what may be reached.
type DialFunc func(ctx context.Context, network, address string) (net.Conn, error)

// Credentials are what a person connected a provider with.
type Credentials struct {
	APIKey string
	// Endpoint is a LibreTranslate instance's address.
	Endpoint string
	// Region is the Azure region a Microsoft key belongs to, when it needs one.
	Region string
}

// Settings is what an operator configures.
type Settings struct {
	// LibreTranslateURL is the operator's own instance, which everybody here
	// may use. Empty when there is none.
	LibreTranslateURL string
	// LibreTranslateAPIKey is that instance's key, if it asks for one.
	LibreTranslateAPIKey string
	// SecretKey seals personal keys; base64 of 32 bytes. Empty means personal
	// keys cannot be saved.
	SecretKey string
	// PreviousSecretKey is the secret key being replaced: keys sealed under
	// it still open, and ResealAll moves them to SecretKey.
	PreviousSecretKey string
	// Timeout bounds one call to a provider.
	Timeout time.Duration

	// For this package's and the API's tests only; see WithTestProviders.
	testBases map[Provider]string
	testDial  DialFunc
	testTLS   *tls.Config
}

/*
WithTestProviders points the fixed providers at stand-ins, reached with the
given dialer.

For tests only, and exported for the API's tests. Not configuration: an option
that redirected Google's address would be a way to send people's keys and
messages somewhere else.
*/
func (s Settings) WithTestProviders(bases map[Provider]string, dial DialFunc, config *tls.Config) Settings {
	s.testBases = bases
	s.testDial = dial
	s.testTLS = config
	return s
}

// Service calls providers.
type Service struct {
	settings Settings
	sealer   *sealer
	previous *sealer
	log      *slog.Logger

	// operator reaches the operator's own instance: their choice, often on a
	// private network beside this server, so not refused for being private.
	operator *http.Client
	// public reaches everything else — a fixed provider, or an address a
	// member typed — and refuses private addresses.
	public *http.Client
	// fixed reaches Google, Microsoft and DeepL: the public client, except in
	// tests.
	fixed *http.Client
}

// NewService builds the service. dial is the guarded dialer used for
// everything an operator did not configure themselves.
func NewService(settings Settings, dial DialFunc, log *slog.Logger) (*Service, error) {
	if settings.Timeout <= 0 {
		settings.Timeout = 10 * time.Second
	}

	service := &Service{
		settings: settings,
		log:      log,
		operator: newClient(settings.Timeout, nil, settings.testTLS),
		public:   newClient(settings.Timeout, dial, nil),
	}
	service.fixed = service.public
	if settings.testDial != nil {
		service.fixed = newClient(settings.Timeout, settings.testDial, settings.testTLS)
	}

	if settings.SecretKey != "" {
		key, err := ParseSecretKey(settings.SecretKey)
		if err != nil {
			return nil, fmt.Errorf("translation.secret_key %w", err)
		}
		if service.sealer, err = newSealer(key); err != nil {
			return nil, err
		}
	}
	if settings.PreviousSecretKey != "" && service.sealer != nil {
		key, err := ParseSecretKey(settings.PreviousSecretKey)
		if err != nil {
			return nil, fmt.Errorf("translation.previous_secret_key %w", err)
		}
		if service.previous, err = newSealer(key); err != nil {
			return nil, err
		}
	}

	return service, nil
}

func newClient(timeout time.Duration, dial DialFunc, config *tls.Config) *http.Client {
	transport := &http.Transport{
		TLSClientConfig:       config,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       30 * time.Second,
	}
	if dial != nil {
		transport.DialContext = dial
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		// A redirect leads somewhere other than the host that was checked,
		// carrying the key and the message.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// SharedInstance reports whether the operator provides a LibreTranslate
// instance everybody here may use.
func (s *Service) SharedInstance() bool { return s.settings.LibreTranslateURL != "" }

/*
ValidateEndpoint checks a personal LibreTranslate address when it is saved.

https only, because the key travels with every request. Not a literal private
address — the check that can be made without resolving anything. A name that
resolves somewhere private is refused again when it is dialled, which is the
check that counts.
*/
func ValidateEndpoint(raw string) (string, error) {
	address, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || address.Scheme != "https" || address.Host == "" ||
		address.User != nil || address.RawQuery != "" || address.Fragment != "" {
		return "", ErrInvalidEndpoint
	}
	if ip, err := netip.ParseAddr(strings.Trim(address.Hostname(), "[]")); err == nil {
		if !ip.IsGlobalUnicast() || ip.IsPrivate() {
			return "", ErrInvalidEndpoint
		}
	}
	return strings.TrimRight(address.String(), "/"), nil
}

// Result is one translated text.
type Result struct {
	Text string
	// Source is the language the provider found, lowercased, or empty.
	Source string
}

/*
maxTexts and maxCharacters bound one call.

Below every provider's own limits (Microsoft's are the tightest: 1,000 texts
and 50,000 characters), so a request this server makes is never refused for
size — a batch of fifty full-length messages is 400,000 characters at most,
which the caller splits.
*/
const (
	maxTexts      = 500
	maxCharacters = 40000
)

/*
Translate sends texts to a provider and returns them translated, in order.

credentials is nil for the operator's LibreTranslate instance. Large batches
are sent in several calls, so the caller does not need to know any provider's
limits.
*/
func (s *Service) Translate(
	ctx context.Context,
	provider Provider,
	credentials *Credentials,
	texts []string,
	target string,
) ([]Result, error) {
	results := make([]Result, 0, len(texts))
	for start := 0; start < len(texts); {
		end, size := start, 0
		for end < len(texts) && end-start < maxTexts && (end == start || size+len(texts[end]) <= maxCharacters) {
			size += len(texts[end])
			end++
		}
		batch, err := s.translateBatch(ctx, provider, credentials, texts[start:end], target)
		if err != nil {
			return nil, err
		}
		if len(batch) != end-start {
			s.log.Warn("a translation provider answered with the wrong number of texts",
				"provider", provider, "sent", end-start, "received", len(batch))
			return nil, ErrProviderFailed
		}
		results = append(results, batch...)
		start = end
	}
	return results, nil
}

func (s *Service) translateBatch(
	ctx context.Context,
	provider Provider,
	credentials *Credentials,
	texts []string,
	target string,
) ([]Result, error) {
	switch provider {
	case LibreTranslate:
		return s.libreTranslate(ctx, credentials, texts, target)
	case Google:
		return s.google(ctx, credentials, texts, target)
	case Microsoft:
		return s.microsoft(ctx, credentials, texts, target)
	case DeepL:
		return s.deepl(ctx, credentials, texts, target)
	default:
		return nil, fmt.Errorf("translation: %q is not translated by the server", provider)
	}
}

func (s *Service) base(provider Provider, production string) string {
	if base, ok := s.settings.testBases[provider]; ok {
		return base
	}
	return production
}

func (s *Service) libreTranslate(ctx context.Context, credentials *Credentials, texts []string, target string) ([]Result, error) {
	client, endpoint, key := s.operator, s.settings.LibreTranslateURL, s.settings.LibreTranslateAPIKey
	if credentials != nil {
		client, endpoint, key = s.public, credentials.Endpoint, credentials.APIKey
	}
	if endpoint == "" {
		return nil, ErrNotConnected
	}

	request := map[string]any{"q": texts, "source": "auto", "target": target, "format": "text"}
	if key != "" {
		request["api_key"] = key
	}

	var answer struct {
		TranslatedText   []string        `json:"translatedText"`
		DetectedLanguage json.RawMessage `json:"detectedLanguage"`
	}
	if err := s.post(ctx, client, LibreTranslate, strings.TrimRight(endpoint, "/")+"/translate", nil, request, &answer); err != nil {
		return nil, err
	}

	// One detection per text for a batch; some versions send one for all.
	type detection struct {
		Language string `json:"language"`
	}
	var each []detection
	if json.Unmarshal(answer.DetectedLanguage, &each) != nil {
		var one detection
		_ = json.Unmarshal(answer.DetectedLanguage, &one)
		each = nil
		for range answer.TranslatedText {
			each = append(each, one)
		}
	}

	results := make([]Result, len(answer.TranslatedText))
	for i, text := range answer.TranslatedText {
		results[i].Text = text
		if i < len(each) {
			results[i].Source = Primary(each[i].Language)
		}
	}
	return results, nil
}

func (s *Service) google(ctx context.Context, credentials *Credentials, texts []string, target string) ([]Result, error) {
	if credentials == nil || credentials.APIKey == "" {
		return nil, ErrNotConnected
	}

	var answer struct {
		Data struct {
			Translations []struct {
				TranslatedText         string `json:"translatedText"`
				DetectedSourceLanguage string `json:"detectedSourceLanguage"`
			} `json:"translations"`
		} `json:"data"`
	}
	// The key in a header rather than the query string, where it would be in
	// every error Go's transport produces and every proxy's log.
	headers := http.Header{"X-Goog-Api-Key": {credentials.APIKey}}
	body := map[string]any{"q": texts, "target": googleLanguage(target), "format": "text"}
	if err := s.post(ctx, s.fixed, Google,
		s.base(Google, "https://translation.googleapis.com")+"/language/translate/v2",
		headers, body, &answer); err != nil {
		return nil, err
	}

	results := make([]Result, len(answer.Data.Translations))
	for i, item := range answer.Data.Translations {
		results[i] = Result{Text: item.TranslatedText, Source: Primary(item.DetectedSourceLanguage)}
	}
	return results, nil
}

func (s *Service) microsoft(ctx context.Context, credentials *Credentials, texts []string, target string) ([]Result, error) {
	if credentials == nil || credentials.APIKey == "" {
		return nil, ErrNotConnected
	}

	headers := http.Header{"Ocp-Apim-Subscription-Key": {credentials.APIKey}}
	if credentials.Region != "" {
		headers.Set("Ocp-Apim-Subscription-Region", credentials.Region)
	}
	body := make([]map[string]string, len(texts))
	for i, text := range texts {
		body[i] = map[string]string{"Text": text}
	}

	var answer []struct {
		DetectedLanguage struct {
			Language string `json:"language"`
		} `json:"detectedLanguage"`
		Translations []struct {
			Text string `json:"text"`
		} `json:"translations"`
	}
	endpoint := s.base(Microsoft, "https://api.cognitive.microsofttranslator.com") +
		"/translate?" + url.Values{"api-version": {"3.0"}, "to": {microsoftLanguage(target)}}.Encode()
	if err := s.post(ctx, s.fixed, Microsoft, endpoint, headers, body, &answer); err != nil {
		return nil, err
	}

	results := make([]Result, len(answer))
	for i, item := range answer {
		if len(item.Translations) == 0 {
			return nil, ErrProviderFailed
		}
		results[i] = Result{Text: item.Translations[0].Text, Source: Primary(item.DetectedLanguage.Language)}
	}
	return results, nil
}

func (s *Service) deepl(ctx context.Context, credentials *Credentials, texts []string, target string) ([]Result, error) {
	if credentials == nil || credentials.APIKey == "" {
		return nil, ErrNotConnected
	}

	// A Free key ends in ":fx" and works only against the Free host.
	production := "https://api.deepl.com"
	if strings.HasSuffix(credentials.APIKey, ":fx") {
		production = "https://api-free.deepl.com"
	}

	headers := http.Header{"Authorization": {"DeepL-Auth-Key " + credentials.APIKey}}
	body := map[string]any{"text": texts, "target_lang": deeplLanguage(target)}

	var answer struct {
		Translations []struct {
			DetectedSourceLanguage string `json:"detected_source_language"`
			Text                   string `json:"text"`
		} `json:"translations"`
	}
	if err := s.post(ctx, s.fixed, DeepL, s.base(DeepL, production)+"/v2/translate",
		headers, body, &answer); err != nil {
		return nil, err
	}

	results := make([]Result, len(answer.Translations))
	for i, item := range answer.Translations {
		results[i] = Result{Text: item.Text, Source: Primary(item.DetectedSourceLanguage)}
	}
	return results, nil
}

// maxResponseBytes bounds what a provider may answer with.
const maxResponseBytes = 2 << 20

func (s *Service) post(
	ctx context.Context,
	client *http.Client,
	provider Provider,
	endpoint string,
	headers http.Header,
	body, into any,
) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("translation: encoding the request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		// Not wrapped: the endpoint may be one a person typed, and nothing
		// about it needs repeating.
		return ErrProviderFailed
	}
	for name, values := range headers {
		request.Header[name] = values
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response, err := client.Do(request)
	if err != nil {
		/*
			Logged without the error's text and returned without wrapping. Go's
			transport errors quote the request, and what a request to a provider
			carries is a key and somebody's message.
		*/
		s.log.Warn("cannot reach a translation provider", "provider", provider)
		return ErrProviderFailed
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		// The provider's own message is neither logged nor passed on: it can
		// quote the text it refused.
		s.log.Warn("a translation provider refused a request",
			"provider", provider, "status", response.StatusCode)
		return ErrProviderFailed
	}

	if err := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes+1)).Decode(into); err != nil {
		s.log.Warn("a translation provider's answer could not be read", "provider", provider)
		return ErrProviderFailed
	}
	return nil
}

// Primary returns a language tag's primary subtag, lowercased: "EN-US" is
// "en". Used to compare languages, where a region does not make a message
// need translating.
func Primary(tag string) string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if index := strings.IndexAny(tag, "-_"); index >= 0 {
		tag = tag[:index]
	}
	return tag
}

// The few targets whose code differs between providers.

func googleLanguage(tag string) string {
	if Primary(tag) == "zh" && !strings.Contains(tag, "-") {
		return "zh-CN"
	}
	return tag
}

func microsoftLanguage(tag string) string {
	if Primary(tag) == "zh" && !strings.Contains(tag, "-") {
		return "zh-Hans"
	}
	return tag
}

// DeepL wants upper case, and a variant for the targets it has several of.
func deeplLanguage(tag string) string {
	switch strings.ToLower(tag) {
	case "en":
		return "EN-US"
	case "pt":
		return "PT-BR"
	case "zh":
		return "ZH-HANS"
	}
	return strings.ToUpper(tag)
}
