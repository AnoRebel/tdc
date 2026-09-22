package federation

import (
	"math"
	"math/rand/v2"
	"time"
)

/*
How long to wait before trying an unreachable peer again.

The same shape the web client already uses to reconnect, for the same reason:
exponential so that a peer which is down stops being hammered, and jittered so
that many deliveries failing at the same moment — which is what a peer going
down looks like from here — do not all retry in the same instant and arrive as
a thundering herd the moment it comes back.

Past a ceiling of attempts the peer is abandoned rather than retried forever.
A peer that has been unreachable for a day is not coming back on the next
attempt, and continuing to try costs this server work it could spend on peers
that are answering. The spec calls for exactly this; the operator is told, and
delivery resumes when they say so.
*/

const (
	// FirstRetryDelay is how long to wait after the first failure.
	FirstRetryDelay = 2 * time.Second

	// MaxRetryDelay caps the wait, so a peer down for hours is still tried
	// occasionally rather than at ever-increasing intervals that eventually
	// mean never.
	MaxRetryDelay = 5 * time.Minute

	/*
		MaxDeliveryAttempts is how many consecutive failures abandon a peer.

		Ten, which with the delays above is roughly forty minutes of trying.
		Long enough to ride out a restart, a deployment, or a brief network
		partition; short enough that a peer which has genuinely gone away
		stops costing anything.
	*/
	MaxDeliveryAttempts = 10
)

/*
RetryDelay returns how long to wait before attempt n, counting from zero.

Jittered to between half and all of the exponential value, matching the client
reconnection exactly. The randomness is what prevents synchronised retries; the
halving keeps the average delay sensible rather than doubling it.
*/
func RetryDelay(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}

	// Exponent capped before the shift so that a large attempt count cannot
	// overflow into a negative or absurd duration.
	ceiling := float64(FirstRetryDelay) * math.Pow(2, float64(attempt))
	if ceiling > float64(MaxRetryDelay) || math.IsInf(ceiling, 1) {
		ceiling = float64(MaxRetryDelay)
	}

	return time.Duration(ceiling * (0.5 + rand.Float64()*0.5))
}

/*
Abandoned reports whether a peer has failed often enough to stop trying.

Separate from RetryDelay so the decision is one a caller makes explicitly
rather than inferring from a delay that happens to be large — the difference
between "wait longer" and "stop" is exactly what an operator needs told.
*/
func Abandoned(attempts int) bool { return attempts >= MaxDeliveryAttempts }
