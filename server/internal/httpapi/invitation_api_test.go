package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

type issuedInvitation struct {
	ID     string `json:"id"`
	Token  string `json:"token"`
	Code   string `json:"code"`
	URL    string `json:"url"`
	Uses   int32  `json:"uses"`
	Usable bool   `json:"usable"`
}

// issueInvitation creates one for a channel and returns it.
func issueInvitation(
	t *testing.T,
	handler http.Handler,
	token, roomID string,
	terms map[string]any,
) issuedInvitation {
	t.Helper()

	recorder := call(t, handler, http.MethodPost,
		"/api/rooms/"+roomID+"/invitations", terms, token)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("issuing returned %d: %s", recorder.Code, truncate(recorder.Body.String()))
	}

	var issued issuedInvitation
	if err := json.Unmarshal(recorder.Body.Bytes(), &issued); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	return issued
}

// redeem attempts to accept an invitation and returns the status.
func redeem(t *testing.T, handler http.Handler, token string, body map[string]any) int {
	t.Helper()
	return call(t, handler, http.MethodPost, "/api/invitations/redeem", body, token).Code
}

/*
TestAcceptingAnInvitationGrantsThatChannelOnly is the rule the whole feature
turns on.

An invitation names one channel. Accepting it must admit somebody to that and
nothing else — otherwise a link to a public channel becomes a way into every
private one on the server.
*/
func TestAcceptingAnInvitationGrantsThatChannelOnly(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	invited := makeRoom(t, handler, owner, "wageni", "private")
	other := makeRoom(t, handler, owner, "nyingine", "private")

	issued := issueInvitation(t, handler, owner, invited, nil)

	newcomer := registerAndSignIn(t, handler, "amina")

	if code := redeem(t, handler, newcomer, map[string]any{"token": issued.Token}); code != http.StatusOK {
		t.Fatalf("redeeming returned %d", code)
	}

	// In the invited channel.
	inside := call(t, handler, http.MethodGet, "/api/rooms/"+invited, nil, newcomer)
	if inside.Code != http.StatusOK {
		t.Errorf("the invited channel returned %d after accepting", inside.Code)
	}

	// And nothing else. The other private channel is still invisible.
	outside := call(t, handler, http.MethodGet, "/api/rooms/"+other, nil, newcomer)
	if outside.Code == http.StatusOK {
		t.Error("accepting one invitation granted a channel it did not name")
	}
}

/*
TestEveryRefusalLooksTheSame is the rule that stops this being an oracle.

Expired, exhausted, revoked and never-existent must be indistinguishable. A
response that said "expired" would tell somebody guessing codes that they had
guessed a real one, which is most of the work of guessing it.
*/
func TestEveryRefusalLooksTheSame(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "wageni", "private")

	// Exhausted: one use, already taken.
	oneUse := issueInvitation(t, handler, owner, roomID, map[string]any{"maxUses": 1})
	firstTaker := registerAndSignIn(t, handler, "wa-kwanza")
	if code := redeem(t, handler, firstTaker, map[string]any{"token": oneUse.Token}); code != http.StatusOK {
		t.Fatalf("the first use returned %d", code)
	}

	// Revoked.
	revoked := issueInvitation(t, handler, owner, roomID, nil)
	if r := call(t, handler, http.MethodDelete, "/api/invitations/"+revoked.ID, nil, owner); r.Code != http.StatusNoContent {
		t.Fatalf("revoking returned %d: %s", r.Code, truncate(r.Body.String()))
	}

	// Bound to somebody else.
	bound := issueInvitation(t, handler, owner, roomID,
		map[string]any{"boundTo": "mtu.mwingine@example.com"})

	stranger := registerAndSignIn(t, handler, "amina")

	refusals := map[string]map[string]any{
		"exhausted":        {"token": oneUse.Token},
		"revoked":          {"token": revoked.Token},
		"bound to another": {"token": bound.Token},
		"never existed":    {"token": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
		"empty":            {"token": ""},
	}

	bodies := map[string]string{}
	for name, body := range refusals {
		recorder := call(t, handler, http.MethodPost, "/api/invitations/redeem", body, stranger)

		if recorder.Code != http.StatusNotFound {
			t.Errorf("%s returned %d, expected 404 like every other refusal",
				name, recorder.Code)
		}
		bodies[name] = recorder.Body.String()
	}

	// Every message identical, not merely every status.
	var first string
	for name, body := range bodies {
		if first == "" {
			first = body
			continue
		}
		if body != first {
			t.Errorf("%s answered %q where another refusal answered %q; "+
				"a difference here tells somebody guessing when they guessed right",
				name, body, first)
		}
	}
}

/*
TestRevocationIsImmediate is what makes revoking mean anything.

Somebody who has already been sent a link cannot be un-sent it. The only way to
withdraw it is for the server to stop honouring it, at once — not at the next
expiry, and not for new holders only.
*/
func TestRevocationIsImmediate(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "wageni", "private")

	issued := issueInvitation(t, handler, owner, roomID, nil)

	// Somebody already holds it.
	holder := registerAndSignIn(t, handler, "amina")

	if r := call(t, handler, http.MethodDelete, "/api/invitations/"+issued.ID, nil, owner); r.Code != http.StatusNoContent {
		t.Fatalf("revoking returned %d", r.Code)
	}

	if code := redeem(t, handler, holder, map[string]any{"token": issued.Token}); code != http.StatusNotFound {
		t.Errorf("a revoked invitation was still accepted, returning %d", code)
	}
}

// TestAUseLimitIsEnforced covers the count.
func TestAUseLimitIsEnforced(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "wageni", "private")

	issued := issueInvitation(t, handler, owner, roomID, map[string]any{"maxUses": 2})

	for i, name := range []string{"amina", "juma", "neema"} {
		person := registerAndSignIn(t, handler, name)
		code := redeem(t, handler, person, map[string]any{"token": issued.Token})

		if i < 2 && code != http.StatusOK {
			t.Errorf("use %d returned %d, expected it to be accepted", i+1, code)
		}
		if i == 2 && code == http.StatusOK {
			t.Error("a third person used an invitation limited to two")
		}
	}
}

/*
TestAnInvitationBoundToSomebodyDoesNotTravel covers forwarding.

An invitation meant for one person should not work for whoever they pass it to,
which is the whole reason binding exists.
*/
func TestAnInvitationBoundToSomebodyDoesNotTravel(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "wageni", "private")

	issued := issueInvitation(t, handler, owner, roomID,
		map[string]any{"boundTo": "amina@example.com"})

	// The person it was meant for.
	intended := registerWithEmail(t, handler, "amina", "amina@example.com")
	if code := redeem(t, handler, intended, map[string]any{"token": issued.Token}); code != http.StatusOK {
		t.Errorf("the person it was issued to was refused with %d", code)
	}

	// And somebody they might forward it to.
	forwarded := registerWithEmail(t, handler, "juma", "juma@example.com")
	if code := redeem(t, handler, forwarded, map[string]any{"token": issued.Token}); code == http.StatusOK {
		t.Error("a bound invitation worked for somebody it was not issued to")
	}
}

// TestAcceptingTwiceIsNotARefusal covers somebody clicking a link again.
//
// They are already in, which is what they wanted, and they held a working
// invitation to learn it — so this is not a disclosure and not an error.
func TestAcceptingTwiceIsNotARefusal(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "wageni", "private")

	issued := issueInvitation(t, handler, owner, roomID, map[string]any{"maxUses": 1})
	person := registerAndSignIn(t, handler, "amina")

	if code := redeem(t, handler, person, map[string]any{"token": issued.Token}); code != http.StatusOK {
		t.Fatalf("the first acceptance returned %d", code)
	}
	if code := redeem(t, handler, person, map[string]any{"token": issued.Token}); code != http.StatusOK {
		t.Errorf("accepting again returned %d; they are already in", code)
	}
}

// TestTheSecretIsNeverShownAgain is what makes hashed storage meaningful.
//
// An invitation listed with its token would mean anybody who can list them can
// use them, which is the whole thing hashing is meant to prevent.
func TestTheSecretIsNeverShownAgain(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "wageni", "private")

	issued := issueInvitation(t, handler, owner, roomID, map[string]any{"withCode": true})
	if issued.Token == "" || issued.Code == "" {
		t.Fatal("issuing did not return the secrets it was asked for")
	}

	listed := call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/invitations", nil, owner)
	body := listed.Body.String()

	if contains(body, issued.Token) {
		t.Error("listing invitations returned a usable token")
	}
	if contains(body, issued.Code) {
		t.Error("listing invitations returned a usable code")
	}
}

// contains is a small helper so the assertions above read plainly.
func contains(haystack, needle string) bool {
	return needle != "" && len(haystack) >= len(needle) &&
		func() bool {
			for i := 0; i+len(needle) <= len(haystack); i++ {
				if haystack[i:i+len(needle)] == needle {
					return true
				}
			}
			return false
		}()
}

// registerWithEmail creates an account carrying an address, for the binding
// tests.
func registerWithEmail(t *testing.T, handler http.Handler, username, email string) string {
	t.Helper()

	recorder := call(t, handler, http.MethodPost, "/api/auth/register", map[string]string{
		"username": username, "email": email, "password": testPassword,
	}, "")
	if recorder.Code != http.StatusCreated {
		t.Fatalf("register %q: %d %s", username, recorder.Code, truncate(recorder.Body.String()))
	}
	return signIn(t, handler, username, testPassword)
}
