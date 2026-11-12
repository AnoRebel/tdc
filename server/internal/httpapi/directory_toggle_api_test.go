package httpapi_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
)

/*
Switching the directory off takes away every part of it, and switching it back
on brings everything back as it was.

Every route is found from the server's own description rather than from a list
kept here, so a route added later is covered without anybody remembering to add
it: if it is under /api/directory it must answer 404 while the directory is off.
*/
func TestSwitchingTheDirectoryOffRemovesEverySurface(t *testing.T) {
	handler, founder := withDirectory(t)
	juma := publicDeveloper(t, handler, "juma")
	client := registerAndSignIn(t, handler, "client")
	engagement := engagementBetween(t, handler, client, juma)
	id := engagement["id"].(string)
	version := propose(t, handler, juma, id, "Clinic booking app", 150_000_000)

	memberSearch := func() string {
		recorder := call(t, handler, http.MethodGet, "/api/search?q=juma", nil, client)
		return recorder.Body.String()
	}
	searchedWhileOn := memberSearch()
	profileWhileOn := call(t, handler, http.MethodGet, "/api/directory/me", nil, juma).Body.String()
	negotiationWhileOn := negotiation(t, handler, juma, id)
	if !slices.Contains(capabilities(t, handler, client), "directory") {
		t.Fatal("the server does not advertise the directory while it is on")
	}

	setFeature(t, handler, founder, "directory", false)

	if slices.Contains(capabilities(t, handler, client), "directory") {
		t.Error("the server still advertises the directory after it was switched off")
	}

	values := map[string]string{
		"id": id, "versionId": version, "username": "juma", "slug": "acme",
		"user": "juma", "skill": "go",
	}
	tried := 0
	for _, endpoint := range describedEndpoints(t, handler, founder) {
		if !strings.HasPrefix(endpoint.Path, "/api/directory") {
			continue
		}
		path := endpoint.Path
		for _, parameter := range endpoint.Parameters {
			value, known := values[parameter]
			if !known {
				value = "00000000-0000-0000-0000-000000000000"
			}
			path = strings.Replace(path, ":"+parameter, value, 1)
		}
		for name, token := range map[string]string{"the founder": founder, "a party": juma, "nobody signed in": ""} {
			recorder := call(t, handler, endpoint.Method, path, map[string]any{}, token)
			if recorder.Code != http.StatusNotFound {
				t.Errorf("%s %s answered %s with %d while the directory is off: %s",
					endpoint.Method, path, name, recorder.Code, recorder.Body.String())
			}
		}
		tried++
	}
	// A description that listed nothing would pass the loop above vacuously.
	if tried < 40 {
		t.Fatalf("only %d directory routes were described; the check did not reach them", tried)
	}

	// Member search is the search it always was: the directory never added
	// anything to it, and taking the directory away takes nothing from it.
	if searched := memberSearch(); searched != searchedWhileOn {
		t.Errorf("member search changed with the directory:\non:  %s\noff: %s", searchedWhileOn, searched)
	}

	// On again: no migration, nothing lost, nothing to redo.
	setFeature(t, handler, founder, "directory", true)
	if profile := call(t, handler, http.MethodGet, "/api/directory/me", nil, juma).Body.String(); profile != profileWhileOn {
		t.Errorf("the profile changed across off and on:\nbefore: %s\nafter:  %s", profileWhileOn, profile)
	}
	if again := negotiation(t, handler, juma, id); mustJSON(t, again) != mustJSON(t, negotiationWhileOn) {
		t.Errorf("the proposal changed across off and on:\nbefore: %v\nafter:  %v", negotiationWhileOn, again)
	}
	if code, seen, body := engagementAs(t, handler, client, id); code != http.StatusOK || seen["state"] != "proposal" {
		t.Errorf("the engagement did not come back as it was: %d %s", code, body)
	}
}

type describedEndpoint struct {
	Method     string   `json:"method"`
	Path       string   `json:"path"`
	Parameters []string `json:"parameters"`
}

func describedEndpoints(t *testing.T, handler http.Handler, token string) []describedEndpoint {
	t.Helper()
	recorder := call(t, handler, http.MethodGet, "/api/description", nil, token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("describing the API returned %d", recorder.Code)
	}
	var description struct {
		Endpoints []describedEndpoint `json:"endpoints"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &description); err != nil {
		t.Fatalf("decoding the description: %v", err)
	}
	return description.Endpoints
}

func capabilities(t *testing.T, handler http.Handler, token string) []string {
	t.Helper()
	recorder := call(t, handler, http.MethodGet, "/api/server", nil, token)
	var info struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &info); err != nil {
		t.Fatalf("decoding the server's description: %v (%s)", err, recorder.Body.String())
	}
	return info.Capabilities
}
