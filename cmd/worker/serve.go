package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/runner"
)

// Processor is what the loop below asks of the run service: take the next queued
// run of an organization and execute it. A name of its own so that the loop can be
// tested without a database, a Docker daemon and every port the service holds.
type Processor interface {
	ProcessNext(ctx context.Context, orgID uuid.UUID, logs chan<- domain.LogLine) (bool, error)
}

// Scope is what the loop asks of the organizations a worker serves: which of
// them may have something to do right now.
type Scope interface {
	Active(ctx context.Context) ([]uuid.UUID, error)
}

// serve is the whole scheduling story: look at the organizations with work, in
// turn, and give each a slot to run in while the machine has one.
//
// Each attempt is a goroutine that holds a slot for as long as its run lasts, so
// one organization's hour-long soak does not keep another's run from starting.
// Two things bound that. The machine has a fixed number of slots, because a
// generator more than it can hold would measure its own saturation instead of the
// target. And an organization holds at most its share of them, because the slot
// left over is for somebody else.
//
// The order in which organizations are looked at rotates, the one served last
// being looked at last: with room for a single run, an organization with ten
// waiting cannot keep another's first behind all of them.
//
// It takes two contexts because stopping comes in two kinds. Cancelling drain
// means take nothing more; every run under way is handed hard, which a first
// signal does not touch, and serve returns only once they have all finished.
// Cancelling hard cancels them too.
func serve(
	drain context.Context,
	hard context.Context,
	processor Processor,
	scope Scope,
	slots *runner.Slots,
	interval time.Duration,
	log *slog.Logger,
) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var (
		rotation runner.Rotation
		inflight sync.WaitGroup
		// finished wakes the loop when a run was taken and ended: with a queue
		// behind it there is no reason to wait for the next tick.
		finished = make(chan struct{}, 1)
	)
	// A worker does not leave while a run it started is still measuring.
	defer inflight.Wait()

	stopping := func() bool { return drain.Err() != nil || hard.Err() != nil }

	for {
		// Before claiming, and not only when idle: with a full queue the loop
		// would otherwise never look, and a worker told to stop would take run
		// after run.
		if stopping() {
			log.Info("stopping")
			return nil
		}

		orgs, err := scope.Active(hard)
		if err != nil {
			// A tick with nothing to do, never a list of nobody: a failed read
			// must not look like a quiet database. The next tick tries again.
			log.Error("the organizations with work could not be listed", "error", err)
			orgs = nil
		}

		for _, org := range rotation.Order(orgs) {
			if stopping() {
				break
			}
			if !slots.TryAcquire(org) {
				continue
			}
			rotation.Served(org)

			inflight.Add(1)
			go func() {
				defer inflight.Done()
				defer slots.Release(org)

				if attempt(hard, processor, org, log) {
					select {
					case finished <- struct{}{}:
					default:
					}
				}
			}()
		}

		select {
		case <-drain.Done():
		case <-hard.Done():
		case <-ticker.C:
		case <-finished:
		}
	}
}

// attempt takes the next run of one organization, if there is one, executes it,
// and says whether there was one.
func attempt(ctx context.Context, processor Processor, org uuid.UUID, log *slog.Logger) bool {
	// Not where the lines are stored: the service does that as they arrive, so
	// that the live view has them. These are the same lines, forwarded here for
	// a trace of what a run printed.
	logs := make(chan domain.LogLine, 256)
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for line := range logs {
			log.Debug("generator", "org_id", org, "stream", line.Stream, "message", line.Message)
		}
	}()

	worked, err := processor.ProcessNext(ctx, org, logs)
	close(logs)
	<-drained

	if err != nil {
		// A failed run must not take the worker down with it.
		log.Error("processing a run failed", "org_id", org, "error", err)
	}
	return worked
}
