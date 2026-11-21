//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// What the limits read about a pipeline, against a real database: its own runs,
// in the right window, in the right organization. A token has no person, so a
// person's count is neither what holds it nor what spares it.
//
// Written before the code, which is why they do not compile yet.

// token puts an API token in the world's project and returns its id.
func (w queueWorld) token(ctx context.Context, t *testing.T, name string) uuid.UUID {
	t.Helper()

	id := uuid.New()
	if err := w.db.InTenantTx(ctx, w.org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO api_token (id, org_id, project_id, name, prefix, token_hash, expires_at)
			VALUES ($1, $2, $3, $4, 'blt_testtest', $5, now() + interval '90 days')`,
			id, w.org, w.scenario.ProjectID, name, "hash-"+id.String())
		return err
	}); err != nil {
		t.Fatalf("seed a token: %v", err)
	}
	return id
}

// launchedBy backdates a run's creation and says a token launched it, which is
// how a window is staged.
func (w queueWorld) launchedBy(ctx context.Context, t *testing.T, run domain.Run, token uuid.UUID, at time.Time) {
	t.Helper()

	if err := w.db.InTenantTx(ctx, w.org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE run SET created_at = $3, triggered_by = NULL, triggered_by_token = $4 WHERE org_id = $1 AND id = $2`,
			w.org, run.ID, at, token)
		return err
	}); err != nil {
		t.Fatalf("backdate the run: %v", err)
	}
}

// The token's own runs, and the organization's total, from one question. A
// person's runs count toward the total and toward nobody's token, and the
// boundary belongs to the window.
func TestATokensRunsAreCountedByTheToken(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	since := now.Add(-domain.RateWindow)
	mine, other := world.token(ctx, t, "mine"), world.token(ctx, t, "other")

	world.launchedBy(ctx, t, world.queue(ctx, t, world.env), mine, now.Add(-30*time.Minute))
	world.launchedBy(ctx, t, world.queue(ctx, t, world.env), mine, since)
	// An hour and a second ago: outside the window.
	world.launchedBy(ctx, t, world.queue(ctx, t, world.env), mine, since.Add(-time.Second))
	// Another pipeline's, and a person's: in the total, in nobody else's count.
	world.launchedBy(ctx, t, world.queue(ctx, t, world.env), other, now.Add(-time.Minute))
	world.triggeredAt(ctx, t, world.queue(ctx, t, world.env), world.user, now.Add(-time.Minute))

	counted, err := world.runs.TriggeredByTokenSince(ctx, world.org, mine, since)
	if err != nil {
		t.Fatalf("TriggeredByTokenSince: %v", err)
	}

	if counted.ByToken != 2 {
		t.Errorf("the token launched %d runs in the window, want 2", counted.ByToken)
	}
	if counted.ByOrg != 4 {
		t.Errorf("the organization triggered %d runs in the window, want 4", counted.ByOrg)
	}
	if counted.ByUser != 0 {
		t.Errorf("the question about a token filled in a person's count: %d", counted.ByUser)
	}
}

func TestAnotherOrganizationsTokenRunsAreNotCounted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	theirs := newQueueWorld(ctx, t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	since := now.Add(-domain.RateWindow)
	token := theirs.token(ctx, t, "theirs")
	for range 3 {
		theirs.launchedBy(ctx, t, theirs.queue(ctx, t, theirs.env), token, now.Add(-time.Minute))
	}

	ours := newQueueWorld(ctx, t)
	counted, err := ours.runs.TriggeredByTokenSince(ctx, ours.org, token, since)
	if err != nil {
		t.Fatalf("TriggeredByTokenSince: %v", err)
	}
	if counted.ByToken != 0 || counted.ByOrg != 0 {
		t.Errorf("another organization's runs were counted: %+v", counted)
	}
}

// The wait a pipeline is told is when its own oldest run leaves the window.
func TestTheOldestRunOfATokensWindowIsItsOwn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	since := now.Add(-domain.RateWindow)
	mine, other := world.token(ctx, t, "mine"), world.token(ctx, t, "other")

	world.launchedBy(ctx, t, world.queue(ctx, t, world.env), mine, now.Add(-20*time.Minute))
	world.launchedBy(ctx, t, world.queue(ctx, t, world.env), mine, now.Add(-45*time.Minute))
	// Older, and another pipeline's: not what this token waits for.
	world.launchedBy(ctx, t, world.queue(ctx, t, world.env), other, now.Add(-55*time.Minute))
	// Older than the window: it has left it already.
	world.launchedBy(ctx, t, world.queue(ctx, t, world.env), mine, since.Add(-time.Minute))

	at, found, err := world.runs.OldestByTokenSince(ctx, world.org, mine, since)
	if err != nil || !found {
		t.Fatalf("OldestByTokenSince: %v (found=%v)", err, found)
	}
	if want := now.Add(-45 * time.Minute); !at.Equal(want) {
		t.Errorf("the oldest run is from %s, want %s", at, want)
	}

	// A token with nothing in the window has nobody to wait for.
	if _, found, err := world.runs.OldestByTokenSince(ctx, world.org, world.token(ctx, t, "idle"), since); err != nil || found {
		t.Errorf("an idle token: found=%v err=%v, want nothing to wait for", found, err)
	}
}

// A run names its token by an identifier, and the screen says its name.
func TestATokenIsCalledByItsNameAndOnlyInItsOwnOrganization(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	ours, theirs := newQueueWorld(ctx, t), newQueueWorld(ctx, t)
	token := ours.token(ctx, t, "github-main")

	if name, err := ours.runs.TokenName(ctx, ours.org, token); err != nil || name != "github-main" {
		t.Errorf("TokenName = %q, %v, want its name", name, err)
	}
	// Gone, unknown or another organization's: no name, and no error. Nobody
	// learns from a failure that the id exists somewhere else.
	if name, err := ours.runs.TokenName(ctx, ours.org, uuid.New()); err != nil || name != "" {
		t.Errorf("an unknown token: %q, %v, want no name", name, err)
	}
	if name, err := theirs.runs.TokenName(ctx, theirs.org, token); err != nil || name != "" {
		t.Errorf("another organization's token: %q, %v, want no name", name, err)
	}
}
