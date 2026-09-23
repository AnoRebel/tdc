/*
Package delivery signs what this server sends to programs, and sends it.

Everything outgoing to an integration — an event a webhook subscribed to, a
press on a message a program posted — arrives at somebody else's HTTP endpoint,
which has no session with this server and no reason to believe a request
claiming to come from it. What it needs is a way to check, from the request
alone, that this server sent it, that nothing in it was changed on the way,
and that it is not an old delivery played back.

Two signatures travel on every delivery, for two kinds of receiver:

  - An Ed25519 signature in the RFC 9421 shape federation already uses, made
    with the key the event log is signed with and verifiable from the keys this
    server publishes. It proves the delivery came from this server
    specifically, with nothing shared in advance.
  - An HMAC-SHA256 over the timestamp and body, with a secret the receiver was
    given when it was set up. It is what most webhook receivers already know
    how to check, and it needs no key fetching at all.

Both cover a timestamp, which is what makes a captured delivery expire.
*/
package delivery

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/federation"
)

// The headers a delivery carries.
const (
	// HeaderDelivery names one delivery. It stays the same across retries of
	// that delivery, which is what lets a receiver notice it has already
	// handled one whose acknowledgement was lost.
	HeaderDelivery = "TDC-Delivery"
	// HeaderEvent says what kind of delivery this is, so a receiver can route
	// it before parsing the body.
	HeaderEvent = "TDC-Event"
	// HeaderHMAC carries the shared-secret signature: t=<unix seconds>,v1=<hex>.
	HeaderHMAC = "TDC-Signature-HMAC"
	// HeaderDigest is RFC 9530's digest of the body, which the Ed25519
	// signature covers.
	HeaderDigest = "Content-Digest"
	// HeaderSignatureInput and HeaderSignature are RFC 9421's.
	HeaderSignatureInput = "Signature-Input"
	HeaderSignature      = "Signature"
)

/*
ReplayWindow is how far a delivery's signed timestamp may be from the moment a
receiver checks it, in either direction.

Five minutes. Long enough that an ordinary delivery arriving over a slow
network, or checked by a receiver whose clock is a little wrong, is accepted;
short enough that a delivery somebody captured is useless to replay by the time
anybody could do anything with it. A receiver wanting to refuse a replay inside
the window remembers the TDC-Delivery identifiers it has seen for that long.

A retry is signed afresh, so a delivery retried hours later still arrives
inside its own window.
*/
const ReplayWindow = 5 * time.Minute

// signatureLabel names the signature within the RFC 9421 headers.
const signatureLabel = "tdc"

// coveredComponents are what the Ed25519 signature covers, in order.
var coveredComponents = []string{"content-digest", "tdc-delivery"}

var (
	// ErrUnsigned means a delivery carried no signature to check.
	ErrUnsigned = errors.New("the delivery is not signed")
	// ErrMalformed means the signature headers could not be read.
	ErrMalformed = errors.New("the delivery's signature headers are malformed")
	// ErrTampered means the body or a covered header is not what was signed.
	ErrTampered = errors.New("the delivery is not what was signed")
	// ErrOutsideWindow means the signed timestamp is too far from now.
	ErrOutsideWindow = errors.New("the delivery was signed outside the replay window")
	// ErrUnknownKey means the signature names a key the receiver does not hold.
	ErrUnknownKey = errors.New("the delivery was signed by a key that is not published")
)

// Signer signs deliveries with this server's keys.
type Signer struct {
	keyring *events.Keyring
	now     func() time.Time
}

// NewSigner builds a signer over the event log's keyring.
func NewSigner(keyring *events.Keyring) *Signer {
	return &Signer{keyring: keyring, now: time.Now}
}

/*
Sign returns the headers a delivery travels with.

secret is the receiver's shared secret, when it has one; empty leaves the HMAC
off and the delivery is still verifiable from the published keys. The body is
passed rather than read from a request, because the signature must be over
exactly the bytes that are then sent.
*/
func (s *Signer) Sign(id, kind string, body []byte, secret []byte) (http.Header, error) {
	key := s.keyring.Current()
	if key == nil {
		return nil, errors.New("no signing key is available")
	}

	// Unix seconds: an absolute instant, the same whichever zone the sender
	// and receiver are in.
	created := s.now().Unix()
	digest := federation.Digest(body)

	base := signatureBase(digest, id, created, key.ID)
	signature, err := key.SignBytes([]byte(base))
	if err != nil {
		return nil, fmt.Errorf("sign the delivery: %w", err)
	}

	headers := http.Header{}
	headers.Set("Content-Type", "application/json")
	headers.Set(HeaderDelivery, id)
	headers.Set(HeaderEvent, kind)
	headers.Set(HeaderDigest, digest)
	headers.Set(HeaderSignatureInput, signatureLabel+"="+signatureParams(created, key.ID))
	headers.Set(HeaderSignature, signatureLabel+"=:"+base64.StdEncoding.EncodeToString(signature)+":")
	if len(secret) > 0 {
		headers.Set(HeaderHMAC, fmt.Sprintf("t=%d,v1=%s", created, hmacOf(secret, created, body)))
	}
	return headers, nil
}

/*
signatureBase builds the exact bytes the Ed25519 signature covers.

RFC 9421 §2.5: one line per covered component, then the parameters. Not the
method, path or authority that federation covers: a webhook receiver often sits
behind a proxy that rewrites all three, and a signature that failed there would
be one nobody could use. The delivery's identifier is covered instead, so a
signature cannot be moved onto a different delivery.
*/
func signatureBase(digest, id string, created int64, keyID string) string {
	var builder strings.Builder
	builder.WriteString(`"content-digest": ` + digest + "\n")
	builder.WriteString(`"tdc-delivery": ` + id + "\n")
	builder.WriteString(`"@signature-params": ` + signatureParams(created, keyID))
	return builder.String()
}

func signatureParams(created int64, keyID string) string {
	quoted := make([]string, 0, len(coveredComponents))
	for _, component := range coveredComponents {
		quoted = append(quoted, `"`+component+`"`)
	}
	return fmt.Sprintf(`(%s);created=%d;keyid="%s";alg="ed25519"`,
		strings.Join(quoted, " "), created, keyID)
}

// hmacOf is the shared-secret signature: HMAC-SHA256 over "<created>.<body>".
func hmacOf(secret []byte, created int64, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(strconv.FormatInt(created, 10)))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

/*
Verify checks a delivery's Ed25519 signature against published keys, the way a
receiver would.

It needs nothing but the keys a server publishes and the delivery itself —
no connection to the sender and nothing from its database — which is the whole
of what makes the signature worth having. Exported so this server's tests, and
anybody reading the source to write a receiver, check exactly one scheme.

The timestamp is checked before the cryptography, so a replay costs nothing to
refuse.
*/
func Verify(headers http.Header, body []byte, keys *events.Keyring, now time.Time) error {
	input := headers.Get(HeaderSignatureInput)
	raw := headers.Get(HeaderSignature)
	if input == "" || raw == "" {
		return ErrUnsigned
	}

	created, keyID, err := parseInput(input)
	if err != nil {
		return err
	}
	if err := withinWindow(created, now); err != nil {
		return err
	}

	digest := headers.Get(HeaderDigest)
	if digest != federation.Digest(body) {
		return fmt.Errorf("%w: the body does not match its digest", ErrTampered)
	}

	signature, err := parseSignature(raw)
	if err != nil {
		return err
	}

	key, found := keys.Lookup(keyID)
	if !found {
		return fmt.Errorf("%w: %q", ErrUnknownKey, keyID)
	}

	base := signatureBase(digest, headers.Get(HeaderDelivery), created, keyID)
	if !key.VerifyBytes([]byte(base), signature) {
		return ErrTampered
	}
	return nil
}

// VerifyHMAC checks the shared-secret signature, the way a receiver would.
func VerifyHMAC(headers http.Header, body, secret []byte, now time.Time) error {
	value := headers.Get(HeaderHMAC)
	if value == "" {
		return ErrUnsigned
	}

	var created int64
	var claimed string
	for _, part := range strings.Split(value, ",") {
		name, content, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch name {
		case "t":
			parsed, err := strconv.ParseInt(content, 10, 64)
			if err != nil {
				return fmt.Errorf("%w: t is not a timestamp", ErrMalformed)
			}
			created = parsed
		case "v1":
			claimed = content
		}
	}
	if created == 0 || claimed == "" {
		return fmt.Errorf("%w: expected t= and v1=", ErrMalformed)
	}
	if err := withinWindow(created, now); err != nil {
		return err
	}

	expected := hmacOf(secret, created, body)
	// Compared in constant time, so the comparison does not reveal how much of
	// a forged signature was right.
	if !hmac.Equal([]byte(expected), []byte(claimed)) {
		return ErrTampered
	}
	return nil
}

/*
withinWindow refuses a timestamp too far from now in either direction.

Both directions, and deliberately symmetric: a timestamp from the future is as
suspicious as one from the past, and a receiver whose clock runs slow would
otherwise accept a delivery signed well after it could have been captured.
*/
func withinWindow(created int64, now time.Time) error {
	signed := time.Unix(created, 0)
	if difference := now.Sub(signed); difference > ReplayWindow || difference < -ReplayWindow {
		return fmt.Errorf("%w: signed at %s, checked at %s",
			ErrOutsideWindow, signed.UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339))
	}
	return nil
}

func parseInput(header string) (int64, string, error) {
	label, rest, found := strings.Cut(header, "=")
	if !found || strings.TrimSpace(label) != signatureLabel {
		return 0, "", fmt.Errorf("%w: expected a %q signature", ErrMalformed, signatureLabel)
	}

	open, closing := strings.Index(rest, "("), strings.Index(rest, ")")
	if open < 0 || closing < open {
		return 0, "", fmt.Errorf("%w: no covered component list", ErrMalformed)
	}

	/*
		Everything this scheme covers must be covered. Without the check a
		signature over the digest alone would verify, and could then be moved
		onto another delivery with the same body.
	*/
	covered := map[string]bool{}
	for _, component := range strings.Fields(rest[open+1 : closing]) {
		covered[strings.Trim(component, `"`)] = true
	}
	for _, required := range coveredComponents {
		if !covered[required] {
			return 0, "", fmt.Errorf("%w: the signature does not cover %s", ErrMalformed, required)
		}
	}

	var created int64
	var keyID string
	for _, parameter := range strings.Split(rest[closing+1:], ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(parameter), "=")
		if !ok {
			continue
		}
		value = strings.Trim(value, `"`)
		switch name {
		case "created":
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return 0, "", fmt.Errorf("%w: created is not an integer", ErrMalformed)
			}
			created = parsed
		case "keyid":
			keyID = value
		}
	}
	if created == 0 || keyID == "" {
		return 0, "", fmt.Errorf("%w: created and keyid are required", ErrMalformed)
	}
	return created, keyID, nil
}

func parseSignature(header string) ([]byte, error) {
	label, rest, found := strings.Cut(header, "=")
	if !found || strings.TrimSpace(label) != signatureLabel {
		return nil, fmt.Errorf("%w: expected a %q signature", ErrMalformed, signatureLabel)
	}
	signature, err := base64.StdEncoding.DecodeString(strings.Trim(strings.TrimSpace(rest), ":"))
	if err != nil {
		return nil, fmt.Errorf("%w: the signature is not base64", ErrMalformed)
	}
	return signature, nil
}
