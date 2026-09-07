package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AnoRebel/tdc/server/internal/attachments"
	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/dbtest"
	"github.com/AnoRebel/tdc/server/internal/moderation"
)

/*
TestNothingExpiresWithoutAPolicy is what makes retention safe to ship switched
on.

A server nobody has configured deletes nothing. Zero means keep, and it is the
default for both the server and every channel — so the feature exists without
anybody having to discover a switch to turn it off.
*/
func TestNothingExpiresWithoutAPolicy(t *testing.T) {
	handler, services := newTestAPIWithServices(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	sendMessage(t, handler, owner, roomID, "Habari za asubuhi")

	// No policy anywhere, and everything is old enough that any policy would
	// catch it.
	ageMessages(t, roomID, "400 days")

	removed, err := services.Moderation.ExpireDue(context.Background(), 0, 100, nil)
	if err != nil {
		t.Fatalf("ExpireDue: %v", err)
	}
	if removed != 0 {
		t.Errorf("%d messages were removed with no policy configured", removed)
	}
}

/*
TestContentIsRemovedAtExpiry is the scenario the requirement names.

A policy that discloses a period and then keeps everything anyway is worse than
no policy: people were told their messages would go.
*/
func TestContentIsRemovedAtExpiry(t *testing.T) {
	handler, services := newTestAPIWithServices(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	sendMessage(t, handler, owner, roomID, "Ujumbe wa zamani")
	sendMessage(t, handler, owner, roomID, "Ujumbe mpya")

	// The first is old, the second is not.
	ageMessages(t, roomID, "10 days")
	sendMessage(t, handler, owner, roomID, "Ujumbe wa leo")

	// A week's retention, server wide.
	removed, err := services.Moderation.ExpireDue(context.Background(), 7*24*3600, 100, nil)
	if err != nil {
		t.Fatalf("ExpireDue: %v", err)
	}
	if removed == 0 {
		t.Fatal("nothing was removed although content was past the period")
	}

	timeline := decodeTimeline(t, call(t, handler, http.MethodGet,
		"/api/rooms/"+roomID+"/timeline?limit=50", nil, owner))

	for _, event := range timeline {
		body := bodyOfEvent(event)
		if strings.Contains(body, "Ujumbe wa zamani") || strings.Contains(body, "Ujumbe mpya") {
			t.Errorf("content past the retention period is still readable: %q", body)
		}
	}

	// And what is inside the period stays.
	kept := false
	for _, event := range timeline {
		if strings.Contains(bodyOfEvent(event), "Ujumbe wa leo") {
			kept = true
		}
	}
	if !kept {
		t.Error("a message inside the retention period was removed")
	}
}

/*
TestAChannelsOwnPeriodOverridesTheServers covers the per-channel half.

A channel for something short-lived should be able to keep less than the
server, without the operator changing the server's policy for everybody.
*/
func TestAChannelsOwnPeriodOverridesTheServers(t *testing.T) {
	handler, services := newTestAPIWithServices(t)

	owner := completeSetup(t, handler)

	// A channel keeping one day, on a server keeping a year.
	shortLived := makeRoomWithTTL(t, handler, owner, "matukio", 24*3600)
	ordinary := makeRoom(t, handler, owner, "general", "public")

	sendMessage(t, handler, owner, shortLived, "Litapotea")
	sendMessage(t, handler, owner, ordinary, "Litabaki")

	ageMessages(t, shortLived, "2 days")
	ageMessages(t, ordinary, "2 days")

	if _, err := services.Moderation.ExpireDue(
		context.Background(), 365*24*3600, 100, nil); err != nil {
		t.Fatalf("ExpireDue: %v", err)
	}

	shortTimeline := decodeTimeline(t, call(t, handler, http.MethodGet,
		"/api/rooms/"+shortLived+"/timeline?limit=50", nil, owner))
	for _, event := range shortTimeline {
		if strings.Contains(bodyOfEvent(event), "Litapotea") {
			t.Error("the channel's own shorter period was not applied")
		}
	}

	ordinaryTimeline := decodeTimeline(t, call(t, handler, http.MethodGet,
		"/api/rooms/"+ordinary+"/timeline?limit=50", nil, owner))
	kept := false
	for _, event := range ordinaryTimeline {
		if strings.Contains(bodyOfEvent(event), "Litabaki") {
			kept = true
		}
	}
	if !kept {
		t.Error("a channel with no period of its own lost content inside the server's")
	}
}

/*
TestThePolicyIsDisclosedToMembers is the scenario that makes retention
honest.

A conversation that quietly deletes itself is one people say things in they
would not have said knowing.
*/
func TestThePolicyIsDisclosedToMembers(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoomWithTTL(t, handler, owner, "matukio", 24*3600)

	member := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, member, roomID)

	// Read by an ordinary member, not only the operator who set it.
	room := decode(t, call(t, handler, http.MethodGet, "/api/rooms/"+roomID, nil, member))

	seconds, _ := room["retentionSeconds"].(float64)
	if int(seconds) != 24*3600 {
		t.Errorf("the channel reports retentionSeconds %v, expected 86400", room["retentionSeconds"])
	}
	if fromChannel, _ := room["retentionFromChannel"].(bool); !fromChannel {
		t.Error("the channel does not say the period is its own rather than the server's")
	}
}

// TestTheServersPolicyIsDisclosedToo covers a channel inheriting rather than
// setting its own.
func TestTheServersPolicyIsDisclosedToo(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	setSetting(t, handler, owner, "moderation.retention_seconds", "604800")

	room := decode(t, call(t, handler, http.MethodGet, "/api/rooms/"+roomID, nil, owner))

	seconds, _ := room["retentionSeconds"].(float64)
	if int(seconds) != 604800 {
		t.Errorf("the channel reports retentionSeconds %v, expected the server's 604800",
			room["retentionSeconds"])
	}
	if fromChannel, _ := room["retentionFromChannel"].(bool); fromChannel {
		t.Error("an inherited period is reported as the channel's own")
	}
}

/*
TestShorteningRetentionIsRecordedAsItsOwnAction is the scenario about
destruction rather than configuration.

Somebody asking why a year of messages vanished should find this entry, not
have to infer it from two numbers in a generic setting change.
*/
func TestShorteningRetentionIsRecordedAsItsOwnAction(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	// A year, then a day: existing content becomes eligible.
	setSetting(t, handler, owner, "moderation.retention_seconds", "31536000")
	setSetting(t, handler, owner, "moderation.retention_seconds", "86400")

	entries := decodeAudit(t, call(t, handler, http.MethodGet,
		"/api/admin/audit?limit=20", nil, owner))

	/*
		Both changes shorten: none to a year makes everything eligible, and a
		year to a day makes more of it eligible. So this looks for the one it
		is about rather than asserting on whichever it meets first — the
		earlier draft did the latter and failed against a correct log.
	*/
	found := false
	for _, entry := range entries {
		if entry.Action != "server.retention.shortened" {
			continue
		}

		var detail struct{ From, To string }
		if err := json.Unmarshal([]byte(entry.Detail), &detail); err != nil {
			t.Fatalf("decoding the detail: %v", err)
		}
		if detail.From == "31536000" && detail.To == "86400" {
			found = true
		}
	}

	if !found {
		t.Error("shortening retention was recorded as an ordinary setting change, " +
			"indistinguishable from lengthening it")
	}
}

/*
TestLengtheningRetentionIsNotRecordedAsShortening keeps the distinction
meaningful.

An action name that appeared on every change would tell a reader nothing.
*/
func TestLengtheningRetentionIsNotRecordedAsShortening(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	setSetting(t, handler, owner, "moderation.retention_seconds", "86400")
	setSetting(t, handler, owner, "moderation.retention_seconds", "31536000")

	entries := decodeAudit(t, call(t, handler, http.MethodGet,
		"/api/admin/audit?limit=20", nil, owner))

	// The first change — from none to a day — genuinely shortens, so exactly
	// one entry is expected rather than none.
	shortenings := 0
	for _, entry := range entries {
		if entry.Action == "server.retention.shortened" {
			shortenings++
		}
	}
	// One: the change from no policy to a day. The lengthening after it is
	// not one.
	if shortenings != 1 {
		t.Errorf("%d shortenings recorded across a shortening and a lengthening, "+
			"expected 1", shortenings)
	}
}

/*
TestTurningRetentionOffIsNotShortening covers the case a numeric comparison
gets backwards.

Zero means keep everything. Moving to it removes nothing, however small the
number before it was.
*/
func TestTurningRetentionOffIsNotShortening(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	setSetting(t, handler, owner, "moderation.retention_seconds", "86400")
	setSetting(t, handler, owner, "moderation.retention_seconds", "0")

	entries := decodeAudit(t, call(t, handler, http.MethodGet,
		"/api/admin/audit?limit=20", nil, owner))

	shortenings := 0
	for _, entry := range entries {
		if entry.Action == "server.retention.shortened" {
			shortenings++
		}
	}
	// Only the first change, which introduced a period where there was none.
	if shortenings != 1 {
		t.Errorf("%d shortenings recorded; turning retention off should not be one",
			shortenings)
	}
}

// TestPolicyForPrefersTheChannel covers the resolution rule directly, since
// the API tests reach it only through a timeline.
func TestPolicyForPrefersTheChannel(t *testing.T) {
	day := int32(86400)
	zero := int32(0)

	if policy := moderation.PolicyFor(&day, 604800); policy.Seconds != day || !policy.FromChannel {
		t.Errorf("a channel period gave %+v, expected the channel's own", policy)
	}
	if policy := moderation.PolicyFor(nil, 604800); policy.Seconds != 604800 || policy.FromChannel {
		t.Errorf("no channel period gave %+v, expected the server's", policy)
	}
	if policy := moderation.PolicyFor(&zero, 604800); policy.FromChannel {
		t.Errorf("a channel period of zero gave %+v, expected the server's", policy)
	}
	if policy := moderation.PolicyFor(nil, 0); !policy.Keeps() {
		t.Error("no policy anywhere should keep everything")
	}
}

/*
newTestAPIWithServices builds a handler and a moderation service over the same
database.

Retention runs in a background worker in production, so a test has to drive it
directly. The service holds nothing but the pool and a logger, so a second
instance over the same database behaves identically to the one the handler
uses — which is what makes this equivalent to reaching inside the harness
rather than a fixture that agrees with itself.
*/
func newTestAPIWithServices(t *testing.T) (http.Handler, testServices) {
	t.Helper()

	handler := newTestAPI(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pool, err := db.Open(ctx, db.Config{URL: dbtest.URL(t, "httpapi")}, logger)
	if err != nil {
		t.Fatalf("opening the test database: %v", err)
	}
	t.Cleanup(pool.Close)

	return handler, testServices{
		Moderation: moderation.NewService(pool, logger),
		// The same store the handler uses, so a test can check that expiring a
		// message really removed its files rather than only unlinking them.
		Attachments: attachments.NewService(
			pool, testStore(t), attachments.DefaultLimits(), logger),
		pool: pool,
	}
}

type testServices struct {
	Moderation  *moderation.Service
	Attachments *attachments.Service
	pool        *db.Pool
}

/*
ageMessages backdates every message in a room.

Retention is measured against how old something is, and a test cannot wait a
week. Moving the clock backwards on the rows is the honest alternative to
making the period configurable down to seconds and sleeping — which would test
a code path production never takes.
*/
func ageMessages(t *testing.T, roomID, interval string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pool, err := db.Open(ctx, db.Config{URL: dbtest.URL(t, "httpapi")}, logger)
	if err != nil {
		t.Fatalf("opening the test database: %v", err)
	}
	defer pool.Close()

	// created_at is milliseconds since the epoch, so the interval is converted
	// rather than subtracted as a timestamp.
	if _, err := pool.Exec(ctx, `
		UPDATE events
		SET created_at = created_at - (EXTRACT(EPOCH FROM $2::interval) * 1000)::bigint
		WHERE room_id = $1`, roomID, interval); err != nil {
		t.Fatalf("ageing messages: %v", err)
	}
}

// makeRoomWithTTL creates a channel that keeps messages for a fixed period.
func makeRoomWithTTL(t *testing.T, handler http.Handler, token, slug string, seconds int) string {
	t.Helper()

	recorder := call(t, handler, http.MethodPost, "/api/rooms", map[string]any{
		"slug": slug, "name": slug, "visibility": "public",
		"messageTtlSeconds": seconds,
	}, token)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("creating a room with a retention period returned %d: %s",
			recorder.Code, truncate(recorder.Body.String()))
	}

	var room struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &room); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	return room.ID
}

// auditEntry is one row of the audit log as the API returns it.
type auditEntry struct {
	Action string `json:"action"`
	Detail string `json:"detail"`
}

func decodeAudit(t *testing.T, recorder *httptest.ResponseRecorder) []auditEntry {
	t.Helper()

	if recorder.Code != http.StatusOK {
		t.Fatalf("reading the audit log returned %d: %s",
			recorder.Code, truncate(recorder.Body.String()))
	}

	var entries []auditEntry
	if err := json.Unmarshal(recorder.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decoding the audit log: %v", err)
	}
	return entries
}
