//go:build integration

package postgres_test

import (
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres"
)

// An organization that nobody has configured accepts every worker: the first
// contract is the first minimum.
func TestTheMinimumContractStartsAtOne(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)

	minimum, err := postgres.NewRunnerPolicyRepository(f.db).MinimumContract(ctx, f.orgA)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if minimum != 1 {
		t.Fatalf("minimum = %d, want 1", minimum)
	}
}

// The minimum of one organization is not the minimum of another.
func TestTheMinimumContractIsPerOrganization(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	policy := postgres.NewRunnerPolicyRepository(f.db)

	err := f.db.InTenantTx(ctx, f.orgA, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE organization SET min_runner_contract = 4 WHERE id = $1`, f.orgA)
		return err
	})
	if err != nil {
		t.Fatalf("raise the minimum: %v", err)
	}

	a, err := policy.MinimumContract(ctx, f.orgA)
	if err != nil {
		t.Fatalf("read alpha: %v", err)
	}
	b, err := policy.MinimumContract(ctx, f.orgB)
	if err != nil {
		t.Fatalf("read beta: %v", err)
	}
	if a != 4 || b != 1 {
		t.Fatalf("minimums = %d and %d, want 4 and 1", a, b)
	}
}

// A minimum under one would admit a worker that has no contract at all.
func TestAMinimumUnderOneIsRefusedByTheTable(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)

	err := f.db.InTenantTx(ctx, f.orgA, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE organization SET min_runner_contract = 0 WHERE id = $1`, f.orgA)
		return err
	})
	if err == nil {
		t.Fatal("a minimum of zero was accepted")
	}
}
