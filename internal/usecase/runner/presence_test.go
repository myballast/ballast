package runner

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

// store keeps what was reported, and what the context looked like when it was.
type store struct {
	mu       sync.Mutex
	reported []domain.Runner
	// ctxErr is ctx.Err() at each call: a report written with a context that
	// was already cancelled would be refused by a real database.
	ctxErr []error
	fail   error
	// listed is what List returns, and listCalls how often it was asked.
	listed    []domain.Runner
	listCalls int
}

func (s *store) Report(ctx context.Context, _ uuid.UUID, runner domain.Runner) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ctxErr = append(s.ctxErr, ctx.Err())
	if s.fail != nil {
		return s.fail
	}
	s.reported = append(s.reported, runner)
	return nil
}

func (s *store) List(context.Context, uuid.UUID) ([]domain.Runner, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.listCalls++
	return s.listed, nil
}

func (s *store) last() domain.Runner {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reported[len(s.reported)-1]
}

func newPresence(t *testing.T) (*Presence, *store, *clock) {
	t.Helper()

	repo := &store{}
	clk := &clock{now: time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)}

	presence, err := NewPresence(repo, clk, uuid.New(), "vps-1", "a1b2c3d", domain.RunnerContract)
	if err != nil {
		t.Fatalf("new presence: %v", err)
	}
	return presence, repo, clk
}

func TestAServingRunnerSaysWhoItIsAndWhenItStarted(t *testing.T) {
	presence, repo, clk := newPresence(t)
	startedAt := clk.now

	clk.now = clk.now.Add(10 * time.Second)
	if err := presence.Serving(context.Background()); err != nil {
		t.Fatalf("serving: %v", err)
	}

	got := repo.last()
	if got.Name != "vps-1" || got.Version != "a1b2c3d" || got.Contract != domain.RunnerContract {
		t.Fatalf("the report does not say who the runner is: %+v", got)
	}
	if got.State != domain.RunnerServing {
		t.Fatalf("state = %q, want serving", got.State)
	}
	if !got.StartedAt.Equal(startedAt) {
		t.Fatalf("started at %s: it moves with every beat, but it is when the process started (%s)",
			got.StartedAt, startedAt)
	}
	if !got.HeartbeatAt.Equal(clk.now) {
		t.Fatalf("heartbeat at %s, want the instant of the report (%s)", got.HeartbeatAt, clk.now)
	}
}

func TestEachBeatMovesTheHeartbeatAndNothingElse(t *testing.T) {
	presence, repo, clk := newPresence(t)

	_ = presence.Serving(context.Background())
	first := repo.last()

	clk.now = clk.now.Add(10 * time.Second)
	_ = presence.Serving(context.Background())
	second := repo.last()

	if !second.HeartbeatAt.After(first.HeartbeatAt) {
		t.Fatal("the heartbeat did not move")
	}
	if !second.StartedAt.Equal(first.StartedAt) || second.Name != first.Name {
		t.Fatal("a beat changed what the runner is")
	}
}

func TestADrainingRunnerSaysSo(t *testing.T) {
	presence, repo, _ := newPresence(t)

	if err := presence.Draining(context.Background()); err != nil {
		t.Fatalf("draining: %v", err)
	}
	if got := repo.last().State; got != domain.RunnerDraining {
		t.Fatalf("state = %q, want draining", got)
	}
}

// The last thing a worker does is write that it is gone, and it does that while
// stopping: the context it was given is by then cancelled. A farewell that a
// cancelled context refuses is a worker that looks online for thirty seconds
// after it left.
func TestAStoppedRunnerWritesItEvenWithACancelledContext(t *testing.T) {
	presence, repo, _ := newPresence(t)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	if err := presence.Stopped(cancelled); err != nil {
		t.Fatalf("stopped: %v", err)
	}
	if got := repo.last().State; got != domain.RunnerStopped {
		t.Fatalf("state = %q, want stopped", got)
	}
	if repo.ctxErr[len(repo.ctxErr)-1] != nil {
		t.Fatalf("the report went out on a cancelled context: %v", repo.ctxErr[len(repo.ctxErr)-1])
	}
}

func TestAFailedReportIsReturnedNotSwallowed(t *testing.T) {
	presence, repo, _ := newPresence(t)
	repo.fail = errors.New("the database is gone")

	if err := presence.Serving(context.Background()); err == nil {
		t.Fatal("a report that failed was not reported")
	}
}

func TestARunnerThatCannotBeStoredIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	repo := &store{}
	clk := &clock{now: time.Now()}

	if _, err := NewPresence(repo, clk, uuid.New(), "", "a1b2c3d", domain.RunnerContract); err == nil {
		t.Fatal("a runner with no name was accepted")
	}
	if len(repo.reported) != 0 {
		t.Fatal("something was written for a runner that was refused")
	}
}
