package runner

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type policy struct {
	minimum int
	err     error
}

func (p policy) MinimumContract(context.Context, uuid.UUID) (int, error) { return p.minimum, p.err }

// A worker at the minimum, or above it, may take a run.
func TestAWorkerAtOrAboveTheMinimumIsAdmitted(t *testing.T) {
	for _, c := range []struct {
		name              string
		contract, minimum int
	}{
		{"at the minimum", 3, 3},
		{"above the minimum", 4, 3},
		{"the first contract against the first minimum", 1, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			admitted, _, err := NewGate(policy{minimum: c.minimum}, c.contract).Admit(context.Background(), uuid.New())
			if err != nil {
				t.Fatalf("admit: %v", err)
			}
			if !admitted {
				t.Fatalf("a worker at contract %d was refused against a minimum of %d", c.contract, c.minimum)
			}
		})
	}
}

// The one the minimum exists for: an older worker stays up, and takes nothing.
func TestAWorkerBelowTheMinimumIsRefusedAndTheMinimumIsSaid(t *testing.T) {
	admitted, minimum, err := NewGate(policy{minimum: 3}, 2).Admit(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if admitted {
		t.Fatal("a worker below the minimum was admitted")
	}
	if minimum != 3 {
		t.Fatalf("minimum = %d, want 3: the caller says it, and a refusal that does not name the number is a puzzle", minimum)
	}
}

// If the minimum cannot be read, nothing is taken. A worker that took runs on
// the strength of a number it could not check would be no gate at all, and the
// run it did not take stays queued, intact, for the next beat.
func TestAMinimumThatCannotBeReadAdmitsNobody(t *testing.T) {
	admitted, _, err := NewGate(policy{err: errors.New("the database is gone")}, 5).Admit(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("a failed read was not reported")
	}
	if admitted {
		t.Fatal("a worker was admitted without the minimum being known")
	}
}
