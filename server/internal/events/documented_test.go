package events_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/AnoRebel/tdc/server/internal/events"
)

/*
The published derivation, implemented from the document alone.

If an independent implementer following docs/federation-protocol.md reaches a
different identifier than this server does, the document is wrong — which is
the failure it exists to prevent, and which nothing else would catch.
*/
func TestTheDocumentedDerivationMatchesTheImplementation(t *testing.T) {
	event := events.Event{
		Room:      "townsquare@a.example.org",
		Sender:    "amina@b.example.org",
		Origin:    "b.example.org",
		Type:      events.TypeMessageSent,
		CreatedAt: 1758553650123,
		Nonce:     "Yx3KpQ2fN8vL1mR4",
		// Deliberately unsorted and loosely spaced, as a sender may send it.
		Content: json.RawMessage(`{"body":"Good morning.",  "alpha":1}`),
	}

	// Step 1, from the document: SHA-256 of the canonical JSON of content.
	var parsed any
	if err := json.Unmarshal(event.Content, &parsed); err != nil {
		t.Fatalf("parse content: %v", err)
	}
	canonicalContent, err := json.Marshal(parsed)
	if err != nil {
		t.Fatalf("re-encode content: %v", err)
	}
	contentSum := sha256.Sum256(canonicalContent)
	contentHash := base64.RawURLEncoding.EncodeToString(contentSum[:])

	if contentHash != events.HashContent(event.Content) {
		t.Fatalf("the documented content hash %q does not match HashContent %q",
			contentHash, events.HashContent(event.Content))
	}

	// Step 2, from the document: this exact object, in this key order.
	form := struct {
		Room        string `json:"room"`
		Sender      string `json:"sender"`
		Origin      string `json:"origin"`
		Type        string `json:"type"`
		CreatedAt   int64  `json:"createdAt"`
		Nonce       string `json:"nonce"`
		ContentHash string `json:"contentHash"`
	}{
		Room:        event.Room,
		Sender:      event.Sender,
		Origin:      event.Origin,
		Type:        string(event.Type),
		CreatedAt:   event.CreatedAt,
		Nonce:       event.Nonce,
		ContentHash: contentHash,
	}
	canonical, err := json.Marshal(form)
	if err != nil {
		t.Fatalf("encode canonical form: %v", err)
	}

	// Step 3: "$" plus base64url-unpadded SHA-256 of those bytes.
	sum := sha256.Sum256(canonical)
	documented := "$" + base64.RawURLEncoding.EncodeToString(sum[:])

	actual, err := event.DeriveID()
	if err != nil {
		t.Fatalf("DeriveID: %v", err)
	}

	if documented != actual {
		t.Fatalf("an implementer following the document derives %s,\n"+
			"this server derives                                %s", documented, actual)
	}
	t.Logf("both derive %s", actual)
}
