package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/cache"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/settings"
	"github.com/AnoRebel/tdc/server/internal/translation"
)

/*
Inline message translation.

What a person chose, the providers they connected, and the proxy that sends
messages to a provider on their behalf. Translating on the reader's own device
needs none of the proxy: the client does it and only reads preferences here.
*/

type translationProviderResponse struct {
	Name string `json:"name"`
	// Allowed is the operator's choice; a provider not allowed is listed so a
	// client can say why it is missing.
	Allowed bool `json:"allowed"`
	// Connected is whether this person can use it now: their own connection,
	// or — for LibreTranslate — the operator's instance.
	Connected bool `json:"connected"`
	// HasKey is whether a key is saved. The key itself never leaves.
	HasKey bool `json:"hasKey"`
	// Shared is true for LibreTranslate when the operator provides an
	// instance everybody may use.
	Shared   bool   `json:"shared"`
	Endpoint string `json:"endpoint"`
	Region   string `json:"region"`
}

type translationPreferences struct {
	Provider       string `json:"provider"`
	TargetLanguage string `json:"targetLanguage"`
	AutoTranslate  bool   `json:"autoTranslate"`
}

type translationRoomRule struct {
	RoomID        uuid.UUID `json:"roomId"`
	AutoTranslate bool      `json:"autoTranslate"`
}

type translationOverviewResponse struct {
	Providers []translationProviderResponse `json:"providers"`
	// CanStoreKeys is false when the operator has not configured a secret key,
	// so a client can say why it cannot connect a provider with a key.
	CanStoreKeys bool `json:"canStoreKeys"`
	// DefaultLanguage is the server's interface language. An empty target
	// means the language the person reads the interface in, which their
	// client knows; this is what that falls back to.
	DefaultLanguage string                 `json:"defaultLanguage"`
	Preferences     translationPreferences `json:"preferences"`
	Rooms           []translationRoomRule  `json:"rooms"`
}

// allowedTranslationProviders is the operator's list.
func (s *Server) allowedTranslationProviders(ctx context.Context) []string {
	return settings.ListOf(s.settings.Get(ctx, settings.TranslationProviders))
}

func (s *Server) handleTranslationOverview(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)

	connections, err := s.queries.ListTranslationConnections(ctx, user.ID)
	if err != nil {
		return err
	}
	byProvider := map[string]dbgen.TranslationConnection{}
	for _, connection := range connections {
		byProvider[connection.Provider] = connection
	}

	allowed := s.allowedTranslationProviders(ctx)
	providers := make([]translationProviderResponse, 0, len(translation.Providers))
	for _, provider := range translation.Providers {
		name := string(provider)
		connection, connected := byProvider[name]
		response := translationProviderResponse{
			Name:     name,
			Allowed:  slices.Contains(allowed, name),
			HasKey:   connected && len(connection.SealedKey) > 0,
			Endpoint: connection.Endpoint,
			Region:   connection.Region,
		}
		switch provider {
		case translation.Device:
			// Whether the browser has it is the browser's to say.
			response.Connected = true
		case translation.LibreTranslate:
			response.Shared = s.translation.SharedInstance()
			response.Connected = connected || response.Shared
		default:
			response.Connected = response.HasKey
		}
		providers = append(providers, response)
	}

	overview := translationOverviewResponse{
		Providers:       providers,
		CanStoreKeys:    s.translation.CanStoreKeys(),
		DefaultLanguage: s.config.DefaultLanguage,
		Rooms:           []translationRoomRule{},
	}

	preferences, err := s.queries.GetTranslationPreferences(ctx, user.ID)
	switch {
	case err == nil:
		overview.Preferences = translationPreferences{
			Provider:       preferences.Provider,
			TargetLanguage: preferences.TargetLanguage,
			AutoTranslate:  preferences.AutoTranslate,
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return err
	}

	rules, err := s.queries.ListTranslationRoomRules(ctx, user.ID)
	if err != nil {
		return err
	}
	for _, rule := range rules {
		overview.Rooms = append(overview.Rooms, translationRoomRule{RoomID: rule.RoomID, AutoTranslate: rule.AutoTranslate})
	}

	return c.JSON(http.StatusOK, overview)
}

/*
languageTag accepts a BCP 47 tag of the shape providers use: a primary subtag
and optional subtags ("sw", "pt-BR", "zh-Hans"). Not a list of languages —
which ones a provider supports is the provider's to say, and they differ.
*/
var languageTag = regexp.MustCompile(`^[a-zA-Z]{2,3}(-[a-zA-Z0-9]{2,8}){0,2}$`)

func (s *Server) handleSetTranslationPreferences(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)

	var request translationPreferences
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	if request.Provider != "" && !translation.Known(request.Provider) {
		return echo.NewHTTPError(http.StatusBadRequest, "that is not a translation provider")
	}
	if request.Provider != "" && !slices.Contains(s.allowedTranslationProviders(ctx), request.Provider) {
		return echo.NewHTTPError(http.StatusForbidden, "this server does not allow that translation provider")
	}
	if request.TargetLanguage != "" && !languageTag.MatchString(request.TargetLanguage) {
		return echo.NewHTTPError(http.StatusBadRequest, "that is not a language code")
	}

	if err := s.queries.SetTranslationPreferences(ctx, dbgen.SetTranslationPreferencesParams{
		UserID:         user.ID,
		Provider:       request.Provider,
		TargetLanguage: request.TargetLanguage,
		AutoTranslate:  request.AutoTranslate,
	}); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

type translationConnectionRequest struct {
	APIKey   string `json:"apiKey"`
	Endpoint string `json:"endpoint"`
	Region   string `json:"region"`
}

// azureRegion is the shape of an Azure region name ("westeurope").
var azureRegion = regexp.MustCompile(`^[a-z0-9]{2,40}$`)

func (s *Server) handleConnectTranslation(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)

	provider := translation.Provider(c.Param("provider"))
	if !translation.Known(string(provider)) || provider == translation.Device {
		return echo.NewHTTPError(http.StatusNotFound, "that is not a provider to connect")
	}
	if !slices.Contains(s.allowedTranslationProviders(ctx), string(provider)) {
		return echo.NewHTTPError(http.StatusForbidden, "this server does not allow that translation provider")
	}

	var request translationConnectionRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	key := strings.TrimSpace(request.APIKey)
	if len(key) > 512 {
		return echo.NewHTTPError(http.StatusBadRequest, "that key is too long to be one")
	}

	existing, err := s.queries.GetTranslationConnection(ctx, dbgen.GetTranslationConnectionParams{
		UserID: user.ID, Provider: string(provider),
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	// A blank key keeps the one already saved, so changing a region or an
	// address does not mean pasting the key again.
	sealed := existing.SealedKey
	if key != "" {
		if sealed, err = s.translation.Seal(user.ID, provider, key); errors.Is(err, translation.ErrCannotStoreKeys) {
			return echo.NewHTTPError(http.StatusServiceUnavailable,
				"this server is not set up to store translation keys; ask its operator")
		} else if err != nil {
			return err
		}
	}

	endpoint, region := "", ""
	switch provider {
	case translation.LibreTranslate:
		if endpoint, err = translation.ValidateEndpoint(request.Endpoint); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
	case translation.Microsoft:
		region = strings.ToLower(strings.TrimSpace(request.Region))
		if region != "" && !azureRegion.MatchString(region) {
			return echo.NewHTTPError(http.StatusBadRequest, "that is not an Azure region name")
		}
	}
	if provider.NeedsKey() && len(sealed) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "this provider needs a key")
	}

	if err := s.queries.SetTranslationConnection(ctx, dbgen.SetTranslationConnectionParams{
		UserID:    user.ID,
		Provider:  string(provider),
		SealedKey: sealed,
		Endpoint:  endpoint,
		Region:    region,
	}); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (s *Server) handleDisconnectTranslation(c *echo.Context) error {
	user, _ := currentUser(c)
	if err := s.queries.DeleteTranslationConnection(c.Request().Context(), dbgen.DeleteTranslationConnectionParams{
		UserID: user.ID, Provider: c.Param("provider"),
	}); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

type translationRoomRuleRequest struct {
	AutoTranslate bool `json:"autoTranslate"`
}

func (s *Server) handleSetTranslationRoomRule(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}
	// A rule for a room somebody cannot read would say the room exists.
	if _, err := s.rooms.Visible(ctx, roomID, user.ID); err != nil {
		return roomError(err)
	}

	var request translationRoomRuleRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	if err := s.queries.SetTranslationRoomRule(ctx, dbgen.SetTranslationRoomRuleParams{
		UserID: user.ID, RoomID: roomID, AutoTranslate: request.AutoTranslate,
	}); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (s *Server) handleClearTranslationRoomRule(c *echo.Context) error {
	user, _ := currentUser(c)
	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}
	if err := s.queries.DeleteTranslationRoomRule(c.Request().Context(), dbgen.DeleteTranslationRoomRuleParams{
		UserID: user.ID, RoomID: roomID,
	}); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

type translateRequest struct {
	Messages       []string `json:"messages"`
	TargetLanguage string   `json:"targetLanguage"`
	Provider       string   `json:"provider"`
}

type translatedMessage struct {
	MessageID      string `json:"messageId"`
	Body           string `json:"body"`
	SourceLanguage string `json:"sourceLanguage"`
	// Skipped is true when the message is already in the target language;
	// Body is then the message as written.
	Skipped bool `json:"skipped"`
}

type translateResponse struct {
	Provider       string              `json:"provider"`
	TargetLanguage string              `json:"targetLanguage"`
	Translations   []translatedMessage `json:"translations"`
	// Missing are messages that cannot be translated for this person: not in
	// this room, not readable, removed, or not a message. Which is not said.
	Missing []string `json:"missing"`
}

// maxTranslateMessages bounds one request: a screen of conversation.
const maxTranslateMessages = 50

// pendingTranslation is a message that needs a provider.
type pendingTranslation struct {
	messageID string
	sourceID  string
	body      string
	hash      []byte
	segments  []translation.Segment
}

/*
handleTranslateMessages translates messages through a provider on this
person's behalf.

The room is checked first, as every room route does: a room this person cannot
read is 404, and nothing about its messages is looked at. Each message is then
taken only if it is a readable message in that room; anything else is
returned as missing, without saying which reason applied.

The hourly limit counts requests that reach a provider. Translations already
made are free, which is what lets a busy channel be translated automatically
without everybody in it spending their allowance on the same messages.
*/
func (s *Server) handleTranslateMessages(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}
	if _, err := s.rooms.Visible(ctx, roomID, user.ID); err != nil {
		return roomError(err)
	}

	var request translateRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	if len(request.Messages) == 0 || len(request.Messages) > maxTranslateMessages {
		return echo.NewHTTPError(http.StatusBadRequest, "ask for between 1 and 50 messages")
	}
	if !languageTag.MatchString(request.TargetLanguage) {
		return echo.NewHTTPError(http.StatusBadRequest, "that is not a language code")
	}
	provider := translation.Provider(request.Provider)
	if !translation.Known(request.Provider) || provider == translation.Device {
		return echo.NewHTTPError(http.StatusBadRequest,
			"that is not a provider this server translates with")
	}
	if !slices.Contains(s.allowedTranslationProviders(ctx), request.Provider) {
		return echo.NewHTTPError(http.StatusForbidden, "this server does not allow that translation provider")
	}

	credentials, err := s.translationCredentials(ctx, user.ID, provider)
	if err != nil {
		return err
	}

	response := translateResponse{
		Provider:       request.Provider,
		TargetLanguage: request.TargetLanguage,
		Translations:   []translatedMessage{},
		Missing:        []string{},
	}

	var pending []pendingTranslation
	for _, messageID := range dedupe(request.Messages) {
		body, sourceID, ok := s.readableBody(ctx, roomID, messageID)
		if !ok {
			response.Missing = append(response.Missing, messageID)
			continue
		}
		hash := sha256.Sum256([]byte(body))

		cached, err := s.queries.GetMessageTranslation(ctx, dbgen.GetMessageTranslationParams{
			MessageID: messageID, TargetLanguage: request.TargetLanguage,
			Provider: request.Provider, BodyHash: hash[:],
		})
		if err == nil {
			response.Translations = append(response.Translations,
				translatedOf(messageID, body, cached.TranslatedBody, cached.SourceLanguage, request.TargetLanguage))
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		segments := translation.Split(body)
		if len(translation.Translatable(segments)) == 0 {
			// Nothing in it is words: code, a link, an emoji. As written.
			response.Translations = append(response.Translations, translatedMessage{
				MessageID: messageID, Body: body, Skipped: true,
			})
			continue
		}
		pending = append(pending, pendingTranslation{
			messageID: messageID, sourceID: sourceID, body: body, hash: hash[:], segments: segments,
		})
	}

	if len(pending) > 0 {
		decision, err := s.limiter.Allow(ctx, "translate:"+user.ID.String(), cache.RateLimit{
			Attempts: int(s.settings.Int(ctx, settings.TranslationRequestsPerHour)),
			Window:   time.Hour,
		})
		if err != nil {
			return err
		}
		if !decision.Allowed {
			return retryAfter(c, decision.RetryAfter)
		}

		translated, err := s.translatePending(ctx, provider, credentials, pending, request.TargetLanguage)
		if err != nil {
			return translationError(err)
		}
		response.Translations = append(response.Translations, translated...)
	}

	return c.JSON(http.StatusOK, response)
}

// translatePending sends every pending message's words in one batch and
// stores what comes back.
func (s *Server) translatePending(
	ctx context.Context,
	provider translation.Provider,
	credentials *translation.Credentials,
	pending []pendingTranslation,
	target string,
) ([]translatedMessage, error) {
	var texts []string
	for _, item := range pending {
		texts = append(texts, translation.Translatable(item.segments)...)
	}

	results, err := s.translation.Translate(ctx, provider, credentials, texts, target)
	if err != nil {
		return nil, err
	}

	out := make([]translatedMessage, 0, len(pending))
	next := 0
	for _, item := range pending {
		count := len(translation.Translatable(item.segments))
		pieces := make([]string, count)
		// The language of the longest piece speaks for the message: a
		// greeting in one language and a paragraph in another is the
		// paragraph's language.
		source, longest := "", -1
		for i := range count {
			result := results[next+i]
			pieces[i] = result.Text
			if result.Source != "" && len(texts[next+i]) > longest {
				source, longest = result.Source, len(texts[next+i])
			}
		}
		next += count

		body := translation.Replace(item.segments, pieces)
		if err := s.queries.StoreMessageTranslation(ctx, dbgen.StoreMessageTranslationParams{
			MessageID:      item.messageID,
			SourceEventID:  item.sourceID,
			TargetLanguage: target,
			Provider:       string(provider),
			BodyHash:       item.hash,
			TranslatedBody: body,
			SourceLanguage: source,
		}); err != nil {
			// A translation that could not be kept is still a translation.
			s.log.Warn("cannot keep a translation", "error", err)
		}
		out = append(out, translatedOf(item.messageID, item.body, body, source, target))
	}
	return out, nil
}

// translatedOf builds one answer, reporting a message already in the target
// language as skipped and as written.
func translatedOf(messageID, original, translated, source, target string) translatedMessage {
	if source != "" && source == translation.Primary(target) {
		return translatedMessage{MessageID: messageID, Body: original, SourceLanguage: source, Skipped: true}
	}
	return translatedMessage{MessageID: messageID, Body: translated, SourceLanguage: source}
}

/*
readableBody returns a message's current words and the event they came from,
or false when this person may not have it translated.

The room has already been checked. What is left is the message: in this room,
a message rather than anything else in the log, and not removed. Its words are
its newest edit's, when it has one, because that is what a reader sees.
*/
func (s *Server) readableBody(ctx context.Context, roomID uuid.UUID, messageID string) (string, string, bool) {
	event, err := s.rooms.EventInRoom(ctx, roomID, messageID)
	if err != nil || event.Type != string(events.TypeMessageSent) || event.RedactedAt != nil {
		return "", "", false
	}

	source := event
	if edit, err := s.queries.LatestMessageEdit(ctx, dbgen.LatestMessageEditParams{
		RoomID: roomID, EventID: messageID,
	}); err == nil {
		source = edit
	}

	var content struct {
		Body string `json:"body"`
	}
	if json.Unmarshal(source.Content, &content) != nil || strings.TrimSpace(content.Body) == "" {
		return "", "", false
	}
	return content.Body, source.ID, true
}

// translationCredentials resolves what to call a provider with: this
// person's connection, or for LibreTranslate the operator's instance.
func (s *Server) translationCredentials(
	ctx context.Context,
	userID uuid.UUID,
	provider translation.Provider,
) (*translation.Credentials, error) {
	connection, err := s.queries.GetTranslationConnection(ctx, dbgen.GetTranslationConnectionParams{
		UserID: userID, Provider: string(provider),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		if provider == translation.LibreTranslate && s.translation.SharedInstance() {
			return nil, nil
		}
		return nil, echo.NewHTTPError(http.StatusConflict, "connect this translation provider first")
	}
	if err != nil {
		return nil, err
	}

	key, err := s.translation.Open(userID, provider, connection.SealedKey)
	if err != nil {
		// A key sealed under a secret key the server no longer has. Said as
		// what the person can do about it.
		return nil, echo.NewHTTPError(http.StatusConflict,
			"the saved key for this provider can no longer be read; connect it again")
	}
	return &translation.Credentials{APIKey: key, Endpoint: connection.Endpoint, Region: connection.Region}, nil
}

func translationError(err error) error {
	switch {
	case errors.Is(err, translation.ErrNotConnected):
		return echo.NewHTTPError(http.StatusConflict, "connect this translation provider first")
	default:
		// The details were logged, without the text; the caller learns only
		// that it failed.
		return echo.NewHTTPError(http.StatusBadGateway, "the translation provider could not be reached")
	}
}

func dedupe(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}
