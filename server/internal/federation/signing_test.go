package federation

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AnoRebel/tdc/server/internal/events"
)

func signingKeyring(t *testing.T, id string) *events.Keyring {
	t.Helper()
	key, err := events.NewKeyPair(id)
	if err != nil {
		t.Fatalf("NewKeyPair: %v", err)
	}
	return events.NewKeyring(key)
}

// verifying builds the verify-only keyring a peer would hold, from what the
// signer publishes — the real path, not a shortcut that shares the private key.
func verifying(t *testing.T, signer *events.Keyring) *events.Keyring {
	t.Helper()
	ring, err := events.KeyringFromPublished(signer.Published())
	if err != nil {
		t.Fatalf("KeyringFromPublished: %v", err)
	}
	return ring
}

func signedRequest(t *testing.T, keyring *events.Keyring, method, url string, body []byte, now time.Time) *http.Request {
	t.Helper()
	request, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if err := Sign(request, body, keyring, now); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	return request
}

// received turns a signed outgoing request into what the recipient sees: a
// server-side request carrying the same headers. Signing an outgoing request
// and verifying that same object would skip the transit the signature exists
// to protect.
func received(signed *http.Request, body []byte) *http.Request {
	incoming := httptest.NewRequest(signed.Method, signed.URL.String(), bytes.NewReader(body))
	incoming.Host = signed.Host
	for name, values := range signed.Header {
		for _, value := range values {
			incoming.Header.Add(name, value)
		}
	}
	return incoming
}

func TestASignedRequestVerifies(t *testing.T) {
	signer := signingKeyring(t, "key-1")
	now := time.Now()
	body := []byte(`{"hello":"peer"}`)

	signed := signedRequest(t, signer, http.MethodPost, "https://b.example.org/api/federation/ping", body, now)
	incoming := received(signed, body)

	signature, err := Verify(incoming, body, verifying(t, signer), "b.example.org", now)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if signature.KeyID != "key-1" {
		t.Errorf("KeyID = %q, want key-1", signature.KeyID)
	}
	if !signature.Expires.After(signature.Created) {
		t.Error("expires must be after created")
	}
}

/*
Altering any covered field breaks the signature.

Each case is a separate attack the covered component list exists to refuse, so
each is asserted separately rather than as one "tampering is caught".
*/
func TestAlteringACoveredFieldBreaksTheSignature(t *testing.T) {
	signer := signingKeyring(t, "key-1")
	verifier := verifying(t, signer)
	now := time.Now()
	body := []byte(`{"hello":"peer"}`)

	cases := map[string]func(*http.Request) ([]byte, string){
		"the method": func(r *http.Request) ([]byte, string) {
			r.Method = http.MethodDelete
			return body, "b.example.org"
		},
		"the path": func(r *http.Request) ([]byte, string) {
			r.URL.Path = "/api/federation/something-else"
			return body, "b.example.org"
		},
		"the destination": func(r *http.Request) ([]byte, string) {
			return body, "c.example.org"
		},
		"the body": func(r *http.Request) ([]byte, string) {
			return []byte(`{"hello":"somebody else"}`), "b.example.org"
		},
	}

	for name, tamper := range cases {
		t.Run(name, func(t *testing.T) {
			signed := signedRequest(t, signer, http.MethodPost,
				"https://b.example.org/api/federation/ping", body, now)
			incoming := received(signed, body)

			deliveredBody, authority := tamper(incoming)

			if _, err := Verify(incoming, deliveredBody, verifier, authority, now); err == nil {
				t.Fatalf("altering %s was accepted", name)
			}
		})
	}
}

/*
A request signed for one server is refused by another.

The destination is covered by the signature, so this is not a comparison the
recipient could forget to make — changing it breaks the cryptography. The test
asserts the refusal names the right reason, because "signature invalid" would
send an operator looking for a tampering problem they do not have.
*/
func TestARequestSignedForAnotherServerIsRefused(t *testing.T) {
	signer := signingKeyring(t, "key-1")
	now := time.Now()
	body := []byte(`{}`)

	// Signed for server B.
	signed := signedRequest(t, signer, http.MethodPost, "https://b.example.org/api/federation/ping", body, now)
	incoming := received(signed, body)

	// Presented to server C, which knows perfectly well what it is called.
	// The delivered Host is C's, as it would be in a real redirection — the
	// signature is the only thing that still names B.
	incoming.Host = "c.example.org"

	_, err := Verify(incoming, body, verifying(t, signer), "c.example.org", now)
	if err == nil {
		t.Fatal("a request signed for another server was accepted")
	}
}

/*
An expired signature is refused — and the arithmetic is in absolute instants,
so it does not matter what zone either server keeps.

This is the failure mode that bites in production: a window that looks correct
on a developer's machine and is hours wrong on a server in another zone. The
test signs in one zone and verifies in another, with clocks that agree about
the instant, and requires the answer to be the same.
*/
func TestExpiryIsAbsoluteAcrossTimezones(t *testing.T) {
	signer := signingKeyring(t, "key-1")
	verifier := verifying(t, signer)
	body := []byte(`{}`)

	nairobi, err := time.LoadLocation("Africa/Nairobi") // +0300
	if err != nil {
		t.Skipf("no zone database: %v", err)
	}
	honolulu, err := time.LoadLocation("Pacific/Honolulu") // -1000
	if err != nil {
		t.Skipf("no zone database: %v", err)
	}

	// One instant, named in two zones thirteen hours apart.
	instant := time.Now()
	signingClock := instant.In(nairobi)
	verifyingClock := instant.In(honolulu)

	signed := signedRequest(t, signer, http.MethodPost,
		"https://b.example.org/api/federation/ping", body, signingClock)

	// Fresh: the same instant, read from a wildly different wall clock.
	if _, err := Verify(received(signed, body), body, verifier, "b.example.org", verifyingClock); err != nil {
		t.Fatalf("a signature made at the same instant was refused: %v", err)
	}

	// Expired: one second past the window, again in the far-away zone.
	past := instant.Add(SignatureLifetime + time.Second).In(honolulu)
	_, err = Verify(received(signed, body), body, verifier, "b.example.org", past)
	if !errors.Is(err, ErrSignatureExpired) {
		t.Fatalf("Verify after the window = %v, want ErrSignatureExpired", err)
	}

	// And still valid a moment before it lapses, so the boundary is not simply
	// always-expired — which would make the assertion above meaningless.
	justInside := instant.Add(SignatureLifetime - time.Second).In(honolulu)
	if _, err := Verify(received(signed, body), body, verifier, "b.example.org", justInside); err != nil {
		t.Fatalf("a signature one second inside its window was refused: %v", err)
	}
}

/*
A peer whose clock runs slightly fast is tolerated; one wildly ahead is not.

Zero tolerance would mean a server a second out of step has every request
refused, presenting as a signature fault rather than the clock fault it is.
*/
func TestAClockSlightlyAheadIsToleratedAndWildlyAheadIsNot(t *testing.T) {
	signer := signingKeyring(t, "key-1")
	verifier := verifying(t, signer)
	body := []byte(`{}`)
	now := time.Now()

	// The sender's clock is two seconds fast, inside tolerance.
	slightlyFast := now.Add(2 * time.Second)
	signed := signedRequest(t, signer, http.MethodPost,
		"https://b.example.org/api/federation/ping", body, slightlyFast)
	if _, err := Verify(received(signed, body), body, verifier, "b.example.org", now); err != nil {
		t.Fatalf("a peer two seconds fast was refused: %v", err)
	}

	// An hour ahead is not drift; it is a signature from the future.
	wild := now.Add(time.Hour)
	signed = signedRequest(t, signer, http.MethodPost,
		"https://b.example.org/api/federation/ping", body, wild)
	_, err := Verify(received(signed, body), body, verifier, "b.example.org", now)
	if !errors.Is(err, ErrSignatureFromTheFuture) {
		t.Fatalf("Verify = %v, want ErrSignatureFromTheFuture", err)
	}
	if !strings.Contains(err.Error(), "clocks") {
		t.Errorf("the refusal should point at the clocks, got %q", err)
	}
}

// An unsigned request is refused before anything else happens.
func TestAnUnsignedRequestIsRefused(t *testing.T) {
	incoming := httptest.NewRequest(http.MethodPost, "https://b.example.org/api/federation/ping", nil)

	_, err := Verify(incoming, nil, signingKeyring(t, "key-1"), "b.example.org", time.Now())
	if !errors.Is(err, ErrNotSigned) {
		t.Fatalf("Verify = %v, want ErrNotSigned", err)
	}
}

/*
A signature covering less than we require is refused, even though it verifies.

An attacker who can get a signature over only @method holds a signature that is
cryptographically perfect and means nothing. What is covered is our decision,
not the sender's.
*/
func TestASignatureCoveringTooLittleIsRefused(t *testing.T) {
	signer := signingKeyring(t, "key-1")
	now := time.Now()
	body := []byte(`{}`)

	signed := signedRequest(t, signer, http.MethodPost, "https://b.example.org/api/federation/ping", body, now)
	incoming := received(signed, body)

	// Claim to cover only the method, leaving everything else unprotected.
	input := incoming.Header.Get("Signature-Input")
	_, params, _ := strings.Cut(input, ")")
	incoming.Header.Set("Signature-Input", signatureLabel+`=("@method")`+params)

	_, err := Verify(incoming, body, verifying(t, signer), "b.example.org", now)
	if !errors.Is(err, ErrSignatureMalformed) {
		t.Fatalf("Verify = %v, want ErrSignatureMalformed", err)
	}
	for _, required := range []string{"@path", "@authority", "content-digest"} {
		if !strings.Contains(err.Error(), required) {
			t.Errorf("the refusal should name the missing %s, got %q", required, err)
		}
	}
}

// A key the verifier does not hold is refused rather than accepted by default.
func TestAnUnknownKeyIsRefused(t *testing.T) {
	signer := signingKeyring(t, "key-1")
	stranger := signingKeyring(t, "key-2")
	now := time.Now()
	body := []byte(`{}`)

	signed := signedRequest(t, signer, http.MethodPost, "https://b.example.org/api/federation/ping", body, now)

	_, err := Verify(received(signed, body), body, verifying(t, stranger), "b.example.org", now)
	if !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("Verify = %v, want ErrSignatureInvalid", err)
	}
}

// The digest must match the body actually delivered, not merely be present.
func TestASwappedBodyIsRefused(t *testing.T) {
	signer := signingKeyring(t, "key-1")
	now := time.Now()
	body := []byte(`{"amount":1}`)

	signed := signedRequest(t, signer, http.MethodPost, "https://b.example.org/api/federation/ping", body, now)

	swapped := []byte(`{"amount":1000000}`)
	_, err := Verify(received(signed, swapped), swapped, verifying(t, signer), "b.example.org", now)
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("Verify = %v, want ErrDigestMismatch", err)
	}
}

/*
Signing and verifying agree on a server behind a non-default port.

Every other signing test here uses default HTTPS, where the authority and the
bare host happen to be identical — so a verifier that stripped the port passed
them all and failed against any real deployment on :8443. This is that case.
*/
func TestSigningAgreesOnANonDefaultPort(t *testing.T) {
	signer := signingKeyring(t, "key-1")
	now := time.Now()
	body := []byte(`{}`)

	signed := signedRequest(t, signer, http.MethodPost,
		"https://b.example.org:8443/api/federation/ping", body, now)

	authority, err := Authority("https://b.example.org:8443")
	if err != nil {
		t.Fatalf("Authority: %v", err)
	}
	if _, err := Verify(received(signed, body), body, verifying(t, signer), authority, now); err != nil {
		t.Fatalf("a request to a server on :8443 was refused: %v", err)
	}

	// And the port is genuinely load-bearing: the same server named without it
	// is a different destination as far as the signature is concerned.
	bare, err := Authority("https://b.example.org")
	if err != nil {
		t.Fatalf("Authority: %v", err)
	}
	if _, err := Verify(received(signed, body), body, verifying(t, signer), bare, now); err == nil {
		t.Fatal("a signature for :8443 verified against the default port")
	}
}

/*
Two identical requests in the same second are both accepted.

A GET with no body has nothing varying in it: same method, path, authority and
empty digest. With only a timestamp to distinguish them, two such requests a
millisecond apart produce byte-identical signatures, and the receiver's replay
cache correctly refuses the second — refusing a server that did nothing wrong.

Found by a demonstration where browsing a peer twice failed the second time,
reported as "replayed signature". The nonce is what separates them.
*/
func TestTwoIdenticalRequestsInTheSameSecondAreBothAccepted(t *testing.T) {
	signer := signingKeyring(t, "key-1")
	verifier := verifying(t, signer)
	cache := NewReplayCache()
	now := time.Now()

	accepted := 0
	for attempt := range 5 {
		// Identical in every respect, signed at the very same instant.
		signed := signedRequest(t, signer, http.MethodGet,
			"https://b.example.org/api/federation/rooms", nil, now)

		signature, err := Verify(received(signed, nil), nil, verifier, "b.example.org", now)
		if err != nil {
			t.Fatalf("request %d was refused: %v", attempt, err)
		}
		if !cache.Remember(signature.Raw, signature.Expires) {
			t.Fatalf("request %d was taken for a replay of request %d", attempt, attempt-1)
		}
		accepted++
	}

	if accepted != 5 {
		t.Fatalf("%d of 5 identical-but-distinct requests were accepted", accepted)
	}
}

/*
And a genuine replay — the same signature presented twice — is still refused.

The nonce distinguishes two requests; it must not make a captured one reusable,
which is what a test of the previous behaviour alone would have missed.
*/
func TestAGenuineReplayIsStillRefusedWithNonces(t *testing.T) {
	signer := signingKeyring(t, "key-1")
	verifier := verifying(t, signer)
	cache := NewReplayCache()
	now := time.Now()

	signed := signedRequest(t, signer, http.MethodGet,
		"https://b.example.org/api/federation/rooms", nil, now)

	first, err := Verify(received(signed, nil), nil, verifier, "b.example.org", now)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !cache.Remember(first.Raw, first.Expires) {
		t.Fatal("the first presentation was refused")
	}

	// The very same captured request, sent again.
	second, err := Verify(received(signed, nil), nil, verifier, "b.example.org", now)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if cache.Remember(second.Raw, second.Expires) {
		t.Fatal("a captured request was accepted twice")
	}
}
