package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

/*
withDigestMail builds a server that sends mail and has an address to put in
links, which is what a summary needs, and returns where its mail goes.
*/
func withDigestMail(t *testing.T) (http.Handler, *testPostbox) {
	t.Helper()
	return withMail(t)
}

type digestSetting struct {
	Frequency        string     `json:"frequency"`
	Hour             int        `json:"hour"`
	Weekday          int        `json:"weekday"`
	TimeZone         string     `json:"timeZone"`
	NextAt           *time.Time `json:"nextAt"`
	LastSentAt       *time.Time `json:"lastSentAt"`
	AddressConfirmed bool       `json:"addressConfirmed"`
}

func chooseDigest(t *testing.T, handler http.Handler, token string, body map[string]any) digestSetting {
	t.Helper()
	recorder := call(t, handler, http.MethodPut, "/api/me/preferences/digest", body, token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("choosing a summary: status %d, body %s", recorder.Code, recorder.Body)
	}
	var setting digestSetting
	if err := json.Unmarshal(recorder.Body.Bytes(), &setting); err != nil {
		t.Fatalf("decoding the summary setting: %v", err)
	}
	return setting
}

// makeDigestDue moves somebody's next summary into the past, so the next pass sends
// it. A test cannot wait until eight tomorrow morning.
func makeDigestDue(t *testing.T, username string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tag, err := poolForTest.Exec(ctx, `
		UPDATE notification_digests SET next_due_at = now() - interval '1 minute'
		WHERE user_id = (SELECT id FROM users WHERE username = $1)`, username)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("making %s's summary due: %v (%d rows)", username, err, tag.RowsAffected())
	}
}

// summariesTo is what has been mailed to one address.
func summariesTo(post *testPostbox, address string) []string {
	var bodies []string
	for _, message := range post.delivered() {
		if message.To == address && strings.Contains(message.Subject, "waiting for you") {
			bodies = append(bodies, message.Body)
		}
	}
	return bodies
}

/*
A server that cannot send mail does not offer summaries: not advertised, and
both routes answer as a disabled feature does. Offering one would promise
something nobody can deliver.
*/
func TestDigestsAreNotOfferedWithoutMail(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	if lists(advertised(t, handler), "notification-digests") {
		t.Error("summaries are advertised by a server that sends no mail")
	}
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		recorder := call(t, handler, method, "/api/me/preferences/digest",
			map[string]any{"frequency": "daily", "hour": 8, "timeZone": "UTC"}, owner)
		if recorder.Code != http.StatusNotFound {
			t.Errorf("%s on a server without mail: status %d, want 404", method, recorder.Code)
		}
	}
}

// With mail they are offered, and off for everybody until they choose.
func TestDigestsAreOfferedWithMailAndOffByDefault(t *testing.T) {
	handler, _ := withDigestMail(t)
	owner := completeSetup(t, handler)

	if !lists(advertised(t, handler), "notification-digests") {
		t.Fatal("summaries are not advertised by a server that sends mail")
	}

	recorder := call(t, handler, http.MethodGet, "/api/me/preferences/digest", nil, owner)
	if recorder.Code != http.StatusOK {
		t.Fatalf("reading the summary setting: status %d", recorder.Code)
	}
	var setting digestSetting
	if err := json.Unmarshal(recorder.Body.Bytes(), &setting); err != nil {
		t.Fatal(err)
	}
	if setting.Frequency != "off" || setting.NextAt != nil {
		t.Errorf("somebody who never chose has %+v, want off with nothing scheduled", setting)
	}

	// And the operator's toggle withdraws them, mail or no mail.
	setFeature(t, handler, owner, "notification-digests", false)
	if lists(advertised(t, handler), "notification-digests") {
		t.Error("summaries are still advertised after the operator switched them off")
	}
	if code := call(t, handler, http.MethodGet, "/api/me/preferences/digest", nil, owner).Code; code != http.StatusNotFound {
		t.Errorf("reading the setting with summaries switched off: status %d, want 404", code)
	}
}

/*
A summary goes only to an address somebody has proved — the rule recovery
follows, for the same reason: an address only typed into a form may be
somebody else's.
*/
func TestADigestNeedsAConfirmedAddress(t *testing.T) {
	handler, _ := withDigestMail(t)
	completeSetup(t, handler)

	if recorder := call(t, handler, http.MethodPost, "/api/auth/register", map[string]string{
		"username": "juma", "email": "juma@example.test", "password": testPassword,
	}, ""); recorder.Code != http.StatusCreated {
		t.Fatalf("register: %d", recorder.Code)
	}
	juma := signIn(t, handler, "juma", testPassword)

	refused := call(t, handler, http.MethodPut, "/api/me/preferences/digest",
		map[string]any{"frequency": "daily", "hour": 8, "timeZone": "Africa/Dar_es_Salaam"}, juma)
	if refused.Code != http.StatusConflict {
		t.Fatalf("choosing a summary with an unconfirmed address: status %d, want 409", refused.Code)
	}

	// Turning it off needs nothing proved.
	chooseDigest(t, handler, juma, map[string]any{"frequency": "off"})
}

// A schedule the server could not keep is refused rather than stored.
func TestAnImpossibleDigestScheduleIsRefused(t *testing.T) {
	handler, _ := withDigestMail(t)
	completeSetup(t, handler)
	registerVerified(t, handler, "amina", "amina@example.test")
	amina := signIn(t, handler, "amina", testPassword)

	for name, body := range map[string]map[string]any{
		"an unknown zone":     {"frequency": "daily", "hour": 8, "timeZone": "Mars/Olympus_Mons"},
		"the server's zone":   {"frequency": "daily", "hour": 8, "timeZone": "Local"},
		"no zone":             {"frequency": "daily", "hour": 8, "timeZone": ""},
		"an hour past 23":     {"frequency": "daily", "hour": 24, "timeZone": "UTC"},
		"a day past Saturday": {"frequency": "weekly", "hour": 8, "weekday": 7, "timeZone": "UTC"},
		"hourly":              {"frequency": "hourly", "hour": 8, "timeZone": "UTC"},
	} {
		if code := call(t, handler, http.MethodPut, "/api/me/preferences/digest", body, amina).Code; code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, code)
		}
	}
}

/*
The hour somebody chose is in their zone. Eight in the morning in Dar es Salaam
is five in the morning UTC, and a schedule built by setting the hour on a UTC
date would send it three hours late.
*/
func TestTheNextDigestIsAtTheChosenHourInTheChosenZone(t *testing.T) {
	handler, _ := withDigestMail(t)
	completeSetup(t, handler)
	registerVerified(t, handler, "amina", "amina@example.test")
	amina := signIn(t, handler, "amina", testPassword)

	setting := chooseDigest(t, handler, amina, map[string]any{
		"frequency": "daily", "hour": 8, "timeZone": "Africa/Dar_es_Salaam",
	})
	if setting.NextAt == nil {
		t.Fatal("a daily summary has nothing scheduled")
	}
	if utc := setting.NextAt.UTC(); utc.Hour() != 5 || utc.Minute() != 0 {
		t.Errorf("the next summary is at %s UTC, want 05:00 (08:00 in Dar es Salaam)", utc.Format(time.RFC3339))
	}
	if until := time.Until(*setting.NextAt); until <= 0 || until > 24*time.Hour {
		t.Errorf("the next summary is %s away, want within the next day", until)
	}
}

/*
The summary replaces individual notifications: what somebody missed arrives as
one mail at the interval they chose, counting what went unread per
conversation, and is not sent again on the next pass.
*/
func TestADigestSummarisesWhatWentUnread(t *testing.T) {
	handler, post := withDigestMail(t)
	owner := completeSetup(t, handler)
	general := makeRoom(t, handler, owner, "general", "public")

	registerVerified(t, handler, "amina", "amina@example.test")
	amina := signIn(t, handler, "amina", testPassword)
	joinRoom(t, handler, amina, general)

	chooseDigest(t, handler, amina, map[string]any{"frequency": "daily", "hour": 8, "timeZone": "UTC"})

	secret := "the launch date is the fourteenth"
	for range 3 {
		sendMessage(t, handler, owner, general, "@amina "+secret)
	}

	server := serverBehind(t, handler)
	ctx := context.Background()

	// Not due yet: nothing goes.
	if sent := server.DeliverDigests(ctx); sent != 0 {
		t.Fatalf("a pass before the summary was due sent %d", sent)
	}

	makeDigestDue(t, "amina")
	if sent := server.DeliverDigests(ctx); sent != 1 {
		t.Fatalf("a pass with one summary due sent %d", sent)
	}

	bodies := summariesTo(post, "amina@example.test")
	if len(bodies) != 1 {
		t.Fatalf("amina received %d summaries, want 1", len(bodies))
	}
	if !strings.Contains(bodies[0], "general: 3 notifications") {
		t.Errorf("the summary does not count the three mentions in general:\n%s", bodies[0])
	}
	if strings.Contains(bodies[0], secret) {
		t.Error("the summary carries what was said, which would put a private message in a mail provider's hands")
	}
	if !strings.Contains(bodies[0], "https://chat.example.test/rooms/"+general) {
		t.Errorf("the summary does not link to the conversation:\n%s", bodies[0])
	}

	// The next pass sends nothing: it is scheduled for tomorrow, and what was
	// summarised is not summarised again.
	if sent := server.DeliverDigests(ctx); sent != 0 {
		t.Errorf("a second pass sent %d more", sent)
	}
	makeDigestDue(t, "amina")
	if sent := server.DeliverDigests(ctx); sent != 0 {
		t.Errorf("a pass with nothing new to say sent %d", sent)
	}

	setting := chooseDigest(t, handler, amina, map[string]any{"frequency": "daily", "hour": 8, "timeZone": "UTC"})
	if setting.LastSentAt == nil {
		t.Error("the setting does not record when the summary went")
	}
}

/*
Only what the person may still read, as of sending. Each notification was
raised under the permissions of its moment; between then and the summary,
somebody may leave a channel, have channel.read withdrawn there, or mute it —
and the summary must say nothing about any of those, not even a count.
*/
func TestADigestLeavesOutWhatCanNoLongerBeRead(t *testing.T) {
	handler, post := withDigestMail(t)
	owner := completeSetup(t, handler)

	kept := makeRoom(t, handler, owner, "kept", "public")
	left := makeRoom(t, handler, owner, "left-behind", "public")
	closed := makeRoom(t, handler, owner, "closed-off", "public")
	muted := makeRoom(t, handler, owner, "muted-one", "public")

	registerVerified(t, handler, "amina", "amina@example.test")
	amina := signIn(t, handler, "amina", testPassword)
	for _, room := range []string{kept, left, closed, muted} {
		joinRoom(t, handler, amina, room)
	}

	chooseDigest(t, handler, amina, map[string]any{"frequency": "weekly", "weekday": 1, "hour": 9, "timeZone": "UTC"})

	for _, room := range []string{kept, left, closed, muted} {
		sendMessage(t, handler, owner, room, "@amina habari")
	}

	// After the notifications were raised, three ways of losing a channel.
	if code := call(t, handler, http.MethodPost, "/api/rooms/"+left+"/leave", nil, amina).Code; code >= 300 {
		t.Fatalf("leaving: status %d", code)
	}
	withdraw(t, handler, owner, "channel.read", closed)
	if code := call(t, handler, http.MethodPut, "/api/me/preferences/notifications",
		map[string]any{"level": "mentions", "muted": true, "room": muted}, amina).Code; code != http.StatusNoContent {
		t.Fatalf("muting: status %d", code)
	}

	makeDigestDue(t, "amina")
	if sent := serverBehind(t, handler).DeliverDigests(context.Background()); sent != 1 {
		t.Fatalf("sent %d summaries, want 1", sent)
	}

	bodies := summariesTo(post, "amina@example.test")
	if len(bodies) != 1 {
		t.Fatalf("amina received %d summaries", len(bodies))
	}
	if !strings.Contains(bodies[0], "kept: 1 notification") {
		t.Errorf("the summary leaves out the channel she can still read:\n%s", bodies[0])
	}
	for _, name := range []string{"left-behind", "closed-off", "muted-one"} {
		if strings.Contains(bodies[0], name) {
			t.Errorf("the summary mentions %s, which she can no longer read or has muted:\n%s", name, bodies[0])
		}
	}
}

/*
Nothing the person may read means no mail at all, rather than one announcing
that there is nothing — and rather than one counting what they cannot see.
*/
func TestNoDigestIsSentWhenNothingReadableWentUnread(t *testing.T) {
	handler, post := withDigestMail(t)
	owner := completeSetup(t, handler)
	room := makeRoom(t, handler, owner, "general", "public")

	registerVerified(t, handler, "amina", "amina@example.test")
	amina := signIn(t, handler, "amina", testPassword)
	joinRoom(t, handler, amina, room)
	chooseDigest(t, handler, amina, map[string]any{"frequency": "daily", "hour": 8, "timeZone": "UTC"})

	sendMessage(t, handler, owner, room, "@amina habari")
	withdraw(t, handler, owner, "channel.read", room)

	makeDigestDue(t, "amina")
	if sent := serverBehind(t, handler).DeliverDigests(context.Background()); sent != 0 {
		t.Errorf("sent %d summaries about a channel she can no longer read", sent)
	}
	if bodies := summariesTo(post, "amina@example.test"); len(bodies) != 0 {
		t.Errorf("mailed: %v", bodies)
	}
}

/*
A status that says not to interrupt holds the summary, the same as it holds a
notification. Held rather than dropped: what it would have said goes out once
the status changes.
*/
func TestADigestWaitsOutAStatusThatSaysNotToInterrupt(t *testing.T) {
	handler, post := withDigestMail(t)
	owner := completeSetup(t, handler)
	room := makeRoom(t, handler, owner, "general", "public")

	registerVerified(t, handler, "amina", "amina@example.test")
	amina := signIn(t, handler, "amina", testPassword)
	joinRoom(t, handler, amina, room)
	chooseDigest(t, handler, amina, map[string]any{"frequency": "daily", "hour": 8, "timeZone": "UTC"})

	sendMessage(t, handler, owner, room, "@amina habari")

	mustCall(t, handler, http.MethodPut, "/api/me/status", map[string]any{"status": "busy"}, amina, http.StatusOK)

	server := serverBehind(t, handler)
	makeDigestDue(t, "amina")
	if sent := server.DeliverDigests(context.Background()); sent != 0 {
		t.Fatalf("a summary went to somebody whose status says not to interrupt: %d", sent)
	}

	mustCall(t, handler, http.MethodPut, "/api/me/status", map[string]any{"status": "available"}, amina, http.StatusOK)
	makeDigestDue(t, "amina")
	if sent := server.DeliverDigests(context.Background()); sent != 1 {
		t.Fatalf("after the status changed, sent %d, want 1", sent)
	}
	if bodies := summariesTo(post, "amina@example.test"); len(bodies) != 1 || !strings.Contains(bodies[0], "general: 1 notification") {
		t.Errorf("the held summary did not carry what it held: %v", bodies)
	}
}

/*
Several servers share one database, and each runs delivery passes. However many
run at once, a summary goes once — which is the failure that matters, since a
summary not yet sent can go on the next pass and one sent twice cannot be
taken back.
*/
func TestConcurrentPassesSendADigestOnce(t *testing.T) {
	handler, post := withDigestMail(t)
	owner := completeSetup(t, handler)
	room := makeRoom(t, handler, owner, "general", "public")

	registerVerified(t, handler, "amina", "amina@example.test")
	amina := signIn(t, handler, "amina", testPassword)
	joinRoom(t, handler, amina, room)
	chooseDigest(t, handler, amina, map[string]any{"frequency": "daily", "hour": 8, "timeZone": "UTC"})
	sendMessage(t, handler, owner, room, "@amina habari")
	makeDigestDue(t, "amina")

	server := serverBehind(t, handler)
	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			server.DeliverDigests(context.Background())
		}()
	}
	wait.Wait()

	if bodies := summariesTo(post, "amina@example.test"); len(bodies) != 1 {
		t.Errorf("eight concurrent passes mailed amina %d summaries, want 1", len(bodies))
	}
}

// The batching window is the operator's, bounded to something that is still a
// burst rather than a silence.
func TestTheBatchWindowIsValidated(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	for value, want := range map[string]int{
		"0": http.StatusNoContent, "300": http.StatusNoContent,
		"3601": http.StatusBadRequest, "-1": http.StatusBadRequest, "soon": http.StatusBadRequest,
	} {
		recorder := call(t, handler, http.MethodPut, "/api/admin/settings/notifications.batch_seconds",
			map[string]string{"value": value}, owner)
		if recorder.Code != want {
			t.Errorf("batch window %q: status %d, want %d", value, recorder.Code, want)
		}
	}
}
