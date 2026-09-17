package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/AnoRebel/tdc/server/internal/config"
)

// responseLimit caps what is read from a provider.
//
// A token response is a few hundred bytes and an account is a few thousand. A
// provider that answers with a gigabyte — misconfigured, compromised, or simply
// somebody else's server at that address — must not be able to exhaust memory
// here.
const responseLimit = 1 << 20

// tokenResponse is the part of an OAuth2 token response this server uses.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	// Error and its description are what a provider sends instead, and they
	// are read so the log says what went wrong rather than only that it did.
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// exchange turns an authorization code into an access token.
func (s *Service) exchange(
	ctx context.Context,
	provider config.OAuthProvider,
	code, verifier, callbackURL string,
) (string, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {callbackURL},
		"client_id":     {provider.ClientID},
		"client_secret": {provider.ClientSecret},
		"code_verifier": {verifier},
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.TokenURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("build token request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// GitHub answers form-encoded unless asked otherwise, and every other
	// provider answers JSON regardless, so asking costs nothing and settles it.
	request.Header.Set("Accept", "application/json")

	response, err := s.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrProviderRefused, err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, responseLimit))
	if err != nil {
		return "", fmt.Errorf("%w: reading the token response: %w", ErrProviderRefused, err)
	}

	var token tokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		// Some providers answer form-encoded even when asked for JSON.
		values, parseErr := url.ParseQuery(string(body))
		if parseErr != nil {
			return "", fmt.Errorf("%w: the token response was neither JSON nor form-encoded", ErrProviderRefused)
		}
		token = tokenResponse{
			AccessToken:      values.Get("access_token"),
			TokenType:        values.Get("token_type"),
			Error:            values.Get("error"),
			ErrorDescription: values.Get("error_description"),
		}
	}

	if token.Error != "" {
		// Logged in full, returned as the one error: the description is the
		// provider's own text and is not shown to whoever is signing in.
		s.log.Warn("oauth token exchange refused",
			"provider", provider.Name,
			"error", token.Error,
			"description", token.ErrorDescription)
		return "", ErrProviderRefused
	}
	if response.StatusCode != http.StatusOK {
		s.log.Warn("oauth token exchange failed",
			"provider", provider.Name, "status", response.StatusCode)
		return "", ErrProviderRefused
	}
	if token.AccessToken == "" {
		return "", fmt.Errorf("%w: no access token in the response", ErrProviderRefused)
	}

	return token.AccessToken, nil
}

/*
account is the union of what providers call the same things.

There is no agreed shape for this response — OpenID Connect names the subject
"sub", GitHub calls it "id" and sends a number, GitLab sends "id" too, Forgejo
sends both "id" and "sub". Rather than a provider-specific parser for each,
every spelling is read and the first one present wins, which means a provider
nobody here has heard of usually works without new code.
*/
type account struct {
	Sub   json.Number `json:"sub"`
	ID    json.Number `json:"id"`
	Email string      `json:"email"`

	// OpenID Connect spells this email_verified; GitLab and others use
	// confirmed_at, a timestamp that is present only once confirmed.
	EmailVerified *bool  `json:"email_verified"`
	ConfirmedAt   string `json:"confirmed_at"`

	PreferredUsername string `json:"preferred_username"`
	Login             string `json:"login"`
	Username          string `json:"username"`
	Nickname          string `json:"nickname"`

	Name     string `json:"name"`
	FullName string `json:"full_name"`
}

// fetchIdentity reads the signed-in account from the provider.
func (s *Service) fetchIdentity(ctx context.Context, provider config.OAuthProvider, token string) (Identity, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, provider.UserURL, nil)
	if err != nil {
		return Identity{}, fmt.Errorf("build account request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")

	response, err := s.client.Do(request)
	if err != nil {
		return Identity{}, fmt.Errorf("%w: %w", ErrProviderRefused, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		s.log.Warn("oauth account request failed",
			"provider", provider.Name, "status", response.StatusCode)
		return Identity{}, ErrProviderRefused
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, responseLimit))
	if err != nil {
		return Identity{}, fmt.Errorf("%w: reading the account: %w", ErrProviderRefused, err)
	}

	var parsed account
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Identity{}, fmt.Errorf("%w: the account response was not JSON", ErrProviderRefused)
	}

	subject := firstOf(parsed.Sub.String(), parsed.ID.String())
	if subject == "" {
		// Without a stable identifier there is nothing to key a credential on,
		// and keying it on the email would mean the account moves whenever the
		// email does.
		return Identity{}, fmt.Errorf("%w: the account has no identifier", ErrProviderRefused)
	}

	return Identity{
		Provider:      provider.Name,
		Subject:       subject,
		Email:         strings.TrimSpace(parsed.Email),
		EmailVerified: parsed.verified(),
		Username: firstOf(parsed.PreferredUsername, parsed.Login,
			parsed.Username, parsed.Nickname),
		DisplayName: firstOf(parsed.Name, parsed.FullName),
	}, nil
}

/*
verified is deliberately conservative.

Absent means no. A provider that does not say whether it verified an address has
not verified it as far as this server is concerned, because the whole linking
rule rests on this one boolean: treating silence as a yes would let anybody who
can type an email address at a permissive provider claim the account that
address belongs to here.
*/
func (a account) verified() bool {
	if a.EmailVerified != nil {
		return *a.EmailVerified
	}
	// A confirmation timestamp is the same claim in a different shape: present
	// means confirmed, and providers that use it omit or null it otherwise.
	return strings.TrimSpace(a.ConfirmedAt) != ""
}

// firstOf returns the first value that is not blank.
func firstOf(candidates ...string) string {
	for _, candidate := range candidates {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" && trimmed != "0" {
			return trimmed
		}
	}
	return ""
}
