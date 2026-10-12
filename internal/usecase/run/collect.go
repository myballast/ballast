package run

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// collectedMessage is what a collected run says about itself afterwards. It is
// stored on the run, so somebody reading their history months later learns what
// happened rather than finding an empty failure.
const collectedMessage = "the worker executing this run stopped reporting; the run was collected"

// Collect ends the runs nobody is watching any more, and reports how many it
// ended.
//
// The case it exists for: a worker is killed mid-run — the process, the
// machine, the whole host — and nothing else in Ballast would ever move that
// run out of running. Its target would stay held until the lock expired, and
// its container would keep generating load until its own time limit ended it.
//
// Two steps, and the order between them is the rule. A run whose heartbeat has
// gone stale becomes orphaned, which means "nobody is watching this, and it may
// well still be running". Only once what it left behind is destroyed does it
// become errored, which means it is over.
func (s *Service) Collect(ctx context.Context, orgID uuid.UUID) (int, error) {
	// Stated in the domain's terms rather than left to the SQL: the statement
	// below performs this transition, and the state machine is where it is
	// decided whether that is allowed.
	if !domain.StatusRunning.CanTransitionTo(domain.StatusOrphaned) {
		return 0, errors.New("a running run may not be orphaned")
	}

	abandoned, err := s.runs.OrphanStale(ctx, orgID, domain.StaleBefore(s.clock.Now()))
	if err != nil {
		return 0, fmt.Errorf("look for abandoned runs: %w", err)
	}
	for _, found := range abandoned {
		s.log.ErrorContext(ctx, "a run was abandoned by its worker",
			"run_id", found.ID, "environment_id", found.EnvironmentID,
			"last_seen", found.HeartbeatAt)
	}

	// Read back rather than carried over from the call above. A collector that
	// died between the two steps left runs orphaned, and no later call to
	// OrphanStale would ever return them again: they are no longer running.
	waiting, err := s.runs.Orphaned(ctx, orgID)
	if err != nil {
		return 0, fmt.Errorf("list the abandoned runs: %w", err)
	}

	var ended int
	for _, orphan := range waiting {
		if err := s.end(ctx, orgID, orphan); err != nil {
			// One run that cannot be cleaned up must not stop the others from
			// being: it stays orphaned and the next pass tries again.
			s.log.ErrorContext(ctx, "an abandoned run could not be collected",
				"run_id", orphan.ID, "error", err)
			continue
		}
		ended++
	}

	return ended, nil
}

// end destroys what one abandoned run left on the machine it ran on, and then
// records that the run is over.
//
// That order, and not the other one. errored says the load has stopped: written
// first, it would be a lie for as long as the container lived, and it frees the
// target — so the next run against it would measure this one's traffic as well
// as its own and nothing in the numbers would say so.
func (s *Service) end(ctx context.Context, orgID uuid.UUID, orphan domain.Run) error {
	if !orphan.Status.CanTransitionTo(domain.StatusErrored) {
		return fmt.Errorf("a run cannot go from %s to %s", orphan.Status, domain.StatusErrored)
	}

	if err := s.cleaner.Destroy(ctx, orphan.ID); err != nil {
		return fmt.Errorf("destroy what the run left behind: %w", err)
	}

	err := s.runs.FailOrphaned(ctx, orgID, orphan.ID, s.clock.Now(), collectedMessage)
	if errors.Is(err, domain.ErrNotFound) {
		// Another collector ended it between the list and here. Both destroyed
		// the same leftovers, which is work done twice rather than work undone.
		return nil
	}
	if err != nil {
		return err
	}

	s.log.InfoContext(ctx, "an abandoned run was collected",
		"run_id", orphan.ID, "environment_id", orphan.EnvironmentID)

	return nil
}

// Sweep destroys everything on this machine that belongs to a run no longer
// being watched, and reports how many it destroyed.
//
// It runs once, when a worker starts. A worker that was killed mid-run left a
// generator behind, and that generator does not stop when its worker does: it
// keeps loading somebody's target until its own time limit ends it. Collect
// handles the runs it can see in the database; this handles what is on the
// machine, including a generator whose run was collected by somebody else while
// this machine was down.
//
// Its scope is worth stating plainly, because it is what makes it correct: it
// can see the machine this worker generates load from, and it can read one
// organization's runs, since row-level security shows it no others. Everything
// it cannot account for, it destroys. That holds because a worker serves one
// organization and that machine runs its runs and nothing else — two workers
// for two organizations sharing one would destroy each other's generators.
func (s *Service) Sweep(ctx context.Context, orgID uuid.UUID) (int, error) {
	live, err := s.runs.LiveRunIDs(ctx, orgID, domain.StaleBefore(s.clock.Now()))
	if err != nil {
		return 0, fmt.Errorf("list the live runs: %w", err)
	}

	destroyed, err := s.cleaner.DestroyExcept(ctx, live)
	if err != nil {
		return destroyed, fmt.Errorf("destroy what no live run owns: %w", err)
	}

	return destroyed, nil
}
