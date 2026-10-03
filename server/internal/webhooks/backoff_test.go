package webhooks

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/AnoRebel/tdc/server/internal/settings"
)

// fixedSettings answers every key from a map.
type fixedSettings map[settings.Key]int64

func (f fixedSettings) Int(_ context.Context, key settings.Key) int64 { return f[key] }

/*
Each retry waits longer than the one before, up to the cap, and never longer.

Checked across many draws, since the jitter is random: the ranges for
successive attempts must not overlap, or "increasing" would only be true on
average — and an owner watching a receiver come back would see it retried
sooner the second time than the first.
*/
func TestEachRetryWaitsLongerUpToTheCap(t *testing.T) {
	service := New(nil, Config{Settings: fixedSettings{
		settings.WebhookRetryBaseSeconds: 60,
		settings.WebhookRetryCapSeconds:  600,
	}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()

	for range 200 {
		previous := time.Duration(0)
		for attempt := int32(1); attempt <= 6; attempt++ {
			delay := service.backoff(ctx, attempt)
			nominal := time.Minute << (attempt - 1)
			if nominal > 10*time.Minute {
				nominal = 10 * time.Minute
			}

			if delay > nominal || delay < nominal*3/4 {
				t.Fatalf("attempt %d waited %s; it should be within a quarter below %s", attempt, delay, nominal)
			}
			if nominal < 10*time.Minute && delay <= previous {
				t.Fatalf("attempt %d waited %s, no longer than the %s before it", attempt, delay, previous)
			}
			previous = delay
		}
	}
}
