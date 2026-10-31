package translation

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"uuid"
)

// plainDial reaches the stand-in providers, which listen on loopback.
func plainDial(ctx context.Context, network, address string) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, network, address)
}

// refuseDial is the guarded dialer's verdict on every address.
func refuseDial(context.Context, string, string) (net.Conn, error) {
	return nil, &net.OpError{Op: "dial", Err: io.ErrClosedPipe}
}

type recorded struct {
	path    string
	headers http.Header
	body    map[string]any
	raw     string
}

// standIn answers like every provider at once, upper-casing what it is sent
// and claiming it was English.
func standIn(t *testing.T, seen *[]recorded) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		entry := recorded{path: r.URL.Path, headers: r.Header.Clone(), raw: string(raw)}
		_ = json.Unmarshal(raw, &entry.body)
		*seen = append(*seen, entry)

		switch {
		case r.URL.Path == "/translate" && r.URL.Query().Get("api-version") != "":
			var items []map[string]string
			_ = json.Unmarshal(raw, &items)
			out := []map[string]any{}
			for _, item := range items {
				out = append(out, map[string]any{
					"detectedLanguage": map[string]any{"language": "en"},
					"translations":     []map[string]string{{"text": strings.ToUpper(item["Text"])}},
				})
			}
			_ = json.NewEncoder(w).Encode(out)
		case r.URL.Path == "/translate":
			var texts []string
			for _, q := range entry.body["q"].([]any) {
				texts = append(texts, strings.ToUpper(q.(string)))
			}
			detections := []map[string]any{}
			for range texts {
				detections = append(detections, map[string]any{"language": "en", "confidence": 90})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"translatedText": texts, "detectedLanguage": detections})
		case r.URL.Path == "/language/translate/v2":
			out := []map[string]string{}
			for _, q := range entry.body["q"].([]any) {
				out = append(out, map[string]string{"translatedText": strings.ToUpper(q.(string)), "detectedSourceLanguage": "en"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"translations": out}})
		case r.URL.Path == "/v2/translate":
			out := []map[string]string{}
			for _, q := range entry.body["text"].([]any) {
				out = append(out, map[string]string{"text": strings.ToUpper(q.(string)), "detected_source_language": "EN"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"translations": out})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func testService(t *testing.T, operatorURL string, base string) (*Service, *bytes.Buffer) {
	t.Helper()
	logs := &bytes.Buffer{}
	key, _ := GenerateSecretKey()
	settings := Settings{LibreTranslateURL: operatorURL, SecretKey: key}
	if base != "" {
		settings = settings.WithTestProviders(map[Provider]string{
			Google: base, Microsoft: base, DeepL: base,
		}, plainDial, nil)
	}
	service, err := NewService(settings, refuseDial, slog.New(slog.NewTextHandler(logs, nil)))
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return service, logs
}

// TestEachProviderTranslatesABatchInOrder checks the four request shapes
// against what each provider documents, and that answers come back in order.
func TestEachProviderTranslatesABatchInOrder(t *testing.T) {
	var seen []recorded
	stand := standIn(t, &seen)
	service, _ := testService(t, stand.URL, stand.URL)

	for _, c := range []struct {
		provider    Provider
		credentials *Credentials
		check       func(recorded) string
	}{
		{LibreTranslate, nil, func(r recorded) string {
			if r.body["source"] != "auto" || r.body["target"] != "sw" {
				return "libretranslate was not asked for auto to sw"
			}
			return ""
		}},
		{Google, &Credentials{APIKey: "g-key"}, func(r recorded) string {
			if r.headers.Get("X-Goog-Api-Key") != "g-key" {
				return "google's key was not in its header"
			}
			return ""
		}},
		{Microsoft, &Credentials{APIKey: "m-key", Region: "westeurope"}, func(r recorded) string {
			if r.headers.Get("Ocp-Apim-Subscription-Key") != "m-key" ||
				r.headers.Get("Ocp-Apim-Subscription-Region") != "westeurope" {
				return "microsoft's key or region was missing"
			}
			return ""
		}},
		{DeepL, &Credentials{APIKey: "d-key:fx"}, func(r recorded) string {
			if r.headers.Get("Authorization") != "DeepL-Auth-Key d-key:fx" || r.body["target_lang"] != "SW" {
				return "deepl's key or target was wrong"
			}
			return ""
		}},
	} {
		seen = nil
		results, err := service.Translate(context.Background(), c.provider, c.credentials,
			[]string{"one", "two"}, "sw")
		if err != nil {
			t.Fatalf("%s: %v", c.provider, err)
		}
		if len(results) != 2 || results[0].Text != "ONE" || results[1].Text != "TWO" || results[0].Source != "en" {
			t.Errorf("%s answered %#v", c.provider, results)
		}
		if len(seen) != 1 {
			t.Fatalf("%s made %d requests, want 1", c.provider, len(seen))
		}
		if problem := c.check(seen[0]); problem != "" {
			t.Error(problem)
		}
		if strings.Contains(seen[0].path, "key") {
			t.Errorf("%s carried a key in its address", c.provider)
		}
	}
}

/*
TestAPersonalEndpointIsDialledThroughTheGuard is the check that counts against
being pointed at this server's own network: a name can pass when it is saved
and resolve somewhere private later.
*/
func TestAPersonalEndpointIsDialledThroughTheGuard(t *testing.T) {
	var seen []recorded
	stand := standIn(t, &seen)
	service, _ := testService(t, "", "")

	_, err := service.Translate(context.Background(), LibreTranslate,
		&Credentials{Endpoint: stand.URL}, []string{"hello"}, "sw")
	if err == nil {
		t.Fatal("a personal endpoint on loopback was reached")
	}
	if len(seen) != 0 {
		t.Errorf("the stand-in received %d requests through the guard", len(seen))
	}
}

// TestFailuresDoNotLogTheTextOrTheKey is the rule this package's comment
// opens with.
func TestFailuresDoNotLogTheTextOrTheKey(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		http.Error(w, "refused: "+string(raw), http.StatusForbidden)
	}))
	t.Cleanup(failing.Close)

	service, logs := testService(t, "", failing.URL)
	_, err := service.Translate(context.Background(), Google, &Credentials{APIKey: "secret-key-123"},
		[]string{"a very private sentence"}, "sw")
	if err == nil {
		t.Fatal("a refusal was reported as success")
	}
	for _, leaked := range []string{"private sentence", "secret-key-123"} {
		if strings.Contains(logs.String(), leaked) || strings.Contains(err.Error(), leaked) {
			t.Errorf("%q reached the log or the error", leaked)
		}
	}
}

func TestValidateEndpoint(t *testing.T) {
	for raw, ok := range map[string]bool{
		"https://translate.example.org":     true,
		"https://translate.example.org/lt/": true,
		"http://translate.example.org":      false,
		"https://127.0.0.1":                 false,
		"https://10.0.0.5:5000":             false,
		"https://[::1]":                     false,
		"https://user:pass@example.org":     false,
		"ftp://example.org":                 false,
	} {
		if _, err := ValidateEndpoint(raw); (err == nil) != ok {
			t.Errorf("ValidateEndpoint(%q) error = %v, want ok=%v", raw, err, ok)
		}
	}
}

// TestASealedKeyOpensOnlyWhereItWasSealed is why the account and provider are
// bound into the seal.
func TestASealedKeyOpensOnlyWhereItWasSealed(t *testing.T) {
	service, _ := testService(t, "", "")
	owner, other := uuid.New(), uuid.New()

	sealed, err := service.Seal(owner, Google, "the-key")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("the-key")) {
		t.Fatal("the sealed key contains the key")
	}
	if got, err := service.Open(owner, Google, sealed); err != nil || got != "the-key" {
		t.Fatalf("Open = %q, %v", got, err)
	}
	if _, err := service.Open(other, Google, sealed); err == nil {
		t.Error("another account's row opened the key")
	}
	if _, err := service.Open(owner, DeepL, sealed); err == nil {
		t.Error("another provider's row opened the key")
	}
}
