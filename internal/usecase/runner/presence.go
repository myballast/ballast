package runner

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// farewell is how long a worker that is leaving waits for its last word to be
// written. A worker that cannot reach its database on the way out is not going
// to be kept alive by it.
const farewell = 5 * time.Second

// Where lists the organizations a worker announces itself in. A Scope is one.
type Where interface {
	Present(ctx context.Context) ([]uuid.UUID, error)
}

// Presence is a worker saying, to the database, that it is there.
//
// It is the worker's own doing and nobody else's: nothing registers a worker and
// nothing is issued to it. Its credential is the role it already connects with
// (docs/adr/0001-runner-connection.md).
//
// A worker that serves several organizations is one runner in each of them: the
// Runners screen of an organization reads its own rows. The list is read at every
// beat, so an organization created since is joined at the next one.
type Presence struct {
	repo  Repository
	clock Clock
	where Where
	self  domain.Runner

	mu sync.Mutex
	// announced is every organization this worker has said something in, so that
	// its farewell reaches all of them even when the list can no longer be read.
	announced map[uuid.UUID]struct{}
}

// NewPresence describes this worker. It refuses one the table would refuse,
// before anything is written.
func NewPresence(
	repo Repository, clock Clock, where Where, name, version string, contract int,
) (*Presence, error) {
	now := clock.Now()
	self := domain.Runner{
		// Set per organization when it is written; a placeholder is enough to
		// check the rest of the description now.
		OrgID:       uuid.Nil,
		Name:        name,
		Version:     version,
		Contract:    contract,
		State:       domain.RunnerServing,
		StartedAt:   now,
		HeartbeatAt: now,
	}
	check := self
	check.OrgID = uuid.New()
	if err := check.Validate(); err != nil {
		return nil, err
	}

	return &Presence{
		repo: repo, clock: clock, where: where, self: self,
		announced: make(map[uuid.UUID]struct{}),
	}, nil
}

// Serving says the worker is taking runs. Called at every beat: it moves the
// heartbeat and nothing else.
func (p *Presence) Serving(ctx context.Context) error {
	return p.report(ctx, domain.RunnerServing)
}

// Draining says the worker was told to stop, takes nothing new, and is letting
// the runs it is executing finish. It is still alive, and still beats.
func (p *Presence) Draining(ctx context.Context) error {
	return p.report(ctx, domain.RunnerDraining)
}

// Stopped says the worker left. It is written while the worker is stopping, so
// the context it is given is usually cancelled already; the write does not
// depend on it, or a worker would look online for thirty seconds after it left.
// It goes to every organization the worker announced itself in, and does not read
// the list for that: a worker leaving because its database is failing still has
// to say so where it can.
func (p *Presence) Stopped(ctx context.Context) error {
	last, cancel := context.WithTimeout(context.WithoutCancel(ctx), farewell)
	defer cancel()

	p.mu.Lock()
	orgs := make([]uuid.UUID, 0, len(p.announced))
	for id := range p.announced {
		orgs = append(orgs, id)
	}
	p.mu.Unlock()

	// A worker that never got to announce itself has nothing to take back, but
	// its row may be there from an earlier life: ask the list, as a beat would.
	if len(orgs) == 0 {
		return p.report(last, domain.RunnerStopped)
	}
	return p.write(last, orgs, domain.RunnerStopped)
}

func (p *Presence) report(ctx context.Context, state domain.RunnerState) error {
	orgs, err := p.where.Present(ctx)
	if err != nil {
		return fmt.Errorf("list the organizations to report in: %w", err)
	}
	return p.write(ctx, orgs, state)
}

// write says the same thing in each organization. One failing is no reason for
// the others to look offline: every one is tried, and the failures come back
// together.
func (p *Presence) write(ctx context.Context, orgs []uuid.UUID, state domain.RunnerState) error {
	now := p.clock.Now()

	var failures []error
	for _, org := range orgs {
		runner := p.self
		runner.OrgID = org
		runner.State = state
		runner.HeartbeatAt = now

		if err := runner.Validate(); err != nil {
			failures = append(failures, err)
			continue
		}
		if err := p.repo.Report(ctx, org, runner); err != nil {
			failures = append(failures, fmt.Errorf("report that the runner is %s in %s: %w", state, org, err))
			continue
		}

		p.mu.Lock()
		p.announced[org] = struct{}{}
		p.mu.Unlock()
	}
	return errors.Join(failures...)
}
