//go:build integration

package postgres_test

import (
	"testing"
	"time"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres"
	"github.com/youssefmahersi/ballast/internal/domain"
)

func aReport(name string, at time.Time) domain.Runner {
	return domain.Runner{
		Name:        name,
		Version:     "a1b2c3d",
		Contract:    domain.RunnerContract,
		State:       domain.RunnerServing,
		StartedAt:   at.Add(-time.Hour),
		HeartbeatAt: at,
	}
}

// Announcing and beating are the same write: a runner that restarts under its
// name takes its row back, and a row that was deleted comes back with the next
// beat.
func TestReportingTwiceKeepsOneRowAndMovesTheHeartbeat(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	repo := postgres.NewRunnerRepository(f.db)

	at := time.Now().UTC().Truncate(time.Millisecond)

	if err := repo.Report(ctx, f.orgA, aReport("vps-1", at)); err != nil {
		t.Fatalf("first report: %v", err)
	}

	later := aReport("vps-1", at.Add(10*time.Second))
	later.State = domain.RunnerDraining
	if err := repo.Report(ctx, f.orgA, later); err != nil {
		t.Fatalf("second report: %v", err)
	}

	runners, err := repo.List(ctx, f.orgA)
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	var found []domain.Runner
	for _, r := range runners {
		if r.Name == "vps-1" {
			found = append(found, r)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d rows for one runner name, want 1", len(found))
	}
	if found[0].State != domain.RunnerDraining || !found[0].HeartbeatAt.Equal(later.HeartbeatAt) {
		t.Fatalf("the second report did not replace the first: %+v", found[0])
	}
}

// What one organization's runner says is nobody else's to read.
func TestARunnerIsNotListedForAnotherOrganization(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	repo := postgres.NewRunnerRepository(f.db)

	if err := repo.Report(ctx, f.orgA, aReport("only-in-alpha", time.Now().UTC())); err != nil {
		t.Fatalf("report: %v", err)
	}

	runners, err := repo.List(ctx, f.orgB)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, r := range runners {
		if r.Name == "only-in-alpha" {
			t.Fatal("a runner of another organization was listed")
		}
	}
}

// The same name in two organizations is two runners.
func TestTwoOrganizationsMayUseTheSameRunnerName(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	repo := postgres.NewRunnerRepository(f.db)

	at := time.Now().UTC()
	if err := repo.Report(ctx, f.orgA, aReport("shared-name", at)); err != nil {
		t.Fatalf("report for alpha: %v", err)
	}
	if err := repo.Report(ctx, f.orgB, aReport("shared-name", at)); err != nil {
		t.Fatalf("report for beta: %v", err)
	}
}
