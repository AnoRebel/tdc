package httpapi

import (
	"context"
	"sync"
	"time"
)

/*
detached returns a context that outlives the request it came from.

A request's context is cancelled the moment the response is written. Work
handed to a goroutine with that context is therefore cancelled immediately,
which turns "do this in the background" into "do not do this at all" —
silently, because the cancellation looks like an ordinary error.

Values are carried across so that anything reading request-scoped state still
finds it, and a deadline is imposed so a stuck operation cannot accumulate
goroutines for the life of the process.
*/
func detached(ctx context.Context, limit time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), limit)
}

/*
backgroundLimit bounds work detached from a request.

Generous, because the work it covers is a handful of database writes: the limit
exists to stop something stuck accumulating goroutines, not to bound normal
operation.
*/
const backgroundLimit = 30 * time.Second

/*
inBackground runs work without holding up the response.

Used for the things that must not delay a message: raising notifications and
indexing for search. Neither is worth making somebody wait for, and neither
failing should fail the send — the message is already stored, which is the part
that matters.

The panic guard is what makes this safe to use: a panic in a bare goroutine
takes down the whole server, where one on a request path is recovered by
middleware.
*/
func (s *Server) inBackground(ctx context.Context, name string, work func(context.Context)) {
	detachedCtx, cancel := detached(ctx, backgroundLimit)

	// Tracked so that shutdown can wait for it, and so a test can wait for the
	// effects of a request it has just made. Without this, a server could exit
	// with notifications half-written, and every test touching one would race.
	s.background.add()

	go func() {
		defer s.background.done()
		defer cancel()
		defer func() {
			if recovered := recover(); recovered != nil {
				s.log.Error("background work panicked",
					"work", name, "panic", recovered)
			}
		}()

		work(detachedCtx)
	}()
}

/*
WaitForBackground blocks until work detached from requests has finished.

Called on shutdown, so a server does not exit with notifications half-written,
and by tests, which would otherwise race the very work they are asserting on.
*/
func (s *Server) WaitForBackground() {
	s.background.wait()
}

/*
inFlight counts the work detached from requests, and lets something wait for
it to reach none.

Not a WaitGroup, whose Add from zero must not overlap a Wait: requests arrive
while a test waits for an earlier one's work, and a server under load starts
work while shutdown has begun waiting. Here starting work during a wait is
allowed; the wait simply ends at the next moment nothing is running.
*/
type inFlight struct {
	mu    sync.Mutex
	count int
	idle  sync.Cond
}

func (f *inFlight) add() {
	f.mu.Lock()
	f.count++
	f.mu.Unlock()
}

func (f *inFlight) done() {
	f.mu.Lock()
	f.count--
	if f.count == 0 {
		f.idle.Broadcast()
	}
	f.mu.Unlock()
}

func (f *inFlight) wait() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.idle.L = &f.mu
	for f.count > 0 {
		f.idle.Wait()
	}
}
