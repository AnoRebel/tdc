package httpapi_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

/*
Where somebody is, and who may know.

The location is one setting on the account and appears wherever the person
does. These tests ask every place that returns a person — the member list, the
directory profile, directory search and its location filter — as viewers of
each rank, and check the place name is absent from the whole response body
wherever the viewer is not entitled to it, not merely from the field.
*/

// setLocation sets somebody's own location by hand.
func setLocation(t *testing.T, handler http.Handler, token, place, visibility string) map[string]any {
	t.Helper()
	recorder := call(t, handler, http.MethodPut, "/api/me/location",
		map[string]any{"mode": "manual", "place": place, "visibility": visibility}, token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("setting a location returned %d: %s", recorder.Code, recorder.Body.String())
	}
	return decode(t, recorder)
}

func TestALocationStartsHidden(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := registerAndSignIn(t, handler, "juma")

	recorder := call(t, handler, http.MethodGet, "/api/me/location", nil, juma)
	if recorder.Code != http.StatusOK {
		t.Fatalf("reading a location returned %d", recorder.Code)
	}
	got := decode(t, recorder)
	if got["visibility"] != "hidden" || got["mode"] != "manual" || got["place"] != "" {
		t.Errorf("a new account's location is %v; want empty, manual and hidden", got)
	}

	// Saved without saying who may see it, it is still hidden.
	recorder = call(t, handler, http.MethodPut, "/api/me/location", map[string]any{"place": "Moshi"}, juma)
	if got := decode(t, recorder); got["visibility"] != "hidden" {
		t.Errorf("a location saved with no visibility became %v", got["visibility"])
	}
}

/*
A hidden or members-only location never reaches somebody not entitled to it.

Checked against the raw body of every response that returns the person, for
each of the three settings, as a stranger, a member and — for the directory —
through a profile whose details are narrower than the location itself.
*/
func TestALocationReachesOnlyThoseEntitled(t *testing.T) {
	handler, founder := withDirectory(t)
	juma := registerAndSignIn(t, handler, "juma")
	amina := registerAndSignIn(t, handler, "amina")
	roomID := makeRoom(t, handler, founder, "general", "public")
	joinRoom(t, handler, juma, roomID)
	joinRoom(t, handler, amina, roomID)
	// Enough reads that a refusal cannot pass for a withheld field.
	for _, key := range []string{"directory.reads_per_minute", "directory.anonymous_reads_per_minute"} {
		if recorder := call(t, handler, http.MethodPut, "/api/admin/settings/"+key,
			map[string]string{"value": "1000"}, founder); recorder.Code != http.StatusNoContent {
			t.Fatalf("setting %s: %d", key, recorder.Code)
		}
	}

	// Everything about the profile is public, so the location's own setting
	// is the only thing deciding.
	saveProfile(t, handler, juma, map[string]any{"visibility": "public", "detailsVisibility": "public"})

	const place = "Bagamoyo"
	contains := func(recorder interface{ String() string }) bool {
		return strings.Contains(recorder.String(), place)
	}
	members := func(token string) bool {
		recorder := call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/members", nil, token)
		if recorder.Code != http.StatusOK {
			t.Fatalf("member list returned %d", recorder.Code)
		}
		return contains(recorder.Body)
	}
	profile := func(token string) bool {
		recorder := call(t, handler, http.MethodGet, "/api/directory/profiles/juma", nil, token)
		if recorder.Code != http.StatusOK {
			t.Fatalf("profile returned %d", recorder.Code)
		}
		return contains(recorder.Body)
	}
	search := func(token string, filter url.Values) (listed, matched bool) {
		recorder := call(t, handler, http.MethodGet, "/api/directory/search", nil, token)
		if recorder.Code != http.StatusOK {
			t.Fatalf("search returned %d", recorder.Code)
		}
		listed = contains(recorder.Body)
		recorder = call(t, handler, http.MethodGet, "/api/directory/search?"+filter.Encode(), nil, token)
		if recorder.Code != http.StatusOK {
			t.Fatalf("search returned %d", recorder.Code)
		}
		matched = strings.Contains(recorder.Body.String(), `"juma"`)
		return listed, matched
	}
	byPlace := url.Values{"location": {"bagamoyo"}}

	for _, tc := range []struct {
		visibility       string
		member, stranger bool
	}{
		{"public", true, true},
		{"members", true, false},
		{"hidden", false, false},
	} {
		setLocation(t, handler, juma, place, tc.visibility)

		if got := members(amina); got != tc.member {
			t.Errorf("%s: the member list showed a member the location: %v", tc.visibility, got)
		}
		if got := profile(amina); got != tc.member {
			t.Errorf("%s: the profile showed a member the location: %v", tc.visibility, got)
		}
		if got := profile(""); got != tc.stranger {
			t.Errorf("%s: the profile showed a stranger the location: %v", tc.visibility, got)
		}
		if listed, matched := search(amina, byPlace); listed != tc.member || matched != tc.member {
			t.Errorf("%s: search as a member listed %v, matched %v", tc.visibility, listed, matched)
		}
		if listed, matched := search("", byPlace); listed != tc.stranger || matched != tc.stranger {
			t.Errorf("%s: search as a stranger listed %v, matched %v; a hidden place must not be guessable",
				tc.visibility, listed, matched)
		}
	}

	// A public location in a profile whose details only members see is a
	// members-only location there: the profile is its ceiling.
	saveProfile(t, handler, juma, map[string]any{"visibility": "public", "detailsVisibility": "members"})
	setLocation(t, handler, juma, place, "public")
	if profile("") {
		t.Error("a stranger saw a location through a profile whose details are members-only")
	}
	if listed, matched := search("", byPlace); listed || matched {
		t.Error("a stranger's search reached a location the profile shows only to members")
	}
	if !profile(amina) {
		t.Error("a member could not see a location the profile shows to members")
	}
}

/*
Detecting keeps the town and nothing else.

The answer names the town nearest the position, puts the setting in automatic
mode and leaves its visibility alone. The position is nowhere in the response
or the stored row.
*/
func TestDetectingKeepsOnlyTheTown(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := registerAndSignIn(t, handler, "juma")
	setLocation(t, handler, juma, "Somewhere", "members")

	recorder := call(t, handler, http.MethodPost, "/api/me/location/detect",
		map[string]any{"latitude": -1.28333, "longitude": 36.81667}, juma)
	if recorder.Code != http.StatusOK {
		t.Fatalf("detecting returned %d: %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	got := decode(t, recorder)
	if got["place"] != "Nairobi, Kenya" || got["mode"] != "automatic" || got["visibility"] != "members" {
		t.Errorf("detecting gave %v", got)
	}
	if got["detectedAt"] == nil {
		t.Error("detecting did not say when")
	}
	if strings.Contains(body, "36.8") || strings.Contains(body, "1.28") {
		t.Errorf("the response repeats the position: %s", body)
	}

	var row string
	if err := poolForTest.QueryRow(context.Background(),
		`SELECT row_to_json(l)::text FROM user_locations l
		 JOIN users u ON u.id = l.user_id WHERE u.username = 'juma'`).Scan(&row); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(row, "36.8") || strings.Contains(row, "1.28") {
		t.Errorf("the stored location holds the position: %s", row)
	}

	// In automatic mode a typed place is not taken as detected.
	recorder = call(t, handler, http.MethodPut, "/api/me/location",
		map[string]any{"mode": "automatic", "place": "Paris", "visibility": "public"}, juma)
	if got := decode(t, recorder); got["place"] != "Nairobi, Kenya" || got["visibility"] != "public" {
		t.Errorf("an automatic save took the typed place: %v", got)
	}

	// Switching to manual keeps what was detected as the starting text.
	recorder = call(t, handler, http.MethodPut, "/api/me/location",
		map[string]any{"mode": "manual", "place": "Nairobi, Kenya", "visibility": "public"}, juma)
	if got := decode(t, recorder); got["mode"] != "manual" || got["detectedAt"] == nil {
		t.Errorf("switching to manual gave %v", got)
	}
}

func TestDetectingRefusesWhatIsNotAPlace(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := registerAndSignIn(t, handler, "juma")
	setLocation(t, handler, juma, "Moshi", "members")

	for _, tc := range []struct {
		body map[string]any
		want int
	}{
		{map[string]any{"latitude": 91, "longitude": 0}, http.StatusBadRequest},
		{map[string]any{"latitude": 1}, http.StatusBadRequest},
		// The middle of the Pacific has no town to name.
		{map[string]any{"latitude": -30, "longitude": -140}, http.StatusNotFound},
	} {
		recorder := call(t, handler, http.MethodPost, "/api/me/location/detect", tc.body, juma)
		if recorder.Code != tc.want {
			t.Errorf("%v answered %d, want %d", tc.body, recorder.Code, tc.want)
		}
	}
	if got := decode(t, call(t, handler, http.MethodGet, "/api/me/location", nil, juma)); got["place"] != "Moshi" || got["mode"] != "manual" {
		t.Errorf("a refused detection changed the setting: %v", got)
	}

	recorder := call(t, handler, http.MethodPut, "/api/me/location",
		map[string]any{"mode": "manual", "place": strings.Repeat("a", 81)}, juma)
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("an 81-character place answered %d", recorder.Code)
	}
}
