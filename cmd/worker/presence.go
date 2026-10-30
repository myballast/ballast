package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Presence is what the loop below asks of the worker's way of saying it is alive.
type Presence interface {
	Serving(ctx context.Context) error
	Draining(ctx context.Context) error
	Stopped(ctx context.Context) error
}

// beat says, until ctx ends, that this worker is there.
//
// It runs on its own and not between two runs, and that is the point: a worker
// executing a two-hour soak would otherwise say nothing for two hours and be read
// as offline for all of them, which is exactly when somebody looks.
//
// The first signal flips what it says to draining, and the beats go on: a
// draining worker is a live one, still executing the run it was told to finish.
// What ends the loop is ctx, which the caller cancels once the last run is over;
// the last word is that the worker stopped.
//
// A report that fails is logged and retried at the next beat. A database that
// blinks is no reason for the worker to stop, nor to stop saying it is alive.
func beat(
	ctx context.Context,
	drain context.Context,
	presence Presence,
	every time.Duration,
	log *slog.Logger,
) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	draining := drain.Done()

	report := func() {
		var err error
		if drain.Err() != nil {
			err = presence.Draining(ctx)
		} else {
			err = presence.Serving(ctx)
		}
		if err != nil {
			log.Warn("the worker could not say that it is alive", "error", err)
		}
	}

	// At once, not after the first interval: a worker that has just started is
	// online from that moment.
	report()

	for {
		select {
		case <-ctx.Done():
			if err := presence.Stopped(ctx); err != nil {
				log.Warn("the worker could not say that it stopped", "error", err)
			}
			return
		case <-draining:
			// Said now rather than at the next beat: a deployment should be
			// visible when it begins. Nil afterwards, or this case would fire in
			// a loop for as long as drain stays done.
			draining = nil
			report()
		case <-ticker.C:
			report()
		}
	}
}

// Admitter answers whether this worker is current enough to be handed a run.
type Admitter interface {
	Admit(ctx context.Context, orgID uuid.UUID) (bool, int, error)
}

// contractGuard stands in front of the run service. A worker older than the
// organization's minimum stays up and keeps beating, and never reaches the call
// that claims a run: what it did not take stays queued, intact, for one that is
// current.
type contractGuard struct {
	next     Processor
	gate     Admitter
	contract int
	log      *slog.Logger

	// said is the minimum already reported, so that a worker polling every second
	// does not write the same sentence a second time every second.
	said int
}

func (g *contractGuard) ProcessNext(ctx context.Context, orgID uuid.UUID, logs chan<- domain.LogLine) (bool, error) {
	admitted, minimum, err := g.gate.Admit(ctx, orgID)
	if err != nil {
		return false, err
	}
	if !admitted {
		if g.said != minimum {
			g.said = minimum
			g.log.Warn("this worker is older than the minimum and takes no run; runs stay queued",
				"contract", g.contract, "minimum", minimum)
		}
		return false, nil
	}

	g.said = 0
	return g.next.ProcessNext(ctx, orgID, logs)
}
