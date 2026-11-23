package cli

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"
)

const (
	// DefaultQueueTimeout is how long the tool waits for Ballast to have room for a
	// run, when it is not told. Ten minutes: a pipeline that meets a limit at the wrong
	// moment goes on by itself, and one that cannot be served in that time leaves with
	// the reason instead of sitting there.
	DefaultQueueTimeout = 10 * time.Minute

	// defaultQueuedNotice is how often a run that is still queued says so.
	defaultQueuedNotice = time.Minute
)

// launchPauses is how long the tool waits before asking again when the server did
// not say how long. They grow, and stop growing: a full queue empties when runs end,
// which nobody can predict.
var launchPauses = []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 30 * time.Second, time.Minute}

// defaultLaunchBackoff is the pause before the nth new try, from 1, with a little
// chance added: pipelines that were started together must not come back together.
func defaultLaunchBackoff(attempt int) time.Duration {
	i := min(max(attempt, 1), len(launchPauses)) - 1
	base := launchPauses[i]
	// Jitter only spreads pipelines out: it is not a secret, so a plain generator will do.
	return base + time.Duration(rand.Int64N(int64(base)/5+1)) //nolint:gosec
}

// errWaitInterrupted is a signal or the timeout arriving while the tool waited to
// ask again. No request was in flight, so no run may exist: it is not the case of a
// launch whose outcome is unknown.
var errWaitInterrupted = errors.New("interrupted while waiting for room to launch")

// queueWaitError is a launch that was refused for the limits and could not be
// served in the time the tool was given.
type queueWaitError struct {
	refusal *APIError
	// waited is what the tool paused in all, limit what it was allowed.
	waited, limit time.Duration
	// needed is how long the server asked for, zero when it did not say.
	needed time.Duration
}

func (e *queueWaitError) Error() string { return e.refusal.Error() }
func (e *queueWaitError) Unwrap() error { return e.refusal }

// launch asks for the run, and asks again for as long as the answer is "not now"
// and the time given to waiting is not spent.
//
// Only a 429 is asked again. It is a refusal that comes before anything is created,
// so a second request cannot make a second run. Everything else keeps its meaning:
// a definite refusal is final, and an outcome that is not known is never repeated.
func (r *Runner) launch(
	ctx context.Context, options RunOptions, request LaunchRequest, say func(string, ...any),
) (Queued, error) {
	var waited time.Duration
	for attempt := 1; ; attempt++ {
		queued, err := r.Client.Launch(ctx, request)
		if err == nil {
			return queued, nil
		}

		var refusal *APIError
		// The context check is a second line: an ended context would be found by the
		// pause too, with the same words. It keeps an answer that arrived as the
		// time ran out from being reported as a wait.
		if ctx.Err() != nil || !errors.As(err, &refusal) || refusal.Status != 429 || options.QueueTimeout <= 0 {
			return Queued{}, err
		}

		pause := refusal.RetryAfter
		left := options.QueueTimeout - waited
		switch {
		case pause > left:
			// The server said how long, and it does not fit. Waiting to fail anyway
			// helps nobody: the pipeline learns it now.
			return Queued{}, &queueWaitError{refusal: refusal, waited: waited, limit: options.QueueTimeout, needed: pause}
		case pause <= 0:
			// The tool's own guess may be cut short, never turned into a refusal.
			pause = min(r.launchBackoff(attempt), left)
			if pause <= 0 {
				return Queued{}, &queueWaitError{refusal: refusal, waited: waited, limit: options.QueueTimeout}
			}
		}

		say("Ballast is limiting runs: %s. Trying again in %s (waited %s of the --queue-timeout of %s)",
			refusal.Message, pause.Round(time.Second), waited.Round(time.Second), options.QueueTimeout)
		if !r.launchWait(ctx, pause) {
			return Queued{}, errWaitInterrupted
		}
		waited += pause
	}
}

func (r *Runner) launchBackoff(attempt int) time.Duration {
	if r.LaunchBackoff != nil {
		return r.LaunchBackoff(attempt)
	}
	return defaultLaunchBackoff(attempt)
}

func (r *Runner) launchWait(ctx context.Context, d time.Duration) bool {
	if r.LaunchWait != nil {
		return r.LaunchWait(ctx, d)
	}
	return r.sleep(ctx, d)
}

func (r *Runner) queuedNotice() time.Duration {
	if r.QueuedNotice > 0 {
		return r.QueuedNotice
	}
	return defaultQueuedNotice
}

// describeQueueWait is the sentence for a launch that could not be served in time. The
// server's own words say which limit; the numbers added are the tool's own.
func describeQueueWait(e *queueWaitError) string {
	message := "Ballast is limiting runs: " + e.refusal.Message
	if e.needed > 0 {
		return fmt.Sprintf("%s. It asks to wait %s, which does not fit in the --queue-timeout of %s",
			message, e.needed.Round(time.Second), e.limit)
	}
	return fmt.Sprintf("%s. Gave up after waiting %s, the --queue-timeout of %s",
		message, e.waited.Round(time.Second), e.limit)
}
