package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

/*
The second factor over HTTP.

The service's own tests cover codes being good once. What is checked here is the
shape of the sign-in it produces: that a password alone stops short of a session,
that the ticket is what carries the sign-in forward, and that an operator's
policy reaches sessions that already existed.
*/

// secondFactorAccount registers somebody and signs them in with a password.
func secondFactorAccount(t *testing.T, handler http.Handler, username string) string {
	t.Helper()

	if recorder := call(t, handler, http.MethodPost, "/api/auth/register", map[string]string{
		"username": username, "password": testPassword,
	}, ""); recorder.Code != http.StatusCreated {
		t.Fatalf("register: status %d, body %s", recorder.Code, recorder.Body.String())
	}
	return signIn(t, handler, username, testPassword)
}

// enrolSecondFactor sets up an authenticator app and returns its secret and the
// recovery codes it produced.
func enrolSecondFactor(t *testing.T, handler http.Handler, token string) (string, []string) {
	t.Helper()

	begun := call(t, handler, http.MethodPost, "/api/me/second-factor/begin", nil, token)
	if begun.Code != http.StatusOK {
		t.Fatalf("beginning enrolment: status %d, body %s", begun.Code, begun.Body.String())
	}

	secret, _ := decode(t, begun)["secret"].(string)
	if secret == "" {
		t.Fatal("enrolment returned no secret")
	}

	confirmed := call(t, handler, http.MethodPost, "/api/me/second-factor/confirm",
		map[string]string{"code": codeFor(t, secret, time.Now())}, token)
	if confirmed.Code != http.StatusCreated {
		t.Fatalf("confirming: status %d, body %s", confirmed.Code, confirmed.Body.String())
	}

	raw, _ := decode(t, confirmed)["recoveryCodes"].([]any)
	codes := make([]string, 0, len(raw))
	for _, code := range raw {
		if text, ok := code.(string); ok {
			codes = append(codes, text)
		}
	}
	if len(codes) == 0 {
		t.Fatal("confirming enrolment produced no recovery codes")
	}

	return secret, codes
}

// codeFor is what an authenticator app would show.
func codeFor(t *testing.T, secret string, at time.Time) string {
	t.Helper()

	code, err := totp.GenerateCodeCustom(secret, at, totp.ValidateOpts{
		Period: 30, Skew: 1, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatalf("generating a code: %v", err)
	}
	return code
}

/*
TestAPasswordAloneDoesNotFinishASignIn.

The whole shape of it: the password is right, and what comes back is a ticket
rather than a session. Anything else would mean a session existed before the
second factor was proved.
*/
func TestAPasswordAloneDoesNotFinishASignIn(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)

	token := secondFactorAccount(t, handler, "amina")
	secret, _ := enrolSecondFactor(t, handler, token)

	recorder := call(t, handler, http.MethodPost, "/api/auth/signin", map[string]string{
		"username": "amina", "password": testPassword,
	}, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("signing in: status %d, body %s", recorder.Code, recorder.Body.String())
	}

	result := decode(t, recorder)
	if required, _ := result["secondFactorRequired"].(bool); !required {
		t.Fatal("a password alone finished the sign-in")
	}
	if _, present := result["token"]; present {
		t.Error("a session was issued before the second factor was proved")
	}

	ticket, _ := result["ticket"].(string)
	if ticket == "" {
		t.Fatal("no ticket was issued to carry the sign-in forward")
	}

	/*
		The code finishes it — from the next window.

		Confirming enrolment spends the period its code came from, so a code
		from that same thirty seconds is refused as a replay. That is the rule
		working; in practice nobody enrols and signs in inside one window, and
		here it has to be said explicitly.
	*/
	finished := call(t, handler, http.MethodPost, "/api/auth/second-factor", map[string]string{
		"ticket": ticket, "code": codeFor(t, secret, time.Now().Add(30*time.Second)),
	}, "")
	if finished.Code != http.StatusOK {
		t.Fatalf("completing: status %d, body %s", finished.Code, finished.Body.String())
	}
	if session, _ := decode(t, finished)["token"].(string); session == "" {
		t.Error("completing the second factor issued no session")
	}
}

/*
TestAWrongCodeSaysNothingAboutThePassword.

The refusal at this step must not be a place to learn that the password was
right — which it would be if a wrong code said something different from a wrong
password. The caller already got here, so what is protected is what a stolen
ticket discloses.
*/
func TestAWrongCodeSaysNothingAboutThePassword(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)

	token := secondFactorAccount(t, handler, "amina")
	enrolSecondFactor(t, handler, token)

	begun := decode(t, call(t, handler, http.MethodPost, "/api/auth/signin", map[string]string{
		"username": "amina", "password": testPassword,
	}, ""))
	ticket, _ := begun["ticket"].(string)

	recorder := call(t, handler, http.MethodPost, "/api/auth/second-factor", map[string]string{
		"ticket": ticket, "code": "000000",
	}, "")
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("a wrong code: status %d, want %d", recorder.Code, http.StatusUnauthorized)
	}

	message, _ := decode(t, recorder)["message"].(string)
	for _, leaked := range []string{"password", "username", "amina"} {
		if mentions(message, leaked) {
			t.Errorf("the refusal mentions %q: %s", leaked, message)
		}
	}
}

// TestARecoveryCodeFinishesASignIn, which is the day the phone is lost.
func TestARecoveryCodeFinishesASignIn(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)

	token := secondFactorAccount(t, handler, "amina")
	_, codes := enrolSecondFactor(t, handler, token)

	begun := decode(t, call(t, handler, http.MethodPost, "/api/auth/signin", map[string]string{
		"username": "amina", "password": testPassword,
	}, ""))
	ticket, _ := begun["ticket"].(string)

	recorder := call(t, handler, http.MethodPost, "/api/auth/second-factor", map[string]string{
		"ticket": ticket, "code": codes[0],
	}, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("using a recovery code: status %d, body %s", recorder.Code, recorder.Body.String())
	}

	result := decode(t, recorder)
	if used, _ := result["usedRecoveryCode"].(bool); !used {
		t.Error("using a recovery code was not reported as such")
	}
	// Somebody should be told before they need the next one, not after.
	if left, _ := result["recoveryCodesLeft"].(float64); left != 9 {
		t.Errorf("%v codes left, want 9", result["recoveryCodesLeft"])
	}
}

/*
TestTurningItOffNeedsThePassword.

A session is not enough to remove a protection — an unlocked laptop is a
session, and the point of the factor is that holding one thing is not sufficient.
*/
func TestTurningItOffNeedsThePassword(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)

	token := secondFactorAccount(t, handler, "amina")
	enrolSecondFactor(t, handler, token)

	if recorder := call(t, handler, http.MethodPost, "/api/me/second-factor/disable",
		map[string]string{"password": "not the password"}, token); recorder.Code != http.StatusUnauthorized {
		t.Errorf("removing with the wrong password: status %d, want %d",
			recorder.Code, http.StatusUnauthorized)
	}

	// Still there.
	status := decode(t, call(t, handler, http.MethodGet, "/api/me/second-factor", nil, token))
	if enrolled, _ := status["enrolled"].(bool); !enrolled {
		t.Error("the refused removal took it off anyway")
	}

	if recorder := call(t, handler, http.MethodPost, "/api/me/second-factor/disable",
		map[string]string{"password": testPassword}, token); recorder.Code != http.StatusNoContent {
		t.Fatalf("removing with the right password: status %d, body %s",
			recorder.Code, recorder.Body.String())
	}

	after := decode(t, call(t, handler, http.MethodGet, "/api/me/second-factor", nil, token))
	if enrolled, _ := after["enrolled"].(bool); enrolled {
		t.Error("it survived being removed")
	}
	if left, _ := after["recoveryCodesLeft"].(float64); left != 0 {
		t.Errorf("%v recovery codes outlived the factor they recovered", after["recoveryCodesLeft"])
	}
}

/*
TestAnOperatorRequirementReachesExistingSessions.

The part that needs middleware rather than a check at sign-in. A policy that
only applied to new sign-ins would leave every session opened before it was set
unaffected, for as long as those sessions last — which on a thirty-day lifetime
is a month of the policy meaning nothing.
*/
func TestAnOperatorRequirementReachesExistingSessions(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)

	// Somebody signed in before the policy exists, with no second factor.
	member := secondFactorAccount(t, handler, "amina")

	if recorder := call(t, handler, http.MethodGet, "/api/rooms", nil, member); recorder.Code != http.StatusOK {
		t.Fatalf("before the policy: status %d", recorder.Code)
	}

	if recorder := call(t, handler, http.MethodPut,
		"/api/admin/settings/auth.second_factor.policy",
		map[string]string{"value": "everybody"},
		admin); recorder.Code != http.StatusNoContent {
		t.Fatalf("setting the policy: status %d, body %s", recorder.Code, recorder.Body.String())
	}

	// The same session, now held short of the server.
	recorder := call(t, handler, http.MethodGet, "/api/rooms", nil, member)
	if recorder.Code != http.StatusForbidden {
		t.Errorf("after the policy: status %d, want %d", recorder.Code, http.StatusForbidden)
	}

	// But still able to do the thing that fixes it.
	for _, reachable := range []string{"/api/me", "/api/me/second-factor"} {
		if recorder := call(t, handler, http.MethodGet, reachable, nil, member); recorder.Code != http.StatusOK {
			t.Errorf("%s is unreachable: status %d — somebody cannot comply", reachable, recorder.Code)
		}
	}

	// And enrolling releases them.
	enrolSecondFactor(t, handler, member)
	if recorder := call(t, handler, http.MethodGet, "/api/rooms", nil, member); recorder.Code != http.StatusOK {
		t.Errorf("after enrolling: status %d, want %d", recorder.Code, http.StatusOK)
	}
}

/*
TestARequiredFactorCannotBeTurnedOff.

Otherwise the policy is a suggestion: anybody it applies to could remove the
factor and carry on, which is not what an operator setting it meant.
*/
func TestARequiredFactorCannotBeTurnedOff(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)

	member := secondFactorAccount(t, handler, "amina")
	enrolSecondFactor(t, handler, member)

	if recorder := call(t, handler, http.MethodPut,
		"/api/admin/settings/auth.second_factor.policy",
		map[string]string{"value": "everybody"},
		admin); recorder.Code != http.StatusNoContent {
		t.Fatalf("setting the policy: status %d, body %s", recorder.Code, recorder.Body.String())
	}

	if recorder := call(t, handler, http.MethodPost, "/api/me/second-factor/disable",
		map[string]string{"password": testPassword}, member); recorder.Code != http.StatusConflict {
		t.Errorf("removing a required factor: status %d, want %d", recorder.Code, http.StatusConflict)
	}
}

// TestNothingIsRequiredByDefault: the ordinary server, where a second factor is
// somebody's own choice.
func TestNothingIsRequiredByDefault(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)

	token := secondFactorAccount(t, handler, "amina")

	status := decode(t, call(t, handler, http.MethodGet, "/api/me/second-factor", nil, token))
	if enrolled, _ := status["enrolled"].(bool); enrolled {
		t.Error("a second factor is set up by default")
	}
	if required, _ := status["required"].(bool); required {
		t.Error("a second factor is required by default")
	}

	// And signing in is unaffected.
	recorder := decode(t, call(t, handler, http.MethodPost, "/api/auth/signin", map[string]string{
		"username": "amina", "password": testPassword,
	}, ""))
	if _, present := recorder["secondFactorRequired"]; present {
		t.Error("a sign-in with no second factor still mentions one")
	}
}

// mentions is a case-insensitive substring check, for asserting that a refusal
// does not name something it should not.
func mentions(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

/*
TestAPasskeySignInIsNotAskedForACode.

A passkey already proves possession of the device, and where the authenticator
checks a fingerprint or a PIN it proves knowledge too. Asking for a code as well
would be ceremony rather than security — and would mean somebody who chose the
stronger way in gets the more tedious sign-in.
*/
func TestAPasskeySignInIsNotAskedForACode(t *testing.T) {
	handler := withPasskeys(t)
	completeSetup(t, handler)

	token := secondFactorAccount(t, handler, "amina")
	enrolSecondFactor(t, handler, token)

	// The same account now has both: a second factor, and a passkey.
	authenticator, credential := enrolPasskey(t, handler, token, "Phone")
	handle := webauthnHandle(t, handler, token)

	recorder := signInWithPasskey(t, handler, authenticator, credential, handle)
	if recorder.Code != http.StatusOK {
		t.Fatalf("signing in with a passkey: status %d, body %s", recorder.Code, recorder.Body.String())
	}

	result := decode(t, recorder)
	if _, asked := result["secondFactorRequired"]; asked {
		t.Error("a passkey sign-in was asked for a second factor as well")
	}
	if session, _ := result["token"].(string); session == "" {
		t.Error("a passkey sign-in did not produce a session")
	}
}
