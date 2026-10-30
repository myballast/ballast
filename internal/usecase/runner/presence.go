package runner

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// farewell is how long a worker that is leaving waits for its last word to be
// written. A worker that cannot reach its database on the way out is not going
// to be kept alive by it.
const farewell = 5 * time.Second

// Presence is a worker saying, to the database, that it is there.
//
// It is the worker's own doing and nobody else's: nothing registers a worker and
// nothing is issued to it. Its credential is the role it already connects with
// (docs/adr/0001-runner-connection.md).
type Presence struct {
	repo  Repository
	clock Clock
	orgID uuid.UUID
	self  domain.Runner
}

// NewPresence describes this worker. It refuses one the table would refuse,
// before anything is written.
func NewPresence(
	repo Repository, clock Clock, orgID uuid.UUID, name, version string, contract int,
) (*Presence, error) {
	now := clock.Now()
	self := domain.Runner{
		OrgID:       orgID,
		Name:        name,
		Version:     version,
		Contract:    contract,
		State:       domain.RunnerServing,
		StartedAt:   now,
		HeartbeatAt: now,
	}
	if err := self.Validate(); err != nil {
		return nil, err
	}

	return &Presence{repo: repo, clock: clock, orgID: orgID, self: self}, nil
}

// Serving says the worker is taking runs. Called at every beat: it moves the
// heartbeat and nothing else.
func (p *Presence) Serving(ctx context.Context) error {
	return p.report(ctx, domain.RunnerServing)
}

// Draining says the worker was told to stop, takes nothing new, and is letting
// the run it is executing finish. It is still alive, and still beats.
func (p *Presence) Draining(ctx context.Context) error {
	return p.report(ctx, domain.RunnerDraining)
}

// Stopped says the worker left. It is written while the worker is stopping, so
// the context it is given is usually cancelled already; the write does not
// depend on it, or a worker would look online for thirty seconds after it left.
func (p *Presence) Stopped(ctx context.Context) error {
	last, cancel := context.WithTimeout(context.WithoutCancel(ctx), farewell)
	defer cancel()

	return p.report(last, domain.RunnerStopped)
}

func (p *Presence) report(ctx context.Context, state domain.RunnerState) error {
	runner := p.self
	runner.State = state
	runner.HeartbeatAt = p.clock.Now()

	if err := runner.Validate(); err != nil {
		return err
	}
	if err := p.repo.Report(ctx, p.orgID, runner); err != nil {
		return fmt.Errorf("report that the runner is %s: %w", state, err)
	}
	return nil
}
