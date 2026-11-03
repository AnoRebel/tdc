package httpapi_test

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
	"sync"
	"sync/atomic"
	"testing"
	"uuid"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/translation"
	"github.com/AnoRebel/tdc/server/internal/unfurl"
)

/*
translationSettingsForTest configures the translation service of the handler
built next. Package-level like the other per-test options here; restored by
the helper that sets it.
*/
var translationSettingsForTest translation.Settings

// translationKeyForTest seals members' keys in every test server, so the
// ordinary server here is one that can store them.
var translationKeyForTest, _ = translation.GenerateSecretKey()

func translationForTest(t *testing.T, logger *slog.Logger) *translation.Service {
	t.Helper()
	settings := translationSettingsForTest
	if settings.SecretKey == "" {
		settings.SecretKey = translationKeyForTest
	}
	// The real guard for anything a member configures, so a test of it tests
	// the thing that runs.
	service, err := translation.NewService(settings, unfurl.DialContext, logger)
	if err != nil {
		t.Fatalf("building translation: %v", err)
	}
	return service
}

/*
fakeTranslator answers like LibreTranslate, Google, Microsoft and DeepL at once.

It "translates" by prefixing each text with the target language, says the
source was English unless a text starts with "sw:", and records every text it
was sent, so a test can say what left the server.
*/
type fakeTranslator struct {
	mu       sync.Mutex
	requests int
	texts    []string
	headers  []http.Header
}

func (f *fakeTranslator) record(r *http.Request, texts []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	f.texts = append(f.texts, texts...)
	f.headers = append(f.headers, r.Header.Clone())
}

func (f *fakeTranslator) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests
}

func (f *fakeTranslator) sent() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.texts, "\n")
}

func source(text string) string {
	if strings.HasPrefix(text, "sw:") {
		return "sw"
	}
	return "en"
}

func (f *fakeTranslator) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)

	strs := func(key string) []string {
		var out []string
		for _, item := range body[key].([]any) {
			out = append(out, item.(string))
		}
		return out
	}

	switch r.URL.Path {
	case "/translate":
		texts := strs("q")
		f.record(r, texts)
		translated, detected := []string{}, []map[string]any{}
		for _, text := range texts {
			translated = append(translated, body["target"].(string)+"<"+text+">")
			detected = append(detected, map[string]any{"language": source(text), "confidence": 90})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"translatedText": translated, "detectedLanguage": detected})
	case "/language/translate/v2":
		texts := strs("q")
		f.record(r, texts)
		out := []map[string]string{}
		for _, text := range texts {
			out = append(out, map[string]string{
				"translatedText": body["target"].(string) + "<" + text + ">", "detectedSourceLanguage": source(text),
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"translations": out}})
	default:
		http.NotFound(w, r)
	}
}

// withTranslationProvider builds a server whose operator runs a
// LibreTranslate instance, and whose Google is the same stand-in.
func withTranslationProvider(t *testing.T) (http.Handler, *fakeTranslator, *httptest.Server) {
	t.Helper()
	fake := &fakeTranslator{}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)

	translationSettingsForTest = translation.Settings{LibreTranslateURL: server.URL}.WithTestProviders(
		map[translation.Provider]string{translation.Google: server.URL},
		func(ctx context.Context, network, address string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, address)
		}, nil)
	t.Cleanup(func() { translationSettingsForTest = translation.Settings{} })

	return newTestAPI(t), fake, server
}

type translateAnswer struct {
	Translations []struct {
		MessageID      string `json:"messageId"`
		Body           string `json:"body"`
		SourceLanguage string `json:"sourceLanguage"`
		Skipped        bool   `json:"skipped"`
	} `json:"translations"`
	Missing []string `json:"missing"`
}

func translate(t *testing.T, handler http.Handler, token, roomID, provider string, ids ...string) (*httptest.ResponseRecorder, translateAnswer) {
	t.Helper()
	recorder := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/translations", map[string]any{
		"messages": ids, "targetLanguage": "sw", "provider": provider,
	}, token)
	var answer translateAnswer
	if recorder.Code == http.StatusOK {
		if err := json.Unmarshal(recorder.Body.Bytes(), &answer); err != nil {
			t.Fatalf("decoding a translation: %v", err)
		}
	}
	return recorder, answer
}

func cachedTranslations(t *testing.T) int {
	t.Helper()
	var count int
	if err := poolForTest.QueryRow(context.Background(),
		"SELECT count(*) FROM message_translations").Scan(&count); err != nil {
		t.Fatalf("counting translations: %v", err)
	}
	return count
}

/*
TestTranslationIsOfferedByDefaultAndSendsNothingByItself is the default the
design argues for: on, and nothing configured to send anywhere.
*/
func TestTranslationIsOfferedByDefaultAndSendsNothingByItself(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)

	if !lists(advertised(t, handler), "translation") {
		t.Error("translation is not advertised on a default server")
	}

	overview := decode(t, mustCall(t, handler, http.MethodGet, "/api/me/translation", nil, token, http.StatusOK))
	if overview["canStoreKeys"] != true {
		t.Error("a server with a secret key says it cannot store keys")
	}
	for _, raw := range overview["providers"].([]any) {
		provider := raw.(map[string]any)
		if provider["allowed"] != true {
			t.Errorf("%v is not allowed by default", provider["name"])
		}
		connected := provider["connected"] == true
		if connected != (provider["name"] == "device") {
			t.Errorf("%v connected = %v on a server with nothing configured", provider["name"], connected)
		}
	}

	roomID := makeRoom(t, handler, token, "general", "public")
	id := sendMessage(t, handler, token, roomID, "hello")
	// No instance and no connection: nothing to translate with.
	if recorder, _ := translate(t, handler, token, roomID, "libretranslate", id); recorder.Code != http.StatusConflict {
		t.Errorf("translating with nothing connected returned %d, want 409", recorder.Code)
	}
}

func TestTranslationSwitchedOffAnswersNothing(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)
	setFeature(t, handler, token, "translation", false)

	for _, request := range []struct{ method, path string }{
		{http.MethodGet, "/api/me/translation"},
		{http.MethodPut, "/api/me/translation/preferences"},
		{http.MethodPut, "/api/me/translation/connections/google"},
		{http.MethodPost, "/api/rooms/00000000-0000-0000-0000-000000000000/translations"},
	} {
		if recorder := call(t, handler, request.method, request.path, map[string]any{}, token); recorder.Code != http.StatusNotFound {
			t.Errorf("%s %s returned %d with translation off, want 404", request.method, request.path, recorder.Code)
		}
	}
	if lists(advertised(t, handler), "translation") {
		t.Error("translation is advertised while switched off")
	}
}

/*
TestAConnectedKeyNeverComesBack is the promise made to whoever pastes one: it
is used, and it is not shown again — not to them, not to anybody — and it is
not readable from the database on its own.
*/
func TestAConnectedKeyNeverComesBack(t *testing.T) {
	handler, fake, _ := withTranslationProvider(t)
	token := completeSetup(t, handler)
	const key = "AIza-a-key-that-must-not-leak"

	mustCall(t, handler, http.MethodPut, "/api/me/translation/connections/google",
		map[string]string{"apiKey": key}, token, http.StatusNoContent)

	overview := mustCall(t, handler, http.MethodGet, "/api/me/translation", nil, token, http.StatusOK)
	if strings.Contains(overview.Body.String(), key) || strings.Contains(overview.Body.String(), key[len(key)-4:]) {
		t.Fatal("the overview carries the key, or part of it")
	}
	for _, raw := range decode(t, overview)["providers"].([]any) {
		if provider := raw.(map[string]any); provider["name"] == "google" && (provider["hasKey"] != true || provider["connected"] != true) {
			t.Errorf("google after connecting: %v", provider)
		}
	}

	var sealed []byte
	if err := poolForTest.QueryRow(context.Background(),
		"SELECT sealed_key FROM translation_connections WHERE provider = 'google'").Scan(&sealed); err != nil {
		t.Fatalf("reading the stored key: %v", err)
	}
	if bytes.Contains(sealed, []byte(key)) {
		t.Fatal("the key is stored as written")
	}

	// It is used, though: the provider receives it.
	roomID := makeRoom(t, handler, token, "general", "public")
	id := sendMessage(t, handler, token, roomID, "good morning")
	if recorder, _ := translate(t, handler, token, roomID, "google", id); recorder.Code != http.StatusOK {
		t.Fatalf("translating with google returned %d: %s", recorder.Code, recorder.Body.String())
	}
	if got := fake.headers[len(fake.headers)-1].Get("X-Goog-Api-Key"); got != key {
		t.Errorf("google was sent key %q", got)
	}

	// Removing it removes it.
	mustCall(t, handler, http.MethodDelete, "/api/me/translation/connections/google", nil, token, http.StatusNoContent)
	if recorder, _ := translate(t, handler, token, roomID, "google", sendMessage(t, handler, token, roomID, "later")); recorder.Code != http.StatusConflict {
		t.Errorf("translating after removing the key returned %d, want 409", recorder.Code)
	}
}

/*
TestTheSecretKeyIsReplacedWithoutReconnecting is translation.previous_secret_key:
a server started with a new secret key and the old one as previous seals every
saved key again under the new one, so the old one can then be removed and the
keys still open.
*/
func TestTheSecretKeyIsReplacedWithoutReconnecting(t *testing.T) {
	handler, _, _ := withTranslationProvider(t)
	token := completeSetup(t, handler)
	const key = "AIza-kept-across-the-change"
	mustCall(t, handler, http.MethodPut, "/api/me/translation/connections/google",
		map[string]string{"apiKey": key}, token, http.StatusNoContent)

	newKey, _ := translation.GenerateSecretKey()
	build := func(settings translation.Settings) *translation.Service {
		service, err := translation.NewService(settings, unfurl.DialContext, slog.New(slog.DiscardHandler))
		if err != nil {
			t.Fatalf("building translation: %v", err)
		}
		return service
	}
	ctx, queries := context.Background(), dbgen.New(poolForTest)

	changing := build(translation.Settings{SecretKey: newKey, PreviousSecretKey: translationKeyForTest})
	if moved, err := changing.ResealAll(ctx, queries); err != nil || moved != 1 {
		t.Fatalf("ResealAll = %d, %v; want 1 key moved", moved, err)
	}
	if moved, _ := changing.ResealAll(ctx, queries); moved != 0 {
		t.Errorf("a second start moved %d keys again", moved)
	}

	var owner uuid.UUID
	var sealed []byte
	if err := poolForTest.QueryRow(ctx,
		"SELECT user_id, sealed_key FROM translation_connections WHERE provider = 'google'").Scan(&owner, &sealed); err != nil {
		t.Fatalf("reading the stored key: %v", err)
	}
	changed := build(translation.Settings{SecretKey: newKey})
	if got, err := changed.Open(owner, translation.Google, sealed); err != nil || got != key {
		t.Errorf("under the new key alone the saved key opens as %q, %v", got, err)
	}
	if _, err := build(translation.Settings{SecretKey: translationKeyForTest}).Open(owner, translation.Google, sealed); err == nil {
		t.Error("the old key still opens it, so it was not sealed again")
	}
}

/*
TestTranslationTranslatesOnlyWhatTheReaderMayRead holds the proxy to the rule
every room route keeps: a private room's messages are not reachable by
somebody outside it, and here that includes not being sent to a provider.
*/
func TestTranslationTranslatesOnlyWhatTheReaderMayRead(t *testing.T) {
	handler, fake, _ := withTranslationProvider(t)
	owner := completeSetup(t, handler)
	outsider := registerAndSignIn(t, handler, "amina")

	private := makeRoom(t, handler, owner, "council", "private")
	secret := sendMessage(t, handler, owner, private, "the private plan")
	public := makeRoom(t, handler, owner, "general", "public")

	recorder, _ := translate(t, handler, outsider, private, "libretranslate", secret)
	if recorder.Code != http.StatusNotFound {
		t.Errorf("an outsider translating a private room got %d, want 404", recorder.Code)
	}

	// Nor by naming the private message through a room they can read.
	recorder, answer := translate(t, handler, outsider, public, "libretranslate", secret)
	if recorder.Code != http.StatusOK || len(answer.Translations) != 0 || len(answer.Missing) != 1 {
		t.Errorf("a private message asked for through a public room: %d %s", recorder.Code, recorder.Body.String())
	}

	if fake.count() != 0 || strings.Contains(fake.sent(), "private plan") {
		t.Fatal("a message the reader may not read reached the provider")
	}

	// And a removed message is not translated either.
	mine := sendMessage(t, handler, owner, public, "soon gone")
	mustCall(t, handler, http.MethodDelete, "/api/rooms/"+public+"/messages/"+mine, nil, owner, http.StatusCreated)
	if _, answer := translate(t, handler, owner, public, "libretranslate", mine); len(answer.Missing) != 1 {
		t.Errorf("a removed message was translated: %+v", answer)
	}
}

/*
TestTranslationKeepsCodeMentionsAndLinksAndCaches covers what is sent, what
comes back, and when a provider is asked at all.
*/
func TestTranslationKeepsCodeMentionsAndLinksAndCaches(t *testing.T) {
	handler, fake, _ := withTranslationProvider(t)
	token := completeSetup(t, handler)
	roomID := makeRoom(t, handler, token, "general", "public")

	id := sendMessage(t, handler, token, roomID, "run `make test` @amina https://tdc.example now")
	recorder, answer := translate(t, handler, token, roomID, "libretranslate", id)
	if recorder.Code != http.StatusOK {
		t.Fatalf("translating returned %d: %s", recorder.Code, recorder.Body.String())
	}
	if got := answer.Translations[0].Body; got != "sw<run> `make test` @amina https://tdc.example sw<now>" {
		t.Errorf("translated body = %q", got)
	}
	for _, kept := range []string{"make test", "@amina", "tdc.example"} {
		if strings.Contains(fake.sent(), kept) {
			t.Errorf("%q was sent to the provider", kept)
		}
	}

	// Asked again, by anybody who can read it: no second request.
	other := registerAndSignIn(t, handler, "juma")
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/join", nil, other, http.StatusOK)
	if recorder, _ := translate(t, handler, other, roomID, "libretranslate", id); recorder.Code != http.StatusOK || fake.count() != 1 {
		t.Errorf("a cached translation went to the provider again (%d requests)", fake.count())
	}

	// Edited: the new words are translated, the old translation goes.
	mustCall(t, handler, http.MethodPatch, "/api/rooms/"+roomID+"/messages/"+id,
		map[string]string{"body": "edited words"}, token, http.StatusCreated)
	_, answer = translate(t, handler, token, roomID, "libretranslate", id)
	if fake.count() != 2 || answer.Translations[0].Body != "sw<edited words>" {
		t.Errorf("after an edit: %d requests, body %q", fake.count(), answer.Translations[0].Body)
	}
	if cachedTranslations(t) != 1 {
		t.Errorf("%d translations kept after an edit, want only the current one", cachedTranslations(t))
	}

	// Removed: its translation goes with it.
	mustCall(t, handler, http.MethodDelete, "/api/rooms/"+roomID+"/messages/"+id, nil, token, http.StatusCreated)
	if cachedTranslations(t) != 0 {
		t.Error("a removed message's translation was kept")
	}

	// Already in the target language: said so, and shown as written.
	sw := sendMessage(t, handler, token, roomID, "sw: habari")
	if _, answer := translate(t, handler, token, roomID, "libretranslate", sw); !answer.Translations[0].Skipped ||
		answer.Translations[0].Body != "sw: habari" {
		t.Errorf("a message already in Swahili: %+v", answer.Translations[0])
	}
}

/*
TestTranslationIsLimitedPerPerson counts requests that reach a provider, and
only those.
*/
func TestTranslationIsLimitedPerPerson(t *testing.T) {
	handler, fake, _ := withTranslationProvider(t)
	token := completeSetup(t, handler)
	setSetting(t, handler, token, "translation.requests_per_hour", "1")
	roomID := makeRoom(t, handler, token, "general", "public")

	first := sendMessage(t, handler, token, roomID, "one")
	if recorder, _ := translate(t, handler, token, roomID, "libretranslate", first); recorder.Code != http.StatusOK {
		t.Fatalf("the first translation returned %d", recorder.Code)
	}
	second := sendMessage(t, handler, token, roomID, "two")
	recorder, _ := translate(t, handler, token, roomID, "libretranslate", second)
	if recorder.Code != http.StatusTooManyRequests || recorder.Header().Get("Retry-After") == "" {
		t.Errorf("a second translation in the hour returned %d, want 429 with Retry-After", recorder.Code)
	}
	if fake.count() != 1 {
		t.Errorf("the provider received %d requests past the limit", fake.count())
	}
	// What is already translated is still served.
	if recorder, _ := translate(t, handler, token, roomID, "libretranslate", first); recorder.Code != http.StatusOK {
		t.Errorf("a cached translation was refused by the limit: %d", recorder.Code)
	}
}

/*
TestAPersonalEndpointCannotReachThisServersNetwork: an address a member types
is refused when it is a private address, and a name that resolves to one is
refused when it is dialled.
*/
func TestAPersonalEndpointCannotReachThisServersNetwork(t *testing.T) {
	handler, fake, _ := withTranslationProvider(t)
	token := completeSetup(t, handler)

	for _, endpoint := range []string{"https://127.0.0.1:5000", "https://10.1.2.3", "http://translate.example.org"} {
		if recorder := call(t, handler, http.MethodPut, "/api/me/translation/connections/libretranslate",
			map[string]string{"endpoint": endpoint}, token); recorder.Code != http.StatusBadRequest {
			t.Errorf("connecting %s returned %d, want 400", endpoint, recorder.Code)
		}
	}

	/*
		A name, which passes when saved and resolves to loopback when used.

		Pointed at a bare listener that counts connections rather than at the
		stand-in: a TLS request to a plain HTTP server fails before the server
		sees anything, which would pass this test with no guard at all. A
		connection accepted is the thing the guard exists to prevent.
	*/
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	var accepted atomic.Int32
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			_ = connection.Close()
		}
	}()
	port := listener.Addr().String()[strings.LastIndex(listener.Addr().String(), ":"):]
	mustCall(t, handler, http.MethodPut, "/api/me/translation/connections/libretranslate",
		map[string]string{"endpoint": "https://localhost" + port}, token, http.StatusNoContent)

	roomID := makeRoom(t, handler, token, "general", "public")
	id := sendMessage(t, handler, token, roomID, "hello")
	if recorder, _ := translate(t, handler, token, roomID, "libretranslate", id); recorder.Code != http.StatusBadGateway {
		t.Errorf("translating through a loopback endpoint returned %d, want 502", recorder.Code)
	}
	if accepted.Load() != 0 || fake.count() != 0 {
		t.Fatal("a personal endpoint on loopback was connected to")
	}
}

// TestTheOperatorDecidesWhichProvidersMayBeUsed.
func TestTheOperatorDecidesWhichProvidersMayBeUsed(t *testing.T) {
	handler, fake, _ := withTranslationProvider(t)
	token := completeSetup(t, handler)
	setSetting(t, handler, token, "translation.providers", "device,libretranslate")

	if recorder := call(t, handler, http.MethodPut, "/api/me/translation/connections/google",
		map[string]string{"apiKey": "k"}, token); recorder.Code != http.StatusForbidden {
		t.Errorf("connecting a denied provider returned %d, want 403", recorder.Code)
	}
	roomID := makeRoom(t, handler, token, "general", "public")
	id := sendMessage(t, handler, token, roomID, "hello")
	if recorder, _ := translate(t, handler, token, roomID, "google", id); recorder.Code != http.StatusForbidden {
		t.Errorf("translating with a denied provider returned %d, want 403", recorder.Code)
	}
	if recorder, _ := translate(t, handler, token, roomID, "device", id); recorder.Code != http.StatusBadRequest {
		t.Errorf("asking the server to translate on-device returned %d, want 400", recorder.Code)
	}
	if fake.count() != 0 {
		t.Error("a refused request reached a provider")
	}
}

func TestTranslationPreferencesAndRoomRules(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)
	roomID := makeRoom(t, handler, token, "general", "public")

	mustCall(t, handler, http.MethodPut, "/api/me/translation/preferences",
		map[string]any{"provider": "libretranslate", "targetLanguage": "sw", "autoTranslate": true}, token, http.StatusNoContent)
	mustCall(t, handler, http.MethodPut, "/api/me/translation/rooms/"+roomID,
		map[string]any{"autoTranslate": false}, token, http.StatusNoContent)

	overview := decode(t, mustCall(t, handler, http.MethodGet, "/api/me/translation", nil, token, http.StatusOK))
	preferences := overview["preferences"].(map[string]any)
	if preferences["provider"] != "libretranslate" || preferences["targetLanguage"] != "sw" || preferences["autoTranslate"] != true {
		t.Errorf("preferences = %v", preferences)
	}
	if rooms := overview["rooms"].([]any); len(rooms) != 1 || rooms[0].(map[string]any)["autoTranslate"] != false {
		t.Errorf("room rules = %v", rooms)
	}

	mustCall(t, handler, http.MethodDelete, "/api/me/translation/rooms/"+roomID, nil, token, http.StatusNoContent)
	if rooms := decode(t, mustCall(t, handler, http.MethodGet, "/api/me/translation", nil, token, http.StatusOK))["rooms"].([]any); len(rooms) != 0 {
		t.Errorf("a cleared rule is still listed: %v", rooms)
	}

	for _, bad := range []map[string]any{
		{"provider": "babelfish"},
		{"targetLanguage": "not a language"},
	} {
		if recorder := call(t, handler, http.MethodPut, "/api/me/translation/preferences", bad, token); recorder.Code != http.StatusBadRequest {
			t.Errorf("preferences %v returned %d, want 400", bad, recorder.Code)
		}
	}

	// A rule for a room the person cannot see says nothing about it.
	outsider := registerAndSignIn(t, handler, "amina")
	private := makeRoom(t, handler, token, "council", "private")
	if recorder := call(t, handler, http.MethodPut, "/api/me/translation/rooms/"+private,
		map[string]any{"autoTranslate": true}, outsider); recorder.Code != http.StatusNotFound {
		t.Errorf("a rule for an unseen private room returned %d, want 404", recorder.Code)
	}
}
