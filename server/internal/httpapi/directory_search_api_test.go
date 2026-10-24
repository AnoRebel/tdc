package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

/*
Skills, availability and finding people.

The directory's search is where a stranger meets a profile, so most of these
are about what it does not return: hidden profiles, hidden fields, and — the
subtle one — hidden fields matched by a filter, which would let a searcher
learn a hidden location by guessing it and watching who appears.
*/

// searchDirectory runs a directory search and returns the usernames found, in order.
func searchDirectory(t *testing.T, handler http.Handler, token string, query url.Values) ([]string, map[string]any) {
	t.Helper()
	recorder := call(t, handler, http.MethodGet, "/api/directory/search?"+query.Encode(), nil, token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("searching %v returned %d: %s", query, recorder.Code, recorder.Body.String())
	}
	body := decode(t, recorder)
	names := []string{}
	for _, found := range body["profiles"].([]any) {
		names = append(names, found.(map[string]any)["username"].(string))
	}
	return names, body
}

// found returns one search result by username, or nil.
func found(body map[string]any, username string) map[string]any {
	for _, profile := range body["profiles"].([]any) {
		if entry := profile.(map[string]any); entry["username"] == username {
			return entry
		}
	}
	return nil
}

// proposeSkill proposes a skill and returns its identifier and status.
func proposeSkill(t *testing.T, handler http.Handler, token, name string) (string, string, int) {
	t.Helper()
	recorder := call(t, handler, http.MethodPost, "/api/directory/skills", map[string]string{"name": name}, token)
	if recorder.Code != http.StatusCreated && recorder.Code != http.StatusOK {
		t.Fatalf("proposing %q returned %d: %s", name, recorder.Code, recorder.Body.String())
	}
	body := decode(t, recorder)
	return body["id"].(string), body["status"].(string), recorder.Code
}

// inDays is an availability expiry that many days ahead.
func inDays(days int) string {
	return time.Now().Add(time.Duration(days) * 24 * time.Hour).UTC().Format(time.RFC3339)
}

func has(names []string, name string) bool {
	for _, each := range names {
		if each == name {
			return true
		}
	}
	return false
}

/*
A skill outside the vocabulary enters review; it does not become searchable.

Its proposer can put it on their own profile, where they see it waiting, and
nobody else can find them by it or attach it themselves.
*/
func TestAnUnknownSkillEntersReview(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := registerAndSignIn(t, handler, "juma")
	amina := registerAndSignIn(t, handler, "amina")

	name := "Elixir " + uuid.NewString()[:6]
	id, status, code := proposeSkill(t, handler, juma, name)
	if status != "proposed" || code != http.StatusCreated {
		t.Fatalf("an unknown skill was %s (%d), want a new proposal", status, code)
	}

	saved := saveProfile(t, handler, juma, map[string]any{
		"visibility": "members", "skills": []map[string]any{{"id": id}},
	})
	skills := saved["profile"].(map[string]any)["skills"].([]any)
	if len(skills) != 1 || skills[0].(map[string]any)["status"] != "proposed" {
		t.Errorf("the proposer does not see their proposal waiting: %v", skills)
	}

	names, _ := searchDirectory(t, handler, amina, url.Values{"skill": {name}})
	if len(names) != 0 {
		t.Errorf("searching by a proposed skill found %v", names)
	}
	if _, view := viewProfile(t, handler, amina, "juma"); view["skills"] != nil {
		t.Errorf("another member was shown a skill still in review: %v", view["skills"])
	}

	recorder := call(t, handler, http.MethodPut, "/api/directory/me", map[string]any{
		"skills": []map[string]any{{"id": id}},
	}, amina)
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("somebody else attached a proposal that is not theirs: %d", recorder.Code)
	}
}

/*
Two spellings of one skill are one searchable entry.

"Golang" is a known other spelling of Go, so proposing it returns Go rather
than a second entry, and profiles saved with either are found by either.
*/
func TestTwoSpellingsOfASkillAreOneEntry(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := registerAndSignIn(t, handler, "juma")
	amina := registerAndSignIn(t, handler, "amina")
	client := registerAndSignIn(t, handler, "client")

	golang, status, code := proposeSkill(t, handler, juma, "Golang")
	goSkill, _, _ := proposeSkill(t, handler, amina, "go")
	if golang != goSkill || status != "active" || code != http.StatusOK {
		t.Fatalf("golang and go are different entries (%s, %s, %s %d)", golang, goSkill, status, code)
	}

	saveProfile(t, handler, juma, map[string]any{"skills": []map[string]any{{"name": "golang"}}})
	saveProfile(t, handler, amina, map[string]any{"skills": []map[string]any{{"name": "Go"}}})

	for _, spelling := range []string{"Go", "golang", "GOLANG"} {
		names, _ := searchDirectory(t, handler, client, url.Values{"skill": {spelling}})
		sort.Strings(names)
		if !reflect.DeepEqual(names, []string{"amina", "juma"}) {
			t.Errorf("searching for %q found %v, want both", spelling, names)
		}
	}
}

/*
The review queue: approving makes a skill attachable and searchable, rejecting
removes it from every profile it was on, and merging folds it into a skill the
vocabulary has. Only somebody who manages the vocabulary decides, and each
decision is recorded.
*/
func TestTheSkillReviewQueue(t *testing.T) {
	handler, founder := withDirectory(t)
	juma := registerAndSignIn(t, handler, "juma")
	amina := registerAndSignIn(t, handler, "amina")
	reviewer := registerAndSignIn(t, handler, "kheri")

	suffix := uuid.NewString()[:6]
	approved, _, _ := proposeSkill(t, handler, juma, "Svelte "+suffix)
	rejected, _, _ := proposeSkill(t, handler, juma, "Nonsense "+suffix)
	merged, _, _ := proposeSkill(t, handler, juma, "Postgre "+suffix)
	saveProfile(t, handler, juma, map[string]any{"skills": []map[string]any{
		{"id": approved}, {"id": rejected}, {"id": merged, "description": "ten years"},
	}})

	if recorder := call(t, handler, http.MethodPost, "/api/directory/moderation/skills/"+approved+"/approve", nil, amina); recorder.Code != http.StatusForbidden {
		t.Fatalf("an ordinary member approved a skill: %d", recorder.Code)
	}
	grantRole(t, handler, founder, reviewer, "directory.skills.manage")

	queue := decodeList(t, call(t, handler, http.MethodGet, "/api/directory/moderation/skills", nil, reviewer))
	if len(queue) != 3 {
		t.Fatalf("the queue holds %d proposals, want 3", len(queue))
	}

	if recorder := call(t, handler, http.MethodPost, "/api/directory/moderation/skills/"+approved+"/approve", nil, reviewer); recorder.Code != http.StatusOK {
		t.Fatalf("approving returned %d", recorder.Code)
	}
	if recorder := call(t, handler, http.MethodPost, "/api/directory/moderation/skills/"+rejected+"/reject",
		map[string]string{"reason": "not a skill"}, reviewer); recorder.Code != http.StatusNoContent {
		t.Fatalf("rejecting returned %d", recorder.Code)
	}

	var postgres string
	if err := poolForTest.QueryRow(context.Background(),
		"SELECT id FROM skills WHERE name_folded = 'postgresql'").Scan(&postgres); err != nil {
		t.Fatalf("finding PostgreSQL: %v", err)
	}
	if recorder := call(t, handler, http.MethodPost, "/api/directory/moderation/skills/"+merged+"/merge",
		map[string]string{"into": postgres}, reviewer); recorder.Code != http.StatusOK {
		t.Fatalf("merging returned %d: %s", recorder.Code, recorder.Body.String())
	}

	// The approved skill is now anybody's to attach, and finds juma.
	saveProfile(t, handler, amina, map[string]any{"skills": []map[string]any{{"id": approved}}})
	names, _ := searchDirectory(t, handler, amina, url.Values{"skill": {"Svelte " + suffix}})
	if !has(names, "juma") || !has(names, "amina") {
		t.Errorf("an approved skill does not find the profiles carrying it: %v", names)
	}

	// The rejected one is off juma's profile and finds nobody; the merged one
	// is PostgreSQL, keeping what juma wrote, and its spelling now means it.
	own := decode(t, call(t, handler, http.MethodGet, "/api/directory/me", nil, juma))
	skills := map[string]string{}
	for _, skill := range own["profile"].(map[string]any)["skills"].([]any) {
		entry := skill.(map[string]any)
		skills[entry["name"].(string)] = entry["description"].(string)
	}
	if _, present := skills["Nonsense "+suffix]; present {
		t.Error("a rejected skill is still on the profile")
	}
	if description, present := skills["PostgreSQL"]; !present || description != "ten years" {
		t.Errorf("the merged skill did not become PostgreSQL with its description kept: %v", skills)
	}
	if id, _, _ := proposeSkill(t, handler, amina, "postgre "+suffix); id != postgres {
		t.Error("the merged spelling does not resolve to the skill it was merged into")
	}

	for _, action := range []string{"directory.skill.approved", "directory.skill.rejected", "directory.skill.merged"} {
		entries := decodeAudit(t, call(t, handler, http.MethodGet, "/api/admin/audit?action="+action, nil, founder))
		if len(entries) != 1 {
			t.Errorf("%s was recorded %d times, want once", action, len(entries))
		}
	}
}

// Somebody who manages the vocabulary adds to it without a queue.
func TestAVocabularyManagerAddsASkillDirectly(t *testing.T) {
	handler, founder := withDirectory(t)
	name := "Haskell " + uuid.NewString()[:6]
	_, status, code := proposeSkill(t, handler, founder, name)
	if status != "active" || code != http.StatusCreated {
		t.Errorf("a manager's new skill is %s (%d), want active", status, code)
	}
}

/*
Availability is a statement about work, independent of presence.

Somebody with their presence hidden can be available and found as available;
and the statement has to lapse, so it cannot be made without an expiry or for
longer than six months.
*/
func TestAvailabilityIsIndependentOfPresence(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := registerAndSignIn(t, handler, "juma")
	client := registerAndSignIn(t, handler, "client")

	setPrivacy(t, handler, juma, map[string]any{"showsPresence": false})

	for description, profile := range map[string]map[string]any{
		"no expiry":       {"openToWork": true},
		"a past expiry":   {"openToWork": true, "availableUntil": inDays(-1)},
		"a year from now": {"openToWork": true, "availableUntil": inDays(365)},
	} {
		if recorder := call(t, handler, http.MethodPut, "/api/directory/me", profile, juma); recorder.Code != http.StatusBadRequest {
			t.Errorf("availability with %s was accepted: %d", description, recorder.Code)
		}
	}

	saveProfile(t, handler, juma, map[string]any{
		"openToWork": true, "availableUntil": inDays(30), "availabilityNote": "short contracts",
	})

	names, body := searchDirectory(t, handler, client, url.Values{"available": {"true"}})
	if !has(names, "juma") {
		t.Fatalf("somebody available with presence hidden is not found as available: %v", names)
	}
	if entry := found(body, "juma"); entry["availabilityNote"] != "short contracts" {
		t.Errorf("the kind of work sought is missing: %v", entry)
	}

	privacy := decode(t, call(t, handler, http.MethodGet, "/api/me/privacy", nil, juma))
	if privacy["showsPresence"] != false {
		t.Error("stating availability changed the presence setting")
	}
}

/*
Availability past its expiry is excluded from an availability filter, with
nothing about the profile changed.
*/
func TestLapsedAvailabilityIsNotAvailable(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := registerAndSignIn(t, handler, "juma")
	client := registerAndSignIn(t, handler, "client")

	saveProfile(t, handler, juma, map[string]any{"openToWork": true, "availableUntil": inDays(10)})
	jumaID := accountID(t, handler, juma)

	ctx := context.Background()
	var before time.Time
	if err := poolForTest.QueryRow(ctx,
		"UPDATE directory_profiles SET availability_expires_at = NOW() - interval '1 minute' WHERE user_id = $1 RETURNING updated_at",
		jumaID).Scan(&before); err != nil {
		t.Fatalf("letting the availability lapse: %v", err)
	}

	names, _ := searchDirectory(t, handler, client, url.Values{"available": {"true"}})
	if has(names, "juma") {
		t.Error("lapsed availability is still found as available")
	}
	names, body := searchDirectory(t, handler, client, url.Values{})
	if !has(names, "juma") {
		t.Fatal("the profile itself left the directory when its availability lapsed")
	}
	if entry := found(body, "juma"); entry["available"] != nil {
		t.Errorf("lapsed availability is still shown: %v", entry)
	}

	var stillOpen bool
	var after time.Time
	if err := poolForTest.QueryRow(ctx,
		"SELECT open_to_work, updated_at FROM directory_profiles WHERE user_id = $1", jumaID,
	).Scan(&stillOpen, &after); err != nil {
		t.Fatalf("reading the profile: %v", err)
	}
	if !stillOpen || !after.Equal(before) {
		t.Error("the profile was changed when its availability lapsed; it should simply stop counting")
	}
}

/*
Search never returns a hidden profile, a hidden field, or a match on one.

The third is the leak worth testing for: a filter on a location the viewer may
not see must not match, or the location is readable by guessing.
*/
func TestSearchRespectsVisibility(t *testing.T) {
	handler, _ := withDirectory(t)
	public := registerAndSignIn(t, handler, "public")
	members := registerAndSignIn(t, handler, "members")
	hidden := registerAndSignIn(t, handler, "hidden")
	secretive := registerAndSignIn(t, handler, "secretive")
	client := registerAndSignIn(t, handler, "client")

	common := func(visibility, details string) map[string]any {
		return map[string]any{
			"visibility": visibility, "detailsVisibility": details,
			"headline":   "Flutter developer",
			"skills":     []map[string]any{{"name": "Flutter"}},
			"openToWork": true, "availableUntil": inDays(20),
		}
	}
	saveProfile(t, handler, public, common("public", "public"))
	saveProfile(t, handler, members, common("members", "members"))
	saveProfile(t, handler, hidden, common("hidden", "hidden"))
	saveProfile(t, handler, secretive, common("members", "hidden"))
	// The account's location, as public as it can be: what limits it here is
	// the profile showing it.
	for _, token := range []string{public, members, hidden, secretive} {
		setLocation(t, handler, token, "Arusha", "public")
	}

	all, body := searchDirectory(t, handler, client, url.Values{})
	if has(all, "hidden") {
		t.Error("a hidden profile was listed")
	}
	if !has(all, "public") || !has(all, "members") || !has(all, "secretive") {
		t.Fatalf("a member's browse missed visible profiles: %v", all)
	}
	entry := found(body, "secretive")
	for _, field := range []string{"headline", "location", "skills", "available", "availabilityNote"} {
		if value, present := entry[field]; present {
			t.Errorf("a hidden %s was listed: %v", field, value)
		}
	}

	for filter, value := range map[string]string{
		"location": "Arusha", "skill": "Flutter", "available": "true", "q": "Flutter developer",
	} {
		names, _ := searchDirectory(t, handler, client, url.Values{filter: {value}})
		if has(names, "secretive") {
			t.Errorf("filtering on %s matched a profile whose %s is hidden", filter, filter)
		}
		if has(names, "hidden") {
			t.Errorf("filtering on %s found a hidden profile", filter)
		}
		if !has(names, "public") {
			t.Errorf("filtering on %s missed a profile that shows it: %v", filter, names)
		}
	}

	// A stranger sees public profiles only.
	anonymous, _ := searchDirectory(t, handler, "", url.Values{})
	if !reflect.DeepEqual(anonymous, []string{"public"}) {
		t.Errorf("a stranger's browse found %v, want only the public profile", anonymous)
	}

	// Hiding a profile takes it out of the same search at once.
	saveProfile(t, handler, public, map[string]any{"visibility": "hidden"})
	if names, _ := searchDirectory(t, handler, client, url.Values{"skill": {"Flutter"}}); has(names, "public") {
		t.Error("a profile hidden a moment ago is still found")
	}
}

/*
Member search is exactly what it was.

The directory adds a search of its own and must not widen the one that already
existed: somebody with a full public profile is found by member search with the
same four fields as somebody with none.
*/
func TestMemberSearchIsUnchangedByTheDirectory(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := registerAndSignIn(t, handler, "jumaprofile")
	registerAndSignIn(t, handler, "jumaplain")

	people := func() map[string]map[string]any {
		body := decode(t, call(t, handler, http.MethodGet, "/api/search/everything?q=juma", nil, juma))
		result := map[string]map[string]any{}
		for _, person := range body["people"].([]any) {
			entry := person.(map[string]any)
			result[entry["username"].(string)] = entry
		}
		return result
	}

	before := people()
	saveProfile(t, handler, juma, map[string]any{
		"visibility": "public", "detailsVisibility": "public", "contactVisibility": "public",
		"headline": "Backend", "contactEmail": "juma@example.com",
		"openToWork": true, "availableUntil": inDays(10),
	})
	after := people()

	if !reflect.DeepEqual(before, after) {
		t.Errorf("member search changed when a profile was created:\nbefore %v\n after %v", before, after)
	}
	keys := func(entry map[string]any) []string {
		names := []string{}
		for key := range entry {
			names = append(names, key)
		}
		sort.Strings(names)
		return names
	}
	if !reflect.DeepEqual(keys(after["jumaprofile"]), keys(after["jumaplain"])) {
		t.Errorf("somebody with a profile is described differently by member search: %v vs %v",
			keys(after["jumaprofile"]), keys(after["jumaplain"]))
	}
}

// profilesDirectly makes profiles without registering, which the
// registration limit would stop well short of a full page.
func profilesDirectly(t *testing.T, count int) {
	t.Helper()
	ctx := context.Background()
	for index := range count {
		id := uuid.New()
		name := fmt.Sprintf("bulk%02d", index)
		if _, err := poolForTest.Exec(ctx,
			"INSERT INTO users (id, username, username_folded, display_name) VALUES ($1, $2, $2, $2)", id, name); err != nil {
			t.Fatalf("making %s: %v", name, err)
		}
		if _, err := poolForTest.Exec(ctx,
			"INSERT INTO directory_profiles (user_id, visibility) VALUES ($1, 'members')", id); err != nil {
			t.Fatalf("making %s's profile: %v", name, err)
		}
	}
}

/*
One request is one page, at the server's size.

A client asking for a thousand gets twenty; paging stops at a ceiling however
far it is pushed. With the rate limit, this is what keeps the directory from
being copied out.
*/
func TestASearchIsOnePageAtMost(t *testing.T) {
	handler, _ := withDirectory(t)
	client := registerAndSignIn(t, handler, "client")
	profilesDirectly(t, 25)

	first, body := searchDirectory(t, handler, client, url.Values{"limit": {"1000"}, "pageSize": {"1000"}, "per_page": {"1000"}})
	if len(first) != 20 || body["more"] != true {
		t.Fatalf("the first page held %d with more=%v, want 20 and more", len(first), body["more"])
	}
	second, body := searchDirectory(t, handler, client, url.Values{"page": {"1"}})
	if len(second) != 5 || body["more"] != false {
		t.Errorf("the second page held %d with more=%v, want 5 and no more", len(second), body["more"])
	}
	if overlap := strings.Join(first, ","); strings.Contains(overlap, second[0]) {
		t.Errorf("the second page repeats the first: %s in %s", second[0], overlap)
	}
}

/*
Paging stops at a ceiling, even with profiles beyond it.

Five hundred and twenty profiles, pages of twenty: the twenty-fifth page is the
last one served, and says there is no more, though there is.
*/
func TestPagingStopsAtACeiling(t *testing.T) {
	handler, _ := withDirectory(t)
	client := registerAndSignIn(t, handler, "client")

	ctx := context.Background()
	if _, err := poolForTest.Exec(ctx, `
		WITH made AS (
			INSERT INTO users (id, username, username_folded, display_name)
			SELECT gen_random_uuid(), 'many' || n, 'many' || n, 'many' || n
			FROM generate_series(1, 520) AS n
			RETURNING id
		)
		INSERT INTO directory_profiles (user_id, visibility) SELECT id, 'members' FROM made`); err != nil {
		t.Fatalf("making profiles: %v", err)
	}

	last, body := searchDirectory(t, handler, client, url.Values{"page": {"24"}})
	if len(last) != 20 || body["more"] != false {
		t.Errorf("the last page served held %d with more=%v, want 20 and no more", len(last), body["more"])
	}
	if beyond, _ := searchDirectory(t, handler, client, url.Values{"page": {"25"}}); len(beyond) != 0 {
		t.Errorf("a page past the ceiling returned %d profiles", len(beyond))
	}
}

/*
Reading the directory is rate limited, per person and per address.

Searches and profile views count together, since opening profiles one by one
is the same enumeration as paging.
*/
func TestDirectoryReadsAreRateLimited(t *testing.T) {
	handler, founder := withDirectory(t)
	client := registerAndSignIn(t, handler, "client")
	juma := registerAndSignIn(t, handler, "juma")
	saveProfile(t, handler, juma, map[string]any{"visibility": "public"})

	for key, value := range map[string]string{
		"directory.reads_per_minute": "3", "directory.anonymous_reads_per_minute": "2",
	} {
		if recorder := call(t, handler, http.MethodPut, "/api/admin/settings/"+key,
			map[string]string{"value": value}, founder); recorder.Code != http.StatusNoContent {
			t.Fatalf("setting %s: %d %s", key, recorder.Code, recorder.Body.String())
		}
	}

	for index, path := range []string{"/api/directory/search", "/api/directory/profiles/juma", "/api/directory/search"} {
		if recorder := call(t, handler, http.MethodGet, path, nil, client); recorder.Code != http.StatusOK {
			t.Fatalf("read %d was refused: %d", index+1, recorder.Code)
		}
	}
	refused := call(t, handler, http.MethodGet, "/api/directory/profiles/juma", nil, client)
	if refused.Code != http.StatusTooManyRequests || refused.Header().Get("Retry-After") == "" {
		t.Errorf("the fourth read in a minute was answered %d, want 429 with Retry-After", refused.Code)
	}

	// Another person is not affected by the first one's reads.
	if recorder := call(t, handler, http.MethodGet, "/api/directory/search", nil, juma); recorder.Code != http.StatusOK {
		t.Errorf("one person's limit refused another: %d", recorder.Code)
	}

	for index := range 2 {
		if recorder := call(t, handler, http.MethodGet, "/api/directory/search", nil, ""); recorder.Code != http.StatusOK {
			t.Fatalf("anonymous read %d was refused: %d", index+1, recorder.Code)
		}
	}
	if recorder := call(t, handler, http.MethodGet, "/api/directory/search", nil, ""); recorder.Code != http.StatusTooManyRequests {
		t.Errorf("the third anonymous read was answered %d, want 429", recorder.Code)
	}
}

// Company and location filters find the people they name.
func TestSearchByCompanyAndLocation(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := registerAndSignIn(t, handler, "juma")
	amina := registerAndSignIn(t, handler, "amina")
	client := registerAndSignIn(t, handler, "client")

	saveProfile(t, handler, juma, map[string]any{})
	saveProfile(t, handler, amina, map[string]any{})
	setLocation(t, handler, juma, "Mwanza", "members")
	setLocation(t, handler, amina, "Dodoma", "members")
	call(t, handler, http.MethodPost, "/api/directory/companies", map[string]any{"slug": "ziwa", "name": "Ziwa Tech"}, juma)

	if names, _ := searchDirectory(t, handler, client, url.Values{"location": {"mwanza"}}); !reflect.DeepEqual(names, []string{"juma"}) {
		t.Errorf("searching by location found %v", names)
	}
	for _, company := range []string{"ziwa", "Ziwa Tech", "tech"} {
		if names, _ := searchDirectory(t, handler, client, url.Values{"company": {company}}); !reflect.DeepEqual(names, []string{"juma"}) {
			t.Errorf("searching by company %q found %v", company, names)
		}
	}
}
