package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// configureFilter sets the word list and what to do about a match.
func configureFilter(t *testing.T, handler http.Handler, token, words, action string) {
	t.Helper()

	setSetting(t, handler, token, "filter.words", words)
	setSetting(t, handler, token, "filter.action", action)
}

/*
TestFilteringIsOffUntilAnOperatorConfiguresIt is the default the requirement
names.

Off is the absence of a word list rather than a separate switch, so there is
one fewer thing to set and one fewer thing to forget.
*/
func TestFilteringIsOffUntilAnOperatorConfiguresIt(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	// Nothing configured. Anything goes through.
	sent := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
		map[string]any{"body": "matusi na matusi tena"}, owner)
	if sent.Code != http.StatusCreated {
		t.Errorf("a message was filtered with no filter configured: %d", sent.Code)
	}

	if open := openReportCount(t, handler, owner); open != 0 {
		t.Errorf("%d reports were raised with no filter configured", open)
	}
}

/*
TestABlockingFilterRefusesTheMessage covers the first of the three actions.

The refusal says a content policy refused it and nothing more. Naming the word
would turn the endpoint into an oracle: one message per candidate word, and the
refusals give back the operator's whole list.
*/
func TestABlockingFilterRefusesTheMessage(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	configureFilter(t, handler, owner, "matusi", "block")

	recorder := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
		map[string]any{"body": "hii ni matusi"}, owner)

	if recorder.Code == http.StatusCreated {
		t.Fatal("a blocking filter accepted a matching message")
	}

	body := recorder.Body.String()
	if !strings.Contains(strings.ToLower(body), "content policy") {
		t.Errorf("the refusal does not say why: %s", truncate(body))
	}
	if strings.Contains(body, "matusi") {
		t.Errorf("the refusal named the word that matched, which lets the "+
			"list be recovered one message at a time: %s", truncate(body))
	}

	// And nothing was stored.
	timeline := decodeTimeline(t, call(t, handler, http.MethodGet,
		"/api/rooms/"+roomID+"/timeline?limit=50", nil, owner))
	for _, event := range timeline {
		if strings.Contains(bodyOfEvent(event), "matusi") {
			t.Error("a blocked message was stored anyway")
		}
	}
}

/*
TestAMaskingFilterDeliversWithTheWordsReplaced covers the second action.

Masked before storing rather than after. A message stored and then edited would
have existed unmasked, and anything watching the timeline live would have seen
it.
*/
func TestAMaskingFilterDeliversWithTheWordsReplaced(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	configureFilter(t, handler, owner, "matusi", "mask")

	recorder := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
		map[string]any{"body": "hii ni matusi kabisa"}, owner)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("a masking filter refused the message: %d: %s",
			recorder.Code, truncate(recorder.Body.String()))
	}

	// Stored masked, not merely displayed masked.
	timeline := decodeTimeline(t, call(t, handler, http.MethodGet,
		"/api/rooms/"+roomID+"/timeline?limit=50", nil, owner))

	found := false
	for _, event := range timeline {
		body := bodyOfEvent(event)
		if strings.Contains(body, "hii ni") {
			found = true
			if strings.Contains(body, "matusi") {
				t.Errorf("the word survived masking: %q", body)
			}
			if !strings.Contains(body, "******") {
				t.Errorf("nothing was masked: %q", body)
			}
			// The rest of the sentence survives, or masking would be
			// indistinguishable from corruption.
			if !strings.Contains(body, "kabisa") {
				t.Errorf("masking removed more than the matched word: %q", body)
			}
		}
	}
	if !found {
		t.Error("the masked message was not delivered at all")
	}
}

/*
TestAFlaggingFilterDeliversAndQueuesForReview covers the third action, and the
scenario that says a flagged message reaches the moderation queue.
*/
func TestAFlaggingFilterDeliversAndQueuesForReview(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	configureFilter(t, handler, owner, "matusi", "flag")

	recorder := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
		map[string]any{"body": "hii ni matusi"}, owner)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("a flagging filter refused the message: %d", recorder.Code)
	}

	// Delivered unaltered: flagging does not change what was said.
	timeline := decodeTimeline(t, call(t, handler, http.MethodGet,
		"/api/rooms/"+roomID+"/timeline?limit=50", nil, owner))
	delivered := false
	for _, event := range timeline {
		if strings.Contains(bodyOfEvent(event), "hii ni matusi") {
			delivered = true
		}
	}
	if !delivered {
		t.Error("a flagged message was altered or not delivered")
	}

	// And a moderator sees it.
	reports := decodeReports(t, call(t, handler, http.MethodGet, "/api/reports", nil, owner))
	if len(reports) != 1 {
		t.Fatalf("expected one report in the queue, got %d", len(reports))
	}

	report := reports[0]
	if !strings.Contains(report.ReportedBody, "matusi") {
		t.Errorf("the queued report does not carry what was said: %+v", report)
	}
	if report.Reporter == "" {
		t.Error("the report has no reporter label, so a moderator cannot tell " +
			"an automatic flag from somebody's own report")
	}
}

/*
TestEditingCannotWalkAroundTheFilter is the bypass a filter on the send path
alone would leave.

Send something harmless, then edit it into anything. A filter that stopped at
sending would be one sentence away from useless.
*/
func TestEditingCannotWalkAroundTheFilter(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	configureFilter(t, handler, owner, "matusi", "block")

	eventID := sendMessage(t, handler, owner, roomID, "habari za asubuhi")

	edited := call(t, handler, http.MethodPatch,
		"/api/rooms/"+roomID+"/messages/"+eventID,
		map[string]any{"body": "sasa ni matusi"}, owner)

	if edited.Code == http.StatusCreated || edited.Code == http.StatusOK {
		t.Error("an edit walked around the content filter")
	}
}

/*
TestAnEditIsMaskedNotMerelyChecked covers what blocking alone cannot show.

Under a blocking filter the edit is refused before the stored text is ever
chosen, so a handler that checked the message and then stored the unfiltered
one would still pass. Masking is where the difference is visible: the check
must decide what is written, not merely whether to proceed.
*/
func TestAnEditIsMaskedNotMerelyChecked(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	eventID := sendMessage(t, handler, owner, roomID, "habari za asubuhi")

	configureFilter(t, handler, owner, "matusi", "mask")

	edited := call(t, handler, http.MethodPatch,
		"/api/rooms/"+roomID+"/messages/"+eventID,
		map[string]any{"body": "sasa ni matusi kabisa"}, owner)
	if edited.Code != http.StatusCreated && edited.Code != http.StatusOK {
		t.Fatalf("the edit was refused: %d: %s",
			edited.Code, truncate(edited.Body.String()))
	}

	if strings.Contains(edited.Body.String(), "matusi") {
		t.Errorf("the edit was stored unmasked: %s", truncate(edited.Body.String()))
	}

	// And in the timeline, which is what other people read.
	timeline := decodeTimeline(t, call(t, handler, http.MethodGet,
		"/api/rooms/"+roomID+"/timeline?limit=50", nil, owner))
	for _, event := range timeline {
		if strings.Contains(bodyOfEvent(event), "matusi") {
			t.Errorf("an unmasked edit reached the timeline: %q", bodyOfEvent(event))
		}
	}
}

// TestThreadRepliesAreFilteredToo covers the other place a message can be
// written, which a filter on the top level alone would miss.
func TestThreadRepliesAreFilteredToo(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	root := sendMessage(t, handler, owner, roomID, "swali langu")

	configureFilter(t, handler, owner, "matusi", "block")

	reply := call(t, handler, http.MethodPost,
		"/api/rooms/"+roomID+"/threads/"+root+"/replies",
		map[string]any{"body": "jibu lenye matusi"}, owner)

	if reply.Code == http.StatusCreated || reply.Code == http.StatusOK {
		t.Error("a thread reply walked around the content filter")
	}
}

/*
TestScheduledMessagesAreFilteredWhenScheduled covers the path with nobody
present at delivery.

Blocking has to refuse while the author is here to be told. A message accepted
now and dropped in an hour is a message its author believes was sent.
*/
func TestScheduledMessagesAreFilteredWhenScheduled(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	configureFilter(t, handler, owner, "matusi", "block")

	recorder := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/scheduled",
		map[string]any{"body": "matusi baadaye", "sendAt": whenSoon()}, owner)

	if recorder.Code == http.StatusCreated || recorder.Code == http.StatusOK {
		t.Error("a scheduled message walked around the content filter")
	}
}

/*
TestAWordListDoesNotCatchWordsContainingIt is the Scunthorpe problem, checked
through the API rather than only in the filter's own tests.

A server that refuses "class" because the list contains "ass" is one operators
turn off, and then the filter protects nobody.
*/
func TestAWordListDoesNotCatchWordsContainingIt(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	configureFilter(t, handler, owner, "ass", "block")

	recorder := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
		map[string]any{"body": "the class was long and the passage longer"}, owner)

	if recorder.Code != http.StatusCreated {
		t.Errorf("an innocent message was refused: %d: %s",
			recorder.Code, truncate(recorder.Body.String()))
	}
}

// TestAnInvalidFilterActionIsRefused keeps a typo from meaning something.
func TestAnInvalidFilterActionIsRefused(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	recorder := call(t, handler, http.MethodPut, "/api/admin/settings/filter.action",
		map[string]any{"value": "delete"}, owner)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("an unknown filter action returned %d, expected 400", recorder.Code)
	}
}

// openReportCount reads how many reports are waiting.
func openReportCount(t *testing.T, handler http.Handler, token string) int {
	t.Helper()
	return len(decodeReports(t, call(t, handler, http.MethodGet, "/api/reports", nil, token)))
}

// decodeTimeline reads a room's events.
func decodeTimeline(t *testing.T, recorder *httptest.ResponseRecorder) []map[string]any {
	t.Helper()

	if recorder.Code != http.StatusOK {
		t.Fatalf("reading the timeline returned %d: %s",
			recorder.Code, truncate(recorder.Body.String()))
	}

	var timeline []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &timeline); err != nil {
		t.Fatalf("decode timeline: %v", err)
	}
	return timeline
}

// bodyOfEvent reads a message's text, or empty for an event that has none.
func bodyOfEvent(event map[string]any) string {
	content, _ := event["content"].(map[string]any)
	body, _ := content["body"].(string)
	return body
}
