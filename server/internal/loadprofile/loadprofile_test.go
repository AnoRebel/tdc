package loadprofile_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"testing"
	"time"

	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/dbtest"
	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/identity"
	"github.com/AnoRebel/tdc/server/internal/rooms"
)

/*
What the server costs to run, measured rather than assumed.

The design commits to a 1 GB budget for the server process, and the reason is
not tidiness: it is the difference between something a Tanzanian developer can
host on a cheap VPS and something that needs a machine nobody wants to pay for.
Mattermost — the closest analogue — wants roughly 4 GB for a community this size.

This is deliberately not a threshold test. It reports a figure, and CI puts that
figure and its change in front of whoever is reviewing. Memory moves for good
reasons, and a job that failed on it would be routed around within a month; a
number somebody has to look at is harder to ignore than a gate they learn to
bypass.

What it measures is the heap after a load that resembles a small community
actually using the server, held rather than discarded, because the question is
what the process retains — not what it allocated and gave back.
*/

// The shape of a small chapter using this: enough people and messages that the
// figure means something, few enough that the test finishes in CI.
const (
	members         = 50
	channels        = 10
	messagesPerRoom = 200
)

/*
memoryTarget is what the design committed to.

Reported against rather than enforced. A build that failed here would be a build
somebody disabled.
*/
const memoryTarget = 1 << 30 // 1 GiB

func TestServerFitsItsMemoryTarget(t *testing.T) {
	url := dbtest.URL(t, "loadprofile")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := db.Migrate(ctx, url, quiet); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	pool, err := db.Open(ctx, db.Config{URL: url}, quiet)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	/*
		Cleared before as well as after.

		This database is reused between runs, and rooms do not cascade from
		users — a leftover channel from the last run makes the next one fail on
		a name collision, which reads as a broken test rather than a dirty
		database.
	*/
	clean := func() {
		for _, statement := range []string{"DELETE FROM rooms", "DELETE FROM users"} {
			if _, err := pool.Exec(context.Background(), statement); err != nil {
				t.Errorf("clean: %v", err)
			}
		}
	}
	clean()
	t.Cleanup(func() {
		clean()
		pool.Close()
	})

	queries := dbgen.New(pool)
	people := identity.NewService(queries, quiet)

	signingKey, err := events.NewKeyPair("loadprofile")
	if err != nil {
		t.Fatalf("NewKeyPair: %v", err)
	}
	conversation := rooms.NewService(queries, events.NewKeyring(signingKey), "tdc.test", quiet)

	/*
		A baseline taken after everything is wired but before any load, so the
		figure below is what the work cost rather than what the runtime and the
		connection pool cost to exist.
	*/
	before := retained()

	/*
		The load itself, held in scope.

		Kept referenced deliberately: the question is what a running server
		retains while people are using it, and letting the garbage collector take
		it all back would measure nothing.
	*/
	held := make([]dbgen.User, 0, members)
	for index := 0; index < members; index++ {
		user, err := people.CreateUser(ctx, identity.NewUser{
			Username: fmt.Sprintf("member%03d", index),
			Password: "correct horse battery staple",
		})
		if err != nil {
			t.Fatalf("creating member %d: %v", index, err)
		}
		held = append(held, user)
	}

	channelList := make([]dbgen.Room, 0, channels)
	for index := 0; index < channels; index++ {
		slug := fmt.Sprintf("channel-%02d", index)
		room, err := conversation.Create(ctx, rooms.NewRoom{
			Slug:      slug,
			Name:      slug,
			CreatedBy: held[index%len(held)].ID,
		})
		if err != nil {
			t.Fatalf("creating channel %d: %v", index, err)
		}
		channelList = append(channelList, room)
	}

	/*
		The messages, which are the bulk of what a chat server does.

		Each one is a signed event appended to the log, so this exercises the
		signing, the sequence allocation and the write path — the work that
		actually happens when a community is talking.
	*/
	for _, room := range channelList {
		for index := 0; index < messagesPerRoom; index++ {
			sender := held[index%len(held)]

			// Everybody who speaks has to be in the room first.
			if index < len(held) {
				if err := conversation.Join(ctx, room.ID, sender.ID); err != nil {
					t.Fatalf("joining %s: %v", room.Name, err)
				}
			}

			if _, err := conversation.SendMessage(ctx, room.ID, sender.ID,
				fmt.Sprintf("message %d in %s", index, room.Name), ""); err != nil {
				t.Fatalf("sending in %s: %v", room.Name, err)
			}
		}
	}

	after := retained()

	/*
		Printed in a shape CI can read without parsing prose.

		The workflow greps for this line, so its format is part of the contract
		between this test and the job that reports it.
	*/
	fmt.Printf("MEMORY_BYTES=%d\n", after)

	t.Logf("held from the system: %.1f MiB before the load, %.1f MiB after",
		float64(before)/1024/1024, float64(after)/1024/1024)
	t.Logf("target: %.0f MiB (%.1f%% used)",
		float64(memoryTarget)/1024/1024, float64(after)/float64(memoryTarget)*100)

	/*
		What was allocated along the way, as well as what is still held.

		A server that allocates a gigabyte to retain two megabytes is doing
		something worth knowing about even though its footprint is fine: it is
		work for the garbage collector, and on a small VPS that is latency.
	*/
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	t.Logf("allocated in total: %.1f MiB, of which %.1f MiB is still on the heap",
		float64(stats.TotalAlloc)/1024/1024, float64(stats.HeapInuse)/1024/1024)

	// Held to here, so nothing above is collected before it is measured.
	runtime.KeepAlive(held)
	runtime.KeepAlive(channelList)
}

/*
retained is what the process has taken from the operating system, after
collection.

Sys rather than HeapInuse, and the difference matters. This server holds almost
nothing on the heap — messages go to Postgres and are not cached, so a run that
allocates a gigabyte in total retains under two megabytes of it. HeapInuse
therefore reports a number that is true and useless: it would stay flat while the
process's actual footprint doubled.

Sys is every region the runtime has obtained: the heap, the stacks, the garbage
collector's own structures, and the spans it has not returned. It is the closest
thing the runtime offers to what somebody watching a 1 GB VPS would see, which is
whose question this is.

Collected twice first, because one cycle finishes what the previous one
scheduled.
*/
func retained() uint64 {
	runtime.GC()
	runtime.GC()

	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return stats.Sys
}
