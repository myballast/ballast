package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres/sqlc"
	"github.com/youssefmahersi/ballast/internal/usecase/run"
)

// What the rate limits read.
//
// Counted from the runs themselves rather than from a counter kept in the API
// process. A counter in memory is wrong the moment a second process exists and
// forgets everything when one restarts, which is exactly the moment somebody
// abusing the thing would choose. The rows are already here, and the two
// indexes of migration 011 make the questions cheap.

// Queued is how many runs an organization has waiting.
func (r *RunRepository) Queued(ctx context.Context, orgID uuid.UUID) (int, error) {
	var waiting int64

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		count, err := sqlc.New(tx).CountQueuedRuns(ctx, orgID)
		if err != nil {
			return fmt.Errorf("count the runs waiting: %w", err)
		}

		waiting = count
		return nil
	})

	return int(waiting), err
}

// TriggeredSince counts the runs started since an instant, by the organization
// and by one of its people.
//
// Both in one statement: a limit asks the two at the same moment, and two
// round trips would let the answers disagree about where the window starts.
func (r *RunRepository) TriggeredSince(
	ctx context.Context, orgID, userID uuid.UUID, since time.Time,
) (run.Triggered, error) {
	var triggered run.Triggered

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		counted, err := sqlc.New(tx).CountTriggeredSince(ctx, sqlc.CountTriggeredSinceParams{
			OrgID:       orgID,
			TriggeredBy: &userID,
			CreatedAt:   since,
		})
		if err != nil {
			return fmt.Errorf("count the runs triggered: %w", err)
		}

		triggered = run.Triggered{ByOrg: int(counted.ByOrg), ByUser: int(counted.ByUser)}
		return nil
	})

	return triggered, err
}

// OldestSince is when the earliest run of a window was created.
//
// A zero user asks about the whole organization, which is how the same
// question serves both limits. It reports false when the window holds nothing:
// a caller is told to wait only when there is something to wait for.
func (r *RunRepository) OldestSince(
	ctx context.Context, orgID, userID uuid.UUID, since time.Time,
) (time.Time, bool, error) {
	var (
		oldest time.Time
		found  bool
	)

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		queries := sqlc.New(tx)

		var at time.Time
		var err error
		if userID == uuid.Nil {
			at, err = queries.OldestRunSince(ctx, sqlc.OldestRunSinceParams{
				OrgID:     orgID,
				CreatedAt: since,
			})
		} else {
			at, err = queries.OldestRunByUserSince(ctx, sqlc.OldestRunByUserSinceParams{
				OrgID:       orgID,
				TriggeredBy: &userID,
				CreatedAt:   since,
			})
		}

		// Nothing in the window is an answer, not a failure: there is simply
		// nobody ahead of this caller.
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("find the oldest run of the window: %w", err)
		}

		oldest, found = at, true
		return nil
	})

	return oldest, found, err
}
