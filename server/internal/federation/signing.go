package federation

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/AnoRebel/tdc/server/internal/events"
)

/*
Signing a federated request, per RFC 9421.

An event's own signature says "this content is authentic, whoever carries it."
A request signature says something different and equally necessary: "this HTTP
call really came from the server it names, right now, and was meant for you."
Neither substitutes for the other. A stolen request could carry a perfectly
valid event signature while lying about who is sending it and where it was
meant to go, and that is exactly what the covered components below refuse.

Four things are covered, and each refuses a specific attack:

  - @method and @path — the request cannot be turned into a different one.
  - @authority — it cannot be replayed against a different server, because
    changing the destination breaks the signature rather than merely looking
    odd.
  - content-digest — the body cannot be altered.

Plus created/expires, which bound how long a captured request stays useful.
*/

const (
	// signatureLabel names our signature within the header. One signature per
	// request, so the label is a constant rather than something to negotiate.
	signatureLabel = "tdc"

	/*
		SignatureLifetime is how long a signed request stays valid.

		Thirty seconds. Long enough to survive an ordinary round trip and a
		little clock drift between two servers nobody synchronised; short
		enough that a captured request is worthless almost immediately. It is
		the window the replay cache has to remember, so making it generous has
		a cost measured in memory as well as in risk.
	*/
	SignatureLifetime = 30 * time.Second

	/*
		ClockSkewTolerance is how far ahead of us a peer's clock may be.

		RFC 9421 deliberately defines no tolerance and leaves it to the
		application, so this is a decision rather than an inherited default.
		Zero would mean a peer whose clock is a second fast has every request
		refused as being from the future — a failure that looks like a
		signature problem and is really an NTP problem, and one that would be
		miserable to diagnose from the far side.

		Five seconds is small enough that it does not meaningfully widen the
		replay window and large enough to absorb the drift between two machines
		that are each roughly correct.
	*/
	ClockSkewTolerance = 5 * time.Second
)

var (
	// ErrNotSigned means a request carried no signature at all.
	ErrNotSigned = errors.New("request is not signed")
	// ErrSignatureMalformed means the signature headers could not be read.
	ErrSignatureMalformed = errors.New("signature headers are malformed")
	// ErrSignatureInvalid means the signature did not verify.
	ErrSignatureInvalid = errors.New("signature does not verify")
	// ErrSignatureExpired means the signature's window has passed.
	ErrSignatureExpired = errors.New("signature has expired")
	// ErrSignatureFromTheFuture means created is further ahead than skew allows.
	ErrSignatureFromTheFuture = errors.New("signature was created in the future")
	// ErrDigestMismatch means the body is not what was signed.
	ErrDigestMismatch = errors.New("body does not match the signed digest")
)

// coveredComponents are what every TDC federated signature covers, in the
// order they appear in the signature base. The order is part of what is
// signed, so it is fixed rather than assembled per request.
var coveredComponents = []string{"@method", "@path", "@authority", "content-digest"}

/*
Signature is what a verified request carries.

Returned by Verify so a caller knows not only that the request was authentic
but which server sent it and which key signed it — the origin is what decides
whose keys verify the events inside, and it comes from the signature rather
than from anything the sender could set freely.
*/
type Signature struct {
	// KeyID is the key that signed, as the sender's discovery document names it.
	KeyID string
	// Created and Expires bound the signature's validity, as absolute instants.
	Created time.Time
	Expires time.Time
	// Raw is the signature bytes, used as the replay cache's key.
	Raw []byte
}

/*
Digest computes the Content-Digest header value for a body.

SHA-256 in the RFC 9530 form the signature then covers. An empty body still
gets a digest — the digest of nothing — because a covered component that is
sometimes absent is a covered component an attacker can remove.
*/
func Digest(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha-256=:" + base64.StdEncoding.EncodeToString(sum[:]) + ":"
}

/*
signatureBase builds the exact bytes that get signed.

RFC 9421 §2.5: one line per covered component, `"name": value`, LF-separated,
ending with the @signature-params line that repeats the covered list and the
parameters. Both sides build this identically from the request itself, which is
what makes altering any covered field break the signature.
*/
func signatureBase(method, path, authority, digest string, created, expires int64, keyID, nonce string) string {
	values := map[string]string{
		"@method":        strings.ToUpper(method),
		"@path":          path,
		"@authority":     authority,
		"content-digest": digest,
	}

	var builder strings.Builder
	for _, component := range coveredComponents {
		builder.WriteString(`"`)
		builder.WriteString(component)
		builder.WriteString(`": `)
		builder.WriteString(values[component])
		builder.WriteString("\n")
	}

	builder.WriteString(`"@signature-params": `)
	builder.WriteString(signatureParams(created, expires, keyID, nonce))
	return builder.String()
}

// signatureParams serialises the inner list and parameters, the form that
// appears both in the Signature-Input header and as the base's final line.
func signatureParams(created, expires int64, keyID, nonce string) string {
	quoted := make([]string, 0, len(coveredComponents))
	for _, component := range coveredComponents {
		quoted = append(quoted, `"`+component+`"`)
	}

	// created and expires are integer seconds since the epoch: absolute
	// instants with no zone, which is what makes two servers in different
	// timezones agree about when a signature stops being valid.
	return fmt.Sprintf(`(%s);created=%d;expires=%d;nonce="%s";keyid="%s";alg="ed25519"`,
		strings.Join(quoted, " "), created, expires, nonce, keyID)
}

/*
Sign adds RFC 9421 signature headers to an outgoing request.

The body is passed separately rather than read from the request, because
reading it here would consume it before it could be sent — and a signature over
a body the recipient never receives is worse than no signature.
*/
func Sign(request *http.Request, body []byte, keyring *events.Keyring, now time.Time) error {
	key := keyring.Current()
	if key == nil {
		return errors.New("no signing key is available")
	}

	digest := Digest(body)
	request.Header.Set("Content-Digest", digest)

	// Unix seconds throughout: an absolute instant, independent of either
	// server's local zone.
	created := now.Unix()
	expires := now.Add(SignatureLifetime).Unix()

	/*
		Normalised rather than taken raw from the request.

		A sender writing "example.org:443" and a verifier computing
		"example.org" for the same destination would produce two different
		signature bases for one request, and the signature would fail for a
		reason neither side could see.
	*/
	authority, err := Authority(request.URL.String())
	if err != nil {
		return fmt.Errorf("determine the destination: %w", err)
	}

	/*
		A nonce, because a timestamp is not unique enough.

		Two identical requests inside the same second — which is what a GET
		with no body repeated quickly is — produce byte-identical signature
		bases and therefore identical signatures. The receiver's replay cache
		cannot tell that from an attacker resending one, so it refuses the
		second, and a server browsing a peer twice in a second is refused for
		having done nothing wrong.

		RFC 9421 defines this parameter for exactly this.
	*/
	nonce, err := newNonce()
	if err != nil {
		return err
	}

	base := signatureBase(request.Method, request.URL.EscapedPath(), authority, digest,
		created, expires, key.ID, nonce)

	signature, err := key.SignBytes([]byte(base))
	if err != nil {
		return fmt.Errorf("sign the request: %w", err)
	}

	request.Header.Set("Signature-Input", signatureLabel+"="+signatureParams(created, expires, key.ID, nonce))
	request.Header.Set("Signature", signatureLabel+"=:"+base64.StdEncoding.EncodeToString(signature)+":")
	return nil
}

/*
Verify checks a request's signature against a peer's published keys.

authority is what this server believes it is — the destination the signature
must name. Passed in rather than read from the request, because a request's Host
header is set by the sender, and letting the sender tell us which destination to
compare against would defeat the whole point of covering it.
*/
func Verify(request *http.Request, body []byte, keyring *events.Keyring, authority string, now time.Time) (Signature, error) {
	input := request.Header.Get("Signature-Input")
	raw := request.Header.Get("Signature")
	if input == "" || raw == "" {
		return Signature{}, ErrNotSigned
	}

	params, err := parseSignatureInput(input)
	if err != nil {
		return Signature{}, err
	}

	signature, err := parseSignature(raw)
	if err != nil {
		return Signature{}, err
	}

	/*
		Time is checked before the cryptography, deliberately.

		An expired signature is refused whether or not it verifies, so there is
		nothing to learn by presenting a forged one — and it saves the
		verification work on requests that are already worthless.
	*/
	created := time.Unix(params.created, 0)
	expires := time.Unix(params.expires, 0)

	if now.After(expires) {
		return Signature{}, fmt.Errorf("%w: expired at %s, now %s",
			ErrSignatureExpired, expires.UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339))
	}
	if created.After(now.Add(ClockSkewTolerance)) {
		return Signature{}, fmt.Errorf("%w: created at %s, now %s — check the clocks",
			ErrSignatureFromTheFuture, created.UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339))
	}

	// The digest the sender signed must match the body actually delivered.
	digest := request.Header.Get("Content-Digest")
	if digest == "" {
		return Signature{}, fmt.Errorf("%w: no Content-Digest", ErrSignatureMalformed)
	}
	if computed := Digest(body); computed != digest {
		return Signature{}, fmt.Errorf("%w: header says %s, body digests to %s",
			ErrDigestMismatch, digest, computed)
	}

	key, found := keyring.Lookup(params.keyID)
	if !found {
		// Not an error the caller should treat as final: an unrecognised key
		// is what a rotation looks like, and the caller re-fetches and retries.
		return Signature{}, fmt.Errorf("%w: no key %q", ErrSignatureInvalid, params.keyID)
	}

	base := signatureBase(request.Method, request.URL.EscapedPath(), authority, digest,
		params.created, params.expires, params.keyID, params.nonce)

	if !key.VerifyBytes([]byte(base), signature) {
		/*
			Deliberately one message for several causes: a tampered field, a
			body swapped after signing, or a request signed for a different
			server and replayed here.

			They cannot be told apart at this point, and pretending otherwise
			would be worse than saying so. The authority is an input to the
			signature, not a value carried in the headers — a request signed
			for another server arrives looking perfectly ordinary, and the
			only thing that reveals it is that the signature does not verify
			against the destination we know ourselves to be. There is no
			field to compare and report.

			An operator diagnosing this reads the sending server's log, which
			names the destination it signed for.
		*/
		return Signature{}, fmt.Errorf("%w against %s", ErrSignatureInvalid, authority)
	}

	return Signature{
		KeyID:   params.keyID,
		Created: created,
		Expires: expires,
		Raw:     signature,
	}, nil
}

// signatureInput is the parsed Signature-Input header.
type signatureInput struct {
	keyID   string
	nonce   string
	created int64
	expires int64
}

func parseSignatureInput(header string) (signatureInput, error) {
	// label=("@method" …);created=…;expires=…;keyid="…";alg="…"
	label, rest, found := strings.Cut(header, "=")
	if !found || strings.TrimSpace(label) != signatureLabel {
		return signatureInput{}, fmt.Errorf("%w: expected a %q signature", ErrSignatureMalformed, signatureLabel)
	}

	listEnd := strings.Index(rest, ")")
	if !strings.HasPrefix(strings.TrimSpace(rest), "(") || listEnd < 0 {
		return signatureInput{}, fmt.Errorf("%w: no covered component list", ErrSignatureMalformed)
	}

	covered := rest[strings.Index(rest, "(")+1 : listEnd]
	if err := checkCovered(covered); err != nil {
		return signatureInput{}, err
	}

	parsed := signatureInput{}
	for _, parameter := range strings.Split(rest[listEnd+1:], ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(parameter), "=")
		if !ok {
			continue
		}
		value = strings.Trim(value, `"`)

		var err error
		switch name {
		case "created":
			parsed.created, err = strconv.ParseInt(value, 10, 64)
		case "expires":
			parsed.expires, err = strconv.ParseInt(value, 10, 64)
		case "keyid":
			parsed.keyID = value
		case "nonce":
			parsed.nonce = value
		}
		if err != nil {
			return signatureInput{}, fmt.Errorf("%w: %s is not an integer", ErrSignatureMalformed, name)
		}
	}

	if parsed.keyID == "" {
		return signatureInput{}, fmt.Errorf("%w: no keyid", ErrSignatureMalformed)
	}
	if parsed.created == 0 || parsed.expires == 0 {
		return signatureInput{}, fmt.Errorf("%w: created and expires are required", ErrSignatureMalformed)
	}
	return parsed, nil
}

/*
checkCovered refuses a signature that does not cover everything we require.

Without this, a sender could sign a request covering only @method — a perfectly
valid RFC 9421 signature that verifies, over almost nothing. The signature must
be over what we decided it would be over, not over whatever the sender chose.
*/
func checkCovered(list string) error {
	present := map[string]bool{}
	for _, component := range strings.Fields(list) {
		present[strings.Trim(component, `"`)] = true
	}

	var missing []string
	for _, required := range coveredComponents {
		if !present[required] {
			missing = append(missing, required)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("%w: signature does not cover %s",
			ErrSignatureMalformed, strings.Join(missing, ", "))
	}
	return nil
}

func parseSignature(header string) ([]byte, error) {
	label, rest, found := strings.Cut(header, "=")
	if !found || strings.TrimSpace(label) != signatureLabel {
		return nil, fmt.Errorf("%w: expected a %q signature", ErrSignatureMalformed, signatureLabel)
	}

	encoded := strings.Trim(strings.TrimSpace(rest), ":")
	signature, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("%w: signature is not base64", ErrSignatureMalformed)
	}
	return signature, nil
}

// newNonce makes a value distinguishing two otherwise identical requests.
func newNonce() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate a nonce: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
