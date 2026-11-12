package httpapi_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// searchIssues runs an issue search and returns the identifiers found, in order.
func searchIssues(t *testing.T, handler http.Handler, token string, parameters url.Values) []string {
	t.Helper()
	body := decode(t, mustCall(t, handler, http.MethodGet, "/api/issues/search?"+parameters.Encode(),
		nil, token, http.StatusOK))
	keys := []string{}
	for _, raw := range body["issues"].([]any) {
		keys = append(keys, raw.(map[string]any)["key"].(string))
	}
	return keys
}

/*
A private project's issues are absent from every search an outsider makes —
by words, by filters, and by naming the project — and no input, however
hostile, makes the search fail.
*/
func TestSearchNeverShowsAnOutsiderAPrivateProject(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	outsider := registerMember(t, handler, "outsider")

	startProject(t, handler, founder, "SEC", "private")
	startProject(t, handler, founder, "OPEN", "public")
	fileIssue(t, handler, founder, "SEC", "Login bypass through the recovery form")
	fileIssue(t, handler, founder, "OPEN", "Login page is slow on 3G")

	both := searchIssues(t, handler, founder, url.Values{"q": {"login"}})
	if len(both) != 2 {
		t.Fatalf("the project's member finds %v, want both issues", both)
	}
	for _, parameters := range []url.Values{
		{"q": {"login"}},
		{"q": {"bypass recovery"}},
		{"q": {"logi"}, "prefix": {"true"}},
		{"state": {"To do"}},
		{"project": {"SEC"}},
		{"project": {"SEC"}, "q": {"login"}},
	} {
		for _, key := range searchIssues(t, handler, outsider, parameters) {
			if strings.HasPrefix(key, "SEC-") {
				t.Errorf("searching %v shows an outsider %s", parameters, key)
			}
		}
	}
	if found := searchIssues(t, handler, outsider, url.Values{"project": {"SEC"}}); len(found) != 0 {
		t.Errorf("narrowing to a private project's key found %v", found)
	}

	for _, hostile := range []string{"'", `"unclosed`, "&&||!", "login:*", ")))", "<>", "\\", "--", "a & ! | b"} {
		recorder := call(t, handler, http.MethodGet, "/api/issues/search?"+url.Values{"q": {hostile}}.Encode(), nil, outsider)
		if recorder.Code != http.StatusOK {
			t.Errorf("searching for %q answered %d: %s", hostile, recorder.Code, recorder.Body.String())
		}
	}
}

/*
Words and filters narrow together: every one given must hold, whichever
project the issue is in.
*/
func TestSearchFiltersNarrowTogether(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	registerMember(t, handler, "amina")
	one := startProject(t, handler, founder, "ONE", "public")
	two := startProject(t, handler, founder, "TWO", "public")
	for _, key := range []string{"ONE", "TWO"} {
		mustCall(t, handler, http.MethodPost, "/api/projects/"+key+"/labels", map[string]any{"name": "bug"}, founder, http.StatusCreated)
		mustCall(t, handler, http.MethodPut, "/api/projects/"+key+"/members/amina", map[string]any{"role": "member"}, founder, http.StatusNoContent)
	}

	fileIssue(t, handler, founder, "ONE", "Crash when uploading a photo")    // ONE-1: bug, Done, amina, Urgent
	fileIssue(t, handler, founder, "ONE", "Crash when uploading a video")    // ONE-2: bug, To do
	fileIssue(t, handler, founder, "TWO", "Crash when uploading a document") // TWO-1: bug, Done, amina
	fileIssue(t, handler, founder, "TWO", "Upload button misaligned")        // TWO-2: Done, amina

	labelIn := func(ref, key string) {
		project := decode(t, mustCall(t, handler, http.MethodGet, "/api/projects/"+key, nil, founder, http.StatusOK))
		mustCall(t, handler, http.MethodPut, "/api/issues/"+ref+"/labels/"+idNamed(t, project["labels"], "bug"), nil, founder, http.StatusOK)
	}
	labelIn("ONE-1", "ONE")
	labelIn("ONE-2", "ONE")
	labelIn("TWO-1", "TWO")
	for ref, project := range map[string]map[string]any{"ONE-1": one, "TWO-1": two, "TWO-2": two} {
		mustCall(t, handler, http.MethodPost, "/api/issues/"+ref+"/state", map[string]any{"stateId": stateNamed(t, project, "Done")}, founder, http.StatusOK)
		mustCall(t, handler, http.MethodPut, "/api/issues/"+ref+"/assignees/amina", nil, founder, http.StatusOK)
	}
	mustCall(t, handler, http.MethodPut, "/api/issues/ONE-1/priority",
		map[string]any{"priorityId": idNamed(t, one["priorities"], "Urgent")}, founder, http.StatusOK)

	cases := []struct {
		parameters url.Values
		want       string
	}{
		{url.Values{"q": {"crash"}}, "ONE-1,ONE-2,TWO-1"},
		{url.Values{"q": {"crash"}, "state": {"done"}}, "ONE-1,TWO-1"},
		{url.Values{"q": {"crash"}, "state": {"Done"}, "label": {"bug"}, "assignee": {"amina"}}, "ONE-1,TWO-1"},
		{url.Values{"q": {"crash"}, "state": {"Done"}, "label": {"bug"}, "project": {"TWO"}}, "TWO-1"},
		{url.Values{"q": {"crash"}, "priority": {"urgent"}}, "ONE-1"},
		{url.Values{"state": {"Done"}, "assignee": {"amina"}}, "ONE-1,TWO-1,TWO-2"},
		{url.Values{"completed": {"false"}}, "ONE-2"},
		{url.Values{"q": {"crash"}, "label": {"docs"}}, ""},
	}
	for _, c := range cases {
		found := searchIssues(t, handler, founder, c.parameters)
		got := map[string]bool{}
		for _, key := range found {
			got[key] = true
		}
		want := map[string]bool{}
		for _, key := range strings.Split(c.want, ",") {
			if key != "" {
				want[key] = true
			}
		}
		if len(got) != len(want) {
			t.Errorf("%v found %v, want %s", c.parameters, found, c.want)
			continue
		}
		for key := range want {
			if !got[key] {
				t.Errorf("%v found %v, want %s", c.parameters, found, c.want)
			}
		}
	}
}

// Results come most recently changed first, and an edit is findable by its
// new words and no longer by its old ones.
func TestSearchIsByRecencyAndFollowsEdits(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	startProject(t, handler, founder, "RANK", "public")
	fileIssue(t, handler, founder, "RANK", "Printing invoices fails")
	fileIssue(t, handler, founder, "RANK", "Printing receipts fails")

	if order := strings.Join(searchIssues(t, handler, founder, url.Values{"q": {"printing"}}), ","); order != "RANK-2,RANK-1" {
		t.Errorf("before any change, the order is %s", order)
	}
	mustCall(t, handler, http.MethodPatch, "/api/issues/RANK-1", map[string]any{"title": "Printing invoices fails on A5 paper"}, founder, http.StatusOK)
	if order := strings.Join(searchIssues(t, handler, founder, url.Values{"q": {"printing"}}), ","); order != "RANK-1,RANK-2" {
		t.Errorf("after RANK-1 changed, the order is %s", order)
	}

	mustCall(t, handler, http.MethodPatch, "/api/issues/RANK-2", map[string]any{"title": "Receipt totals are rounded wrongly"}, founder, http.StatusOK)
	if found := searchIssues(t, handler, founder, url.Values{"q": {"rounded"}}); strings.Join(found, ",") != "RANK-2" {
		t.Errorf("the new words find %v", found)
	}
	for _, key := range searchIssues(t, handler, founder, url.Values{"q": {"printing receipts"}}) {
		if key == "RANK-2" {
			t.Error("the old title still finds the renamed issue")
		}
	}

	mustCall(t, handler, http.MethodDelete, "/api/issues/RANK-1", nil, founder, http.StatusNoContent)
	if found := searchIssues(t, handler, founder, url.Values{"q": {"invoices"}}); len(found) != 0 {
		t.Errorf("a deleted issue is still found: %v", found)
	}
}

/*
What was said about an issue is found alongside it, by anybody who may see the
issue — whether or not they took part — and by nobody who may not.
*/
func TestSearchFindsWhatWasSaidAboutAnIssue(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	reader := registerMember(t, handler, "reader")

	startProject(t, handler, founder, "PUB", "public")
	startProject(t, handler, founder, "PRIV", "private")
	public := fileIssue(t, handler, founder, "PUB", "Session expiry")
	private := fileIssue(t, handler, founder, "PRIV", "Session storage")
	comment(t, handler, founder, public["id"].(string), "The refresh token rotates every hour")
	comment(t, handler, founder, private["id"].(string), "The refresh token is stored in plain text")

	body := decode(t, mustCall(t, handler, http.MethodGet, "/api/issues/search?q=refresh", nil, reader, http.StatusOK))
	discussions := body["discussions"].([]any)
	if len(discussions) != 1 || discussions[0].(map[string]any)["issueKey"] != "PUB-1" {
		t.Errorf("somebody who never joined the discussion finds %v, want only PUB-1's", discussions)
	}

	own := decodeRows(t, mustCall(t, handler, http.MethodGet, "/api/issues/PUB-1/discussion/search?q=rotates",
		nil, reader, http.StatusOK).Body.Bytes())
	if len(own) != 1 {
		t.Errorf("searching the issue's own discussion found %v", own)
	}
	mustCall(t, handler, http.MethodGet, "/api/issues/PRIV-1/discussion/search?q=refresh", nil, reader, http.StatusNotFound)
}
