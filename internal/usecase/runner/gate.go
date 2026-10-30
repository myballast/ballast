package runner

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Policy holds the minimum an organization holds its workers to.
type Policy interface {
	// MinimumContract is the oldest contract the organization still hands a run
	// to. An organization nobody configured answers 1.
	MinimumContract(ctx context.Context, orgID uuid.UUID) (int, error)
}

// Gate is the question a worker asks just before it claims a run: am I new
// enough to be given one?
//
// A worker that is not stays up, keeps saying it is alive, and takes nothing.
// What it did not take stays queued, intact.
type Gate struct {
	policy   Policy
	contract int
}

// NewGate gates a worker that speaks the given contract.
func NewGate(policy Policy, contract int) *Gate {
	return &Gate{policy: policy, contract: contract}
}

// Admit says whether this worker may claim a run, and returns the minimum so the
// caller can name it: a refusal that does not say the number is a puzzle.
//
// When the minimum cannot be read nobody is admitted. A worker that took runs on
// the strength of a number it could not check would be no gate at all.
//
// The read comes just before the claim and not in the same transaction: a worker
// that read the old minimum in the instant before somebody raised it may take
// one more run. The minimum is raised rarely and on purpose, and that is the
// price of leaving the claim as it is.
func (g *Gate) Admit(ctx context.Context, orgID uuid.UUID) (bool, int, error) {
	minimum, err := g.policy.MinimumContract(ctx, orgID)
	if err != nil {
		return false, 0, fmt.Errorf("read the minimum contract: %w", err)
	}

	self := domain.Runner{Contract: g.contract}
	return !self.Outdated(minimum), minimum, nil
}
