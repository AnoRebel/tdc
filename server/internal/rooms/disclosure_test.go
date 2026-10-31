package rooms_test

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
A member list carries only what each person allows it to.

The name and address are unconditional — they are how a message is attributed
and a conversation is opened — and the three details past that are the person's
own. What these check is that withholding happens in the query: a detail
somebody has withheld must never be read out of the database, because a value
in memory is a value some later change can send by accident.
*/

// withhold records that somebody does not want a detail shown.
func withhold(t *testing.T, queries *dbgen.Queries, userID uuid.UUID, presence, status, lastSeen bool) {
	t.Helper()
	err := queries.SetDisclosurePreferences(context.Background(),
		dbgen.SetDisclosurePreferencesParams{
			UserID:        userID,
			ShowsPresence: presence,
			ShowsStatus:   status,
			ShowsLastSeen: lastSeen,
		})
	if err != nil {
		t.Fatalf("SetDisclosurePreferences: %v", err)
	}
}

// find returns one person's row from a member list.
func find(t *testing.T, members []dbgen.ListRoomMembersWithDisclosureRow, username string) dbgen.ListRoomMembersWithDisclosureRow {
	t.Helper()
	for _, member := range members {
		if member.Username == username {
			return member
		}
	}
	t.Fatalf("%s is not in the member list", username)
	return dbgen.ListRoomMembersWithDisclosureRow{}
}

/*
Somebody who has never opened these settings discloses everything.

user_privacy is written on the first change, so nearly everybody has no row.
Reading a missing row as "withholds everything" would have made the member list
useless on the day it shipped, and it is the failure that would look like the
feature working.
*/
func TestTheDefaultIsToDisclose(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	owner := makeUser(t, queries, "amina")
	guest := makeUser(t, queries, "juma")
	room := makeRoom(t, service, owner.ID, "general")
	if err := service.Join(ctx, room.ID, guest.ID); err != nil {
		t.Fatalf("Join: %v", err)
	}

	members, err := service.MembersWithDisclosure(ctx, room.ID, owner.ID, false, 1)
	if err != nil {
		t.Fatalf("MembersWithDisclosure: %v", err)
	}

	juma := find(t, members, "juma")
	if juma.WithholdsPresence || juma.WithholdsStatus || juma.WithholdsLastSeen {
		t.Error("somebody who has changed nothing is withholding something")
	}
}

/*
A withheld detail is not read at all.

The query returns the zero value and says the detail is withheld; the handler
omits the field on that basis. What matters here is that the database was never
asked for the real value, so there is nothing in the row to leak.
*/
func TestAWithheldDetailIsNotReturned(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	owner := makeUser(t, queries, "amina")
	guest := makeUser(t, queries, "juma")
	room := makeRoom(t, service, owner.ID, "general")
	if err := service.Join(ctx, room.ID, guest.ID); err != nil {
		t.Fatalf("Join: %v", err)
	}

	// Juma is online with a status, and willing to show none of it.
	if _, err := queries.ConnectionOpened(ctx, guest.ID); err != nil {
		t.Fatalf("ConnectionOpened: %v", err)
	}
	if _, err := queries.SetStatus(ctx, dbgen.SetStatusParams{
		UserID:     guest.ID,
		Status:     "busy",
		StatusText: "reviewing a pull request",
	}); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	withhold(t, queries, guest.ID, false, false, false)

	members, err := service.MembersWithDisclosure(ctx, room.ID, owner.ID, false, 1)
	if err != nil {
		t.Fatalf("MembersWithDisclosure: %v", err)
	}

	juma := find(t, members, "juma")
	if !juma.WithholdsPresence || !juma.WithholdsStatus || !juma.WithholdsLastSeen {
		t.Fatal("the withholding was not recorded")
	}
	if juma.Online {
		t.Error("presence was withheld and came back anyway")
	}
	if juma.Status != "" || juma.StatusText != "" {
		t.Errorf("status was withheld and came back as %q / %q", juma.Status, juma.StatusText)
	}
	if juma.LastSeenAt.After(time.Unix(0, 0)) {
		t.Errorf("last seen was withheld and came back as %s", juma.LastSeenAt)
	}

	// And the name is still there, because it is not the person's to withhold.
	if juma.DisplayName == "" && juma.Username == "" {
		t.Error("the member list lost the person it was listing")
	}
}

/*
Withholding one detail does not withhold the others.

Three separate decisions. Somebody who hides when they were last here is often
happy to be shown as online now — that is the whole reason they are separate
columns rather than one switch.
*/
func TestDetailsAreWithheldIndividually(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	owner := makeUser(t, queries, "amina")
	guest := makeUser(t, queries, "juma")
	room := makeRoom(t, service, owner.ID, "general")
	if err := service.Join(ctx, room.ID, guest.ID); err != nil {
		t.Fatalf("Join: %v", err)
	}

	if _, err := queries.ConnectionOpened(ctx, guest.ID); err != nil {
		t.Fatalf("ConnectionOpened: %v", err)
	}
	// Shown as online, but not when they were last here.
	withhold(t, queries, guest.ID, true, true, false)

	members, err := service.MembersWithDisclosure(ctx, room.ID, owner.ID, false, 1)
	if err != nil {
		t.Fatalf("MembersWithDisclosure: %v", err)
	}

	juma := find(t, members, "juma")
	if !juma.Online {
		t.Error("presence was disclosed but did not come back")
	}
	if juma.WithholdsPresence {
		t.Error("presence was disclosed and is reported as withheld")
	}
	if !juma.WithholdsLastSeen {
		t.Error("last seen was withheld and is not reported as withheld")
	}
	if juma.LastSeenAt.After(time.Unix(0, 0)) {
		t.Error("last seen was withheld and came back anyway")
	}
}

/*
A moderator sees what was withheld.

Moderation needs the access — a report about somebody's status cannot be judged
without reading it — so the answer is not to refuse but to account for it. The
audit entry is the handler's job; what this checks is that the query opens.
*/
func TestAModeratorSeesWithheldDetails(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	owner := makeUser(t, queries, "amina")
	guest := makeUser(t, queries, "juma")
	room := makeRoom(t, service, owner.ID, "general")
	if err := service.Join(ctx, room.ID, guest.ID); err != nil {
		t.Fatalf("Join: %v", err)
	}

	if _, err := queries.ConnectionOpened(ctx, guest.ID); err != nil {
		t.Fatalf("ConnectionOpened: %v", err)
	}
	if _, err := queries.SetStatus(ctx, dbgen.SetStatusParams{
		UserID:     guest.ID,
		Status:     "busy",
		StatusText: "reviewing a pull request",
	}); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	withhold(t, queries, guest.ID, false, false, false)

	members, err := service.MembersWithDisclosure(ctx, room.ID, owner.ID, true, 1)
	if err != nil {
		t.Fatalf("MembersWithDisclosure: %v", err)
	}

	juma := find(t, members, "juma")
	if juma.Status != "busy" {
		t.Errorf("a moderator saw status %q, want the withheld value", juma.Status)
	}
	if !juma.Online {
		t.Error("a moderator did not see the withheld presence")
	}
	if !juma.LastSeenAt.After(time.Unix(0, 0)) {
		t.Error("a moderator did not see the withheld last-seen time")
	}

	// And is still told it was withheld, so the view is read as an exception
	// rather than as ordinary information.
	if !juma.WithholdsStatus {
		t.Error("a moderator was shown the value without being told it was withheld")
	}
}
