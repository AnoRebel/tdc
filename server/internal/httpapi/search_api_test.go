package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

type searchHit struct {
	EventID     string `json:"eventId"`
	RoomID      string `json:"roomId"`
	Sender      string `json:"sender"`
	Body        string `json:"body"`
	Highlighted string `json:"highlighted"`
}

// searchFor runs a search and decodes the results.
func searchFor(t *testing.T, handler http.Handler, token, query string) []searchHit {
	t.Helper()

	recorder := call(t, handler, http.MethodGet, "/api/search?q="+query, nil, token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("search returned %d: %s", recorder.Code, truncate(recorder.Body.String()))
	}

	var hits []searchHit
	if err := json.Unmarshal(recorder.Body.Bytes(), &hits); err != nil {
		t.Fatalf("decoding results: %v", err)
	}
	return hits
}

// TestSearchFindsMessages covers the ordinary case, including the highlighting
// that tells someone why a result matched.
func TestSearchFindsMessages(t *testing.T) {
	handler := newTestAPI(t)

	token := completeSetup(t, handler)
	roomID := makeRoom(t, handler, token, "general", "public")

	for _, body := range []string{
		"Mkutano wa mwezi huu utakuwa Jumatano",
		"Nitaleta projector kwa ajili ya warsha",
		"Karibu wote kwenye mkutano",
	} {
		sendMessage(t, handler, token, roomID, body)
	}

	hits := searchFor(t, handler, token, "mkutano")
	if len(hits) != 2 {
		t.Fatalf("found %d messages matching \"mkutano\", expected 2", len(hits))
	}

	// Recency first, not relevance: chat messages are short, so nearly every
	// term appears once and rank degenerates into noise.
	if !strings.Contains(hits[0].Body, "Karibu wote") {
		t.Errorf("the first result is %q; the most recent match should lead", hits[0].Body)
	}

	// The match is marked, so a client can show why without guessing.
	if !strings.Contains(strings.ToLower(hits[0].Highlighted), "<mark>") {
		t.Errorf("no highlighting in %q", hits[0].Highlighted)
	}
}

// TestSearchNeverReturnsUnreadableMessages is the requirement this feature
// lives or dies by.
//
// Search reaches across every room at once, which makes it the easiest place
// in the whole product to leak a private conversation. The authorisation is a
// join in the query rather than a filter afterwards: a result fetched and then
// discarded has still been read, and a mistake in the discarding is a leak
// where a mistake in a join is an empty result.
func TestSearchNeverReturnsUnreadableMessages(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	private := makeRoom(t, handler, owner, "siri", "private")

	sendMessage(t, handler, owner, private, "Nenosiri ni kitu cha siri")

	// The owner finds their own message.
	if hits := searchFor(t, handler, owner, "nenosiri"); len(hits) != 1 {
		t.Fatalf("the owner found %d results in their own room, expected 1", len(hits))
	}

	// Somebody who is not in that room finds nothing, and is not told why.
	outsider := registerAndSignIn(t, handler, "mgeni")
	if hits := searchFor(t, handler, outsider, "nenosiri"); len(hits) != 0 {
		t.Errorf("a non-member found %d results in a private room", len(hits))
	}
}

// TestSearchSurvivesHostileInput is why websearch_to_tsquery was chosen.
//
// to_tsquery raises a syntax error on unbalanced quotes, stray operators and
// the rest, which would turn a search box into a way to make the server return
// errors — and any hand-rolled sanitising of it into a thing to get wrong.
// websearch_to_tsquery never raises, whatever is typed at it.
func TestSearchSurvivesHostileInput(t *testing.T) {
	handler := newTestAPI(t)

	token := completeSetup(t, handler)
	roomID := makeRoom(t, handler, token, "general", "public")
	sendMessage(t, handler, token, roomID, "Habari za asubuhi")

	for _, hostile := range []string{
		"'", `"`, "()", "&|!", "a & | b", `"unclosed`, "*", "\\", ":*",
		"' OR 1=1 --", "<script>alert(1)</script>", strings.Repeat("a", 500),
	} {
		recorder := call(t, handler, http.MethodGet,
			"/api/search?q="+urlQueryEscape(hostile), nil, token)

		if recorder.Code != http.StatusOK {
			t.Errorf("the query %q returned %d; it should simply match nothing",
				hostile, recorder.Code)
		}
	}
}

// TestPrefixSearchFindsWhatFullTextCannot checks the second path exists for a
// reason.
//
// websearch_to_tsquery matches whole lexemes, so a search-as-you-type box that
// used it alone would find nothing until the last letter was typed. Trigrams
// find the prefix, and tolerate a typo while they are at it.
func TestPrefixSearchFindsWhatFullTextCannot(t *testing.T) {
	handler := newTestAPI(t)

	token := completeSetup(t, handler)
	roomID := makeRoom(t, handler, token, "general", "public")
	sendMessage(t, handler, token, roomID, "Mkutano wa TDC utafanyika kesho")

	// A partial word finds nothing through full text.
	if hits := searchFor(t, handler, token, "mkut"); len(hits) != 0 {
		t.Errorf("full-text search matched the prefix %q; that is not what it does", "mkut")
	}

	// And finds it through trigrams.
	recorder := call(t, handler, http.MethodGet, "/api/search?q=mkut&prefix=true", nil, token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("prefix search returned %d", recorder.Code)
	}

	var hits []searchHit
	json.Unmarshal(recorder.Body.Bytes(), &hits)
	if len(hits) == 0 {
		t.Error("prefix search found nothing; autocomplete would never work")
	}
}

// TestDeletedMessagesLeaveTheIndex checks that a deletion deletes.
//
// A message removed from the conversation but still findable by search would
// be a deletion that did not delete — and search is exactly where somebody
// would go looking for what was removed.
func TestDeletedMessagesLeaveTheIndex(t *testing.T) {
	handler := newTestAPI(t)

	token := completeSetup(t, handler)
	roomID := makeRoom(t, handler, token, "general", "public")

	eventID := sendMessage(t, handler, token, roomID, "Ujumbe wa kufutwa")

	if hits := searchFor(t, handler, token, "kufutwa"); len(hits) != 1 {
		t.Fatalf("found %d before deletion, expected 1", len(hits))
	}

	deleted := call(t, handler, http.MethodDelete,
		"/api/rooms/"+roomID+"/messages/"+eventID, nil, token)
	if deleted.Code != http.StatusCreated {
		t.Fatalf("deleting returned %d", deleted.Code)
	}

	if hits := searchFor(t, handler, token, "kufutwa"); len(hits) != 0 {
		t.Errorf("a deleted message is still findable: %d results", len(hits))
	}
}

// urlQueryEscape encodes a value for a query string.
func urlQueryEscape(value string) string {
	const hex = "0123456789ABCDEF"

	encoded := make([]byte, 0, len(value)*3)
	for _, b := range []byte(value) {
		if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') ||
			b == '-' || b == '_' || b == '.' || b == '~' {
			encoded = append(encoded, b)
			continue
		}
		encoded = append(encoded, '%', hex[b>>4], hex[b&0x0f])
	}
	return string(encoded)
}
