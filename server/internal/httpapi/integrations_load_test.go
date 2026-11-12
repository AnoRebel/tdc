package httpapi_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AnoRebel/tdc/server/internal/httpapi"
)

/*
What webhook delivery can carry, and what it costs everybody else.

How many deliveries a second the worker pool sends to receivers that answer
promptly, as a baseline to notice regressions against; and how long posting a
message takes with no webhooks at all, beside a receiver that never answers,
and while the pool is busy. The claim that matters is the second: a stalled
receiver costs the people in the channel nothing.

The targets are in docs/webhooks-and-apps.md, under "What it can carry", and
this test fails if they are missed. They are set well inside what this machine
measures, so a shared CI runner meets them too; the figures themselves are
printed so a change in them is visible.
*/

const (
	// deliveryThroughputTarget is the least the pool should manage, per second,
	// to receivers on the same machine with the default four workers and the
	// default pace of a hundred a second.
	deliveryThroughputTarget = 50.0
	/*
		latencyAllowance is how much slower posting may be with a stalled
		receiver beside it: half again, or five milliseconds, whichever is more
		— noise, not a cost.

		Compared on the median. The tail of a hundred requests on a shared
		machine moves with whatever else the machine is doing, and a comparison
		that failed on a neighbour's load would be one somebody learned to
		ignore; the 95th percentile is printed beside it regardless.
	*/
	latencyAllowance = 1.5
	latencyFloor     = 5 * time.Millisecond
)

func TestDeliveryLoadLeavesChatUnaffected(t *testing.T) {
	/*
		Only when asked, and then alone. Run beside the rest of the suite —
		every package at once, against one database — it measures how busy the
		machine is rather than what a stalled receiver costs, and fails for
		reasons that have nothing to do with webhooks. CI runs it by itself,
		in the job that measures memory.
	*/
	if os.Getenv("TDC_LOAD_TESTS") == "" {
		t.Skip("a load measurement: set TDC_LOAD_TESTS=1 and run it on its own")
	}

	settled := newTestAPI(t)
	owner := completeSetup(t, settled)
	for key, value := range map[string]string{
		"integrations.allow_private_addresses":       "true",
		"integrations.webhook_deliveries_per_minute": "0",
		"integrations.delivery_timeout_seconds":      "1",
		// The stalled receiver must stay subscribed for the whole run.
		"integrations.webhook_disable_after_failures": "10000",
	} {
		setSetting(t, settled, owner, key, value)
	}
	roomID := makeRoom(t, settled, owner, "load", "public")
	startDeliveries(t, settled)

	/*
		Posting is timed over a real listener, and through the handler rather
		than the test harness's wrapper: the wrapper waits for background work
		before answering, which is exactly the work this measures as off the
		request's path.
	*/
	server := httptest.NewServer(settled.(interface{ Server() *httpapi.Server }).Server().Handler())
	defer server.Close()

	postTimes := func(count int, label string) []time.Duration {
		times := make([]time.Duration, 0, count)
		for i := range count {
			body, _ := json.Marshal(map[string]any{"body": fmt.Sprintf("%s %d", label, i)})
			request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/rooms/"+roomID+"/messages",
				bytes.NewReader(body))
			request.Header.Set("Authorization", "Bearer "+owner)
			request.Header.Set("Content-Type", "application/json")

			started := time.Now()
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatalf("posting: %v", err)
			}
			_ = response.Body.Close()
			times = append(times, time.Since(started))
			if response.StatusCode != http.StatusCreated {
				t.Fatalf("posting returned %d", response.StatusCode)
			}
		}
		return times
	}

	quietTimes := postTimes(100, "quiet")
	quiet, quietTail := percentile(quietTimes, 50), percentile(quietTimes, 95)

	var delivered atomic.Int64
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		delivered.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer fast.Close()
	stalled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Holds every request past the delivery timeout, and answers nothing.
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer stalled.Close()

	/*
		The claim that matters, measured on its own: a receiver that never
		answers, subscribed to the channel, while people post. Every message
		queues a delivery that will hold a worker for the whole timeout.
	*/
	subscribe(t, settled, owner, roomID, stalled.URL)
	stalledTimes := postTimes(100, "stalled")
	besideStalled, besideStalledTail := percentile(stalledTimes, 50), percentile(stalledTimes, 95)

	// Then the pool at work: prompt receivers beside the stalled one.
	const fastSubscriptions = 8
	for range fastSubscriptions {
		subscribe(t, settled, owner, roomID, fast.URL)
	}

	const messages = 100
	started := time.Now()
	underLoad := percentile(postTimes(messages, "loaded"), 95)

	want := int64(messages * fastSubscriptions)
	eventually(t, 60*time.Second, "every delivery to the prompt receivers", func() bool {
		return delivered.Load() >= want
	})
	throughput := float64(delivered.Load()) / time.Since(started).Seconds()

	fmt.Printf("DELIVERY_THROUGHPUT_PER_SECOND=%.0f\n", throughput)
	fmt.Printf("POST_MEDIAN_QUIET_MS=%.2f\nPOST_MEDIAN_BESIDE_STALLED_RECEIVER_MS=%.2f\n",
		milliseconds(quiet), milliseconds(besideStalled))
	fmt.Printf("POST_P95_QUIET_MS=%.2f\nPOST_P95_BESIDE_STALLED_RECEIVER_MS=%.2f\nPOST_P95_UNDER_DELIVERY_LOAD_MS=%.2f\n",
		milliseconds(quietTail), milliseconds(besideStalledTail), milliseconds(underLoad))
	t.Logf("%d deliveries in %s: %.0f a second (target %.0f)",
		delivered.Load(), time.Since(started).Round(time.Millisecond), throughput, deliveryThroughputTarget)
	t.Logf("posting, median: %s alone, %s beside a stalled receiver", quiet, besideStalled)
	t.Logf("posting, 95th percentile: %s alone, %s beside a stalled receiver, %s while the pool delivers",
		quietTail, besideStalledTail, underLoad)

	if throughput < deliveryThroughputTarget {
		t.Errorf("delivered %.0f a second, under the documented %.0f", throughput, deliveryThroughputTarget)
	}
	allowed := max(time.Duration(float64(quiet)*latencyAllowance), quiet+latencyFloor)
	if besideStalled > allowed {
		t.Errorf("posting's median went from %s to %s beside a stalled receiver; the allowance is %s",
			quiet, besideStalled, allowed)
	}
	/*
		Delivering does cost the database something — every attempt is a
		commit — so posting while the pool is busy is allowed to be slower. It
		is bounded all the same, so a change that made deliveries crowd out
		messages would be noticed.
	*/
	if loadAllowance := 3*quietTail + 10*time.Millisecond; underLoad > loadAllowance {
		t.Errorf("posting's 95th percentile went from %s to %s while the pool delivered; the bound is %s",
			quietTail, underLoad, loadAllowance)
	}
}

func percentile(samples []time.Duration, p int) time.Duration {
	sorted := slices.Clone(samples)
	slices.Sort(sorted)
	return sorted[(len(sorted)*p+99)/100-1]
}

func milliseconds(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
