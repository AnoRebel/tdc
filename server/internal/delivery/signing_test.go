package delivery_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/AnoRebel/tdc/server/internal/delivery"
	"github.com/AnoRebel/tdc/server/internal/events"
)

// signed makes a delivery with a fresh key, returning it with the key's
// published form — which is all a receiver ever holds.
func signed(t *testing.T, body []byte, secret []byte) (http.Header, []events.PublishedKey) {
	t.Helper()

	key, err := events.NewKeyPair("delivery-test")
	if err != nil {
		t.Fatal(err)
	}
	ring := events.NewKeyring(key)

	headers, err := delivery.NewSigner(ring).Sign("0199f1f2-delivery", "message.sent", body, secret)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	return headers, ring.Published()
}

/*
A delivery verifies from the published keys and its own content, and nothing
else.

The keys go through JSON on the way, as they would when a receiver fetches them
from another machine: nothing of the signer — no keyring, no database, no
private half — is in reach of the verification. That independence is the
property the signature exists to provide.
*/
func TestADeliveryVerifiesFromPublishedKeysAlone(t *testing.T) {
	body := []byte(`{"version":1,"type":"message.sent","event":{"content":{"body":"Habari"}}}`)
	headers, published := signed(t, body, nil)

	wire, err := json.Marshal(map[string]any{"keys": published})
	if err != nil {
		t.Fatal(err)
	}
	var fetched struct {
		Keys []events.PublishedKey `json:"keys"`
	}
	if err := json.Unmarshal(wire, &fetched); err != nil {
		t.Fatal(err)
	}
	receiver, err := events.KeyringFromPublished(fetched.Keys)
	if err != nil {
		t.Fatalf("building a keyring from published keys: %v", err)
	}

	if err := delivery.Verify(headers, body, receiver, time.Now()); err != nil {
		t.Fatalf("an unaltered delivery did not verify: %v", err)
	}
}

// One altered byte of the body fails verification.
func TestAnAlteredPayloadFailsVerification(t *testing.T) {
	body := []byte(`{"approved":false}`)
	headers, published := signed(t, body, nil)
	receiver, _ := events.KeyringFromPublished(published)

	// One byte: "false" becomes "talse".
	altered := append([]byte(nil), body...)
	altered[12] = 't'

	if err := delivery.Verify(headers, altered, receiver, time.Now()); !errors.Is(err, delivery.ErrTampered) {
		t.Fatalf("an altered body returned %v, want ErrTampered", err)
	}

	/*
		And a body altered together with its digest — which an attacker who
		can change one can change the other — still fails, because the digest
		is inside what was signed.
	*/
	forged := headers.Clone()
	forged.Set(delivery.HeaderDigest, "sha-256=:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=:")
	if err := delivery.Verify(forged, altered, receiver, time.Now()); !errors.Is(err, delivery.ErrTampered) {
		t.Fatalf("an altered body with a matching forged digest returned %v", err)
	}
}

// A signature cannot be moved onto a different delivery.
func TestASignatureDoesNotTransferToAnotherDelivery(t *testing.T) {
	body := []byte(`{"n":1}`)
	headers, published := signed(t, body, nil)
	receiver, _ := events.KeyringFromPublished(published)

	moved := headers.Clone()
	moved.Set(delivery.HeaderDelivery, "some-other-delivery")
	if err := delivery.Verify(moved, body, receiver, time.Now()); !errors.Is(err, delivery.ErrTampered) {
		t.Fatalf("a signature moved to another delivery returned %v", err)
	}
}

/*
A delivery replayed outside the window is detectably invalid, from either
side of it — and one just inside is accepted, so the window is where the
documentation says it is.
*/
func TestAReplayOutsideTheWindowIsDetected(t *testing.T) {
	body := []byte(`{"n":1}`)
	headers, published := signed(t, body, []byte("shared"))
	receiver, _ := events.KeyringFromPublished(published)

	now := time.Now()
	for name, at := range map[string]time.Time{
		"just inside, later":   now.Add(delivery.ReplayWindow - 2*time.Second),
		"just inside, earlier": now.Add(-delivery.ReplayWindow + 2*time.Second),
	} {
		if err := delivery.Verify(headers, body, receiver, at); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if err := delivery.VerifyHMAC(headers, body, []byte("shared"), at); err != nil {
			t.Errorf("%s, by HMAC: %v", name, err)
		}
	}

	for name, at := range map[string]time.Time{
		"replayed later":           now.Add(delivery.ReplayWindow + 2*time.Second),
		"signed in the far future": now.Add(-delivery.ReplayWindow - 2*time.Second),
	} {
		if err := delivery.Verify(headers, body, receiver, at); !errors.Is(err, delivery.ErrOutsideWindow) {
			t.Errorf("%s: got %v, want ErrOutsideWindow", name, err)
		}
		if err := delivery.VerifyHMAC(headers, body, []byte("shared"), at); !errors.Is(err, delivery.ErrOutsideWindow) {
			t.Errorf("%s, by HMAC: got %v, want ErrOutsideWindow", name, err)
		}
	}
}

/*
The timestamp is an absolute instant, so a delivery signed on a machine in
Nairobi verifies on one in Honolulu at the same moment. A timestamp rebuilt
from wall-clock fields would be off by the difference between them.
*/
func TestTheTimestampIsAbsoluteAcrossTimezones(t *testing.T) {
	nairobi, err := time.LoadLocation("Africa/Nairobi")
	if err != nil {
		t.Skip("no timezone database")
	}
	honolulu, err := time.LoadLocation("Pacific/Honolulu")
	if err != nil {
		t.Skip("no timezone database")
	}

	body := []byte(`{"n":1}`)
	headers, published := signed(t, body, []byte("shared"))
	receiver, _ := events.KeyringFromPublished(published)

	instant := time.Now()
	if err := delivery.Verify(headers, body, receiver, instant.In(honolulu)); err != nil {
		t.Errorf("verifying in Honolulu: %v", err)
	}
	if err := delivery.VerifyHMAC(headers, body, []byte("shared"), instant.In(nairobi)); err != nil {
		t.Errorf("verifying the HMAC in Nairobi: %v", err)
	}
}

// A key the receiver does not hold is named as unknown rather than as a forgery.
func TestAnUnpublishedKeyIsReportedAsUnknown(t *testing.T) {
	body := []byte(`{"n":1}`)
	headers, _ := signed(t, body, nil)
	_, otherKeys := signed(t, body, nil)
	other, _ := events.KeyringFromPublished(otherKeys)

	// Both test keys share an identifier, so one is renamed to be absent.
	headers.Set(delivery.HeaderSignatureInput,
		`tdc=("content-digest" "tdc-delivery");created=`+createdOf(t, headers)+`;keyid="rotated-away";alg="ed25519"`)
	if err := delivery.Verify(headers, body, other, time.Now()); !errors.Is(err, delivery.ErrUnknownKey) {
		t.Fatalf("an unknown key returned %v", err)
	}
}

// A signature that leaves out a component this scheme requires is refused.
func TestASignatureMustCoverTheDelivery(t *testing.T) {
	body := []byte(`{"n":1}`)
	headers, published := signed(t, body, nil)
	receiver, _ := events.KeyringFromPublished(published)

	headers.Set(delivery.HeaderSignatureInput,
		`tdc=("content-digest");created=`+createdOf(t, headers)+`;keyid="delivery-test";alg="ed25519"`)
	if err := delivery.Verify(headers, body, receiver, time.Now()); !errors.Is(err, delivery.ErrMalformed) {
		t.Fatalf("a signature not covering the delivery returned %v", err)
	}
}

// The shared-secret signature fails for the wrong secret and for a changed body.
func TestTheHMACNeedsTheSecretAndTheBody(t *testing.T) {
	body := []byte(`{"n":1}`)
	headers, _ := signed(t, body, []byte("the-right-secret"))

	if err := delivery.VerifyHMAC(headers, body, []byte("the-right-secret"), time.Now()); err != nil {
		t.Fatalf("the right secret did not verify: %v", err)
	}
	if err := delivery.VerifyHMAC(headers, body, []byte("a-guess"), time.Now()); !errors.Is(err, delivery.ErrTampered) {
		t.Errorf("the wrong secret returned %v", err)
	}
	if err := delivery.VerifyHMAC(headers, []byte(`{"n":2}`), []byte("the-right-secret"), time.Now()); !errors.Is(err, delivery.ErrTampered) {
		t.Errorf("a changed body returned %v", err)
	}
}

// Without a secret there is no HMAC header, rather than one over an empty key.
func TestNoSecretMeansNoHMAC(t *testing.T) {
	headers, _ := signed(t, []byte(`{}`), nil)
	if headers.Get(delivery.HeaderHMAC) != "" {
		t.Error("a delivery with no secret carries an HMAC anyway")
	}
}

func createdOf(t *testing.T, headers http.Header) string {
	t.Helper()
	input := headers.Get(delivery.HeaderSignatureInput)
	const marker = "created="
	start := len(input)
	for i := 0; i+len(marker) <= len(input); i++ {
		if input[i:i+len(marker)] == marker {
			start = i + len(marker)
			break
		}
	}
	end := start
	for end < len(input) && input[end] >= '0' && input[end] <= '9' {
		end++
	}
	return input[start:end]
}
