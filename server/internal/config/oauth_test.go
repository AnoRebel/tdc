package config_test

import (
	"strings"
	"testing"

	"github.com/AnoRebel/tdc/server/internal/config"
)

// withDatabase sets the one setting every configuration needs, so each test
// below is about oauth and nothing else.
func withDatabase(t *testing.T) {
	t.Helper()
	t.Setenv("TDC_DATABASE_URL", "postgres://tdc@localhost/tdc")
}

// TestNoProvidersIsTheDefault covers the case almost every server runs in:
// usernames and passwords, and no third party involved in signing in.
func TestNoProvidersIsTheDefault(t *testing.T) {
	withDatabase(t)

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("a configuration with no sign-in providers was refused: %v", err)
	}
	if len(cfg.OAuth.Providers) != 0 {
		t.Errorf("a provider is configured by default; signing in through one must be opt-in")
	}
}

/*
TestAProviderIsReadFromItsOwnNamespace is the shape of the whole feature.

The names come from one list, and each name opens a namespace under it. Getting
this wrong would mean a server that reads the settings of a provider nobody
named, or ignores the settings of one somebody did.
*/
func TestAProviderIsReadFromItsOwnNamespace(t *testing.T) {
	withDatabase(t)
	t.Setenv("TDC_OAUTH_PROVIDERS", "github")
	t.Setenv("TDC_OAUTH_GITHUB_LABEL", "GitHub")
	t.Setenv("TDC_OAUTH_GITHUB_CLIENT_ID", "abc")
	t.Setenv("TDC_OAUTH_GITHUB_CLIENT_SECRET", "shhh")
	t.Setenv("TDC_OAUTH_GITHUB_AUTH_URL", "https://github.com/login/oauth/authorize")
	t.Setenv("TDC_OAUTH_GITHUB_TOKEN_URL", "https://github.com/login/oauth/access_token")
	t.Setenv("TDC_OAUTH_GITHUB_USER_URL", "https://api.github.com/user")
	t.Setenv("TDC_OAUTH_GITHUB_SCOPES", "read:user, user:email")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("a complete provider was refused: %v", err)
	}
	if len(cfg.OAuth.Providers) != 1 {
		t.Fatalf("got %d providers, want 1", len(cfg.OAuth.Providers))
	}

	provider := cfg.OAuth.Providers[0]
	if provider.Name != "github" {
		t.Errorf("name is %q, want github", provider.Name)
	}
	if provider.Label != "GitHub" {
		t.Errorf("label is %q, want GitHub", provider.Label)
	}
	if provider.ClientSecret != "shhh" {
		t.Errorf("client secret is %q, want shhh", provider.ClientSecret)
	}
	if len(provider.Scopes) != 2 || provider.Scopes[0] != "read:user" || provider.Scopes[1] != "user:email" {
		t.Errorf("scopes are %v, want [read:user user:email]", provider.Scopes)
	}
}

// TestTheLabelFallsBackToTheName means a provider is usable without one, and
// what appears on the button is at worst the name that was already typed.
func TestTheLabelFallsBackToTheName(t *testing.T) {
	withDatabase(t)
	completeProvider(t, "Forgejo")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("a provider without a label was refused: %v", err)
	}
	if got := cfg.OAuth.Providers[0].Label; got != "Forgejo" {
		t.Errorf("label is %q; expected it to fall back to the name as written", got)
	}
	if got := cfg.OAuth.Providers[0].Name; got != "forgejo" {
		t.Errorf("name is %q; expected it folded to lower case", got)
	}
}

/*
TestAnIncompleteProviderIsRefusedAtStartup is the point of validating this at
all.

A provider missing its secret or an endpoint cannot complete a sign-in. Starting
anyway produces a button that leads to a broken page, and the person who finds
out is whoever clicks it rather than whoever configured it.
*/
func TestAnIncompleteProviderIsRefusedAtStartup(t *testing.T) {
	settings := map[string]string{
		"CLIENT_ID":     "oauth.gitlab.client_id",
		"CLIENT_SECRET": "oauth.gitlab.client_secret",
		"AUTH_URL":      "oauth.gitlab.auth_url",
		"TOKEN_URL":     "oauth.gitlab.token_url",
		"USER_URL":      "oauth.gitlab.user_url",
	}

	for missing, expect := range settings {
		t.Run("without "+missing, func(t *testing.T) {
			withDatabase(t)
			completeProvider(t, "gitlab")
			t.Setenv("TDC_OAUTH_GITLAB_"+missing, "")

			_, err := config.Load("")
			if err == nil {
				t.Fatalf("a provider with no %s was accepted", missing)
			}
			if !strings.Contains(err.Error(), expect) {
				t.Errorf("the refusal is %q; expected it to name %s", err, expect)
			}
		})
	}
}

/*
TestPlainHTTPEndpointsAreRefused keeps the client secret off the wire.

The secret is sent to the token endpoint and an access token comes back; over
plain HTTP both are readable by anything between here and there. A warning
would be ignored, so this is a refusal.
*/
func TestPlainHTTPEndpointsAreRefused(t *testing.T) {
	withDatabase(t)
	completeProvider(t, "keycloak")
	t.Setenv("TDC_OAUTH_KEYCLOAK_TOKEN_URL", "http://keycloak.internal/token")

	_, err := config.Load("")
	if err == nil {
		t.Fatal("a provider reached over plain http was accepted")
	}
	if !strings.Contains(err.Error(), "oauth.keycloak.token_url") {
		t.Errorf("the refusal is %q; expected it to name oauth.keycloak.token_url", err)
	}
}

// TestAnUnusableProviderNameIsRefused: the name becomes part of a settings key,
// an environment variable and a callback path, so it is kept narrow.
func TestAnUnusableProviderNameIsRefused(t *testing.T) {
	for _, name := range []string{"git hub", "git.hub", "../etc", ""} {
		t.Run("named "+name, func(t *testing.T) {
			withDatabase(t)
			t.Setenv("TDC_OAUTH_PROVIDERS", name)

			cfg, err := config.Load("")
			if name == "" {
				// An empty entry is discarded by the list reader, which leaves
				// no providers rather than a broken one.
				if err != nil {
					t.Fatalf("an empty provider list was refused: %v", err)
				}
				if len(cfg.OAuth.Providers) != 0 {
					t.Error("an empty name produced a provider")
				}
				return
			}
			if err == nil {
				t.Fatalf("a provider named %q was accepted", name)
			}
			if !strings.Contains(err.Error(), "oauth.providers") {
				t.Errorf("the refusal is %q; expected it to name oauth.providers", err)
			}
		})
	}
}

// TestTheSameProviderTwiceIsRefused, because the second would silently read the
// same settings as the first and produce two identical buttons.
func TestTheSameProviderTwiceIsRefused(t *testing.T) {
	withDatabase(t)
	completeProvider(t, "github")
	t.Setenv("TDC_OAUTH_PROVIDERS", "github,GitHub")

	_, err := config.Load("")
	if err == nil {
		t.Fatal("the same provider listed twice was accepted")
	}
	if !strings.Contains(err.Error(), "oauth.providers") {
		t.Errorf("the refusal is %q; expected it to name oauth.providers", err)
	}
}

/*
TestTheClientSecretIsRedacted guards the displayed configuration.

The secret's key contains the provider's name, so it cannot be in the fixed list
of secrets the way the database URL is. If the ending-match ever stopped
working, every provider's secret would appear in full wherever configuration is
shown.
*/
func TestTheClientSecretIsRedacted(t *testing.T) {
	withDatabase(t)
	completeProvider(t, "github")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("a complete provider was refused: %v", err)
	}

	var sawSecret, sawID bool
	for _, setting := range cfg.Describe("") {
		if setting.Key == "oauth.github.client_secret" {
			sawSecret = true
			if !setting.Secret {
				t.Error("the client secret is not marked secret")
			}
			if strings.Contains(setting.Value, "shhh") {
				t.Errorf("the client secret is displayed in full: %q", setting.Value)
			}
		}
		if setting.Key == "oauth.github.client_id" {
			sawID = true
			// Deliberately visible: every browser starting a sign-in is given
			// it, so hiding it would conceal nothing.
			if setting.Secret {
				t.Error("the client id is redacted; it is handed to every browser already")
			}
		}
	}
	if !sawSecret || !sawID {
		t.Error("a configured provider's settings are missing from the displayed configuration")
	}
}

// completeProvider sets everything one provider needs, so each test above
// changes only the one thing it is about.
func completeProvider(t *testing.T, name string) {
	t.Helper()
	upper := strings.ToUpper(name)
	t.Setenv("TDC_OAUTH_PROVIDERS", name)
	t.Setenv("TDC_OAUTH_"+upper+"_CLIENT_ID", "abc")
	t.Setenv("TDC_OAUTH_"+upper+"_CLIENT_SECRET", "shhh")
	t.Setenv("TDC_OAUTH_"+upper+"_AUTH_URL", "https://example.test/authorize")
	t.Setenv("TDC_OAUTH_"+upper+"_TOKEN_URL", "https://example.test/token")
	t.Setenv("TDC_OAUTH_"+upper+"_USER_URL", "https://example.test/user")
}
