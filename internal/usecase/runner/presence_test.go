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
	// reportedIn is the organization of each report, in the same order.
	reportedIn []uuid.UUID
	// failFor makes the report for one organization fail and no other.
	failFor map[uuid.UUID]error
	// ctxErr is ctx.Err() at each call: a report written with a context that
	// was already cancelled would be refused by a real database.
	ctxErr []error
	fail   error
	// listed is what List returns, and listCalls how often it was asked.
	listed    []domain.Runner
	listCalls int
}

func (s *store) Report(ctx context.Context, orgID uuid.UUID, runner domain.Runner) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ctxErr = append(s.ctxErr, ctx.Err())
	if s.fail != nil {
		return s.fail
	}
	if err := s.failFor[orgID]; err != nil {
		return err
	}
	s.reported = append(s.reported, runner)
	s.reportedIn = append(s.reportedIn, orgID)
	return nil
}

// reportsFor is what one organization was told, oldest first.
func (s *store) reportsFor(org uuid.UUID) []domain.Runner {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []domain.Runner
	for i, in := range s.reportedIn {
		if in == org {
			out = append(out, s.reported[i])
		}
	}
	return out
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

	presence, err := NewPresence(repo, clk, OnlyOrganization(uuid.New()), "vps-1", "a1b2c3d", domain.RunnerContract)
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

	if _, err := NewPresence(repo, clk, OnlyOrganization(uuid.New()), "", "a1b2c3d", domain.RunnerContract); err == nil {
		t.Fatal("a runner with no name was accepted")
	}
	if len(repo.reported) != 0 {
		t.Fatal("something was written for a runner that was refused")
	}
}

// presentIn is the organizations a worker announces itself in, changeable between
// two beats the way the real list is when somebody signs up.
type presentIn struct {
	orgs []uuid.UUID
	err  error
}

func (p *presentIn) Present(context.Context) ([]uuid.UUID, error) { return p.orgs, p.err }

func newPresenceFor(t *testing.T, where *presentIn) (*Presence, *store) {
	t.Helper()

	repo := &store{}
	clk := &clock{now: time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)}

	presence, err := NewPresence(repo, clk, where, "vps-1", "a1b2c3d", domain.RunnerContract)
	if err != nil {
		t.Fatalf("new presence: %v", err)
	}
	return presence, repo
}

// A worker that serves every organization is one runner in each of them: the
// Runners screen of an organization reads its own rows, and an organization with
// nothing queued still has to see its worker online.
func TestAWorkerForSeveralOrganizationsSaysItIsAliveInEachOfThem(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	presence, repo := newPresenceFor(t, &presentIn{orgs: []uuid.UUID{a, b}})

	if err := presence.Serving(context.Background()); err != nil {
		t.Fatalf("serving: %v", err)
	}

	for _, org := range []uuid.UUID{a, b} {
		got := repo.reportsFor(org)
		if len(got) != 1 || got[0].State != domain.RunnerServing || got[0].Name != "vps-1" {
			t.Fatalf("organization %s was told %+v, want one serving report from vps-1", org, got)
		}
	}
}

// Somebody signs up between two beats. The next beat includes them: a worker that
// listed its organizations once, at startup, would be invisible to every customer
// who joined after.
func TestAnOrganizationCreatedBetweenTwoBeatsIsJoinedAtTheNext(t *testing.T) {
	a, late := uuid.New(), uuid.New()
	where := &presentIn{orgs: []uuid.UUID{a}}
	presence, repo := newPresenceFor(t, where)

	_ = presence.Serving(context.Background())
	if got := repo.reportsFor(late); len(got) != 0 {
		t.Fatalf("the organization was told something before it existed: %+v", got)
	}

	where.orgs = []uuid.UUID{a, late}
	_ = presence.Serving(context.Background())

	if got := repo.reportsFor(late); len(got) != 1 {
		t.Fatalf("the new organization was told %d time(s), want once", len(got))
	}
}

// One organization's row failing to write is no reason for the others to look
// offline. The error still comes back: it is not swallowed, it is not allowed to
// take the rest down.
func TestOneOrganizationFailingDoesNotSilenceTheOthers(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	presence, repo := newPresenceFor(t, &presentIn{orgs: []uuid.UUID{a, b}})
	repo.failFor = map[uuid.UUID]error{a: errors.New("a write failed")}

	if err := presence.Serving(context.Background()); err == nil {
		t.Fatal("a failed report was not returned")
	}
	if got := repo.reportsFor(b); len(got) != 1 {
		t.Fatalf("organization b was told %d time(s); a failure in a must not stop it", len(got))
	}
}

// A list that cannot be read is no list: nothing is guessed, and the error comes
// back so the beat logs it and tries again.
func TestAnUnreadableListOfOrganizationsIsAnError(t *testing.T) {
	presence, repo := newPresenceFor(t, &presentIn{err: errors.New("the database is gone")})

	if err := presence.Serving(context.Background()); err == nil {
		t.Fatal("an unreadable list was reported as a success")
	}
	if len(repo.reported) != 0 {
		t.Fatal("something was written with no list to write to")
	}
}

// The farewell goes to every organization the worker announced itself in, and it
// does not need the list for that: a worker that is leaving because its database
// is gone still has to say it left where it can.
func TestAStoppedWorkerSaysSoInEveryOrganizationItAnnouncedItselfIn(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	where := &presentIn{orgs: []uuid.UUID{a, b}}
	presence, repo := newPresenceFor(t, where)

	_ = presence.Serving(context.Background())
	where.err = errors.New("the database is gone")

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := presence.Stopped(cancelled); err != nil {
		t.Fatalf("stopped: %v", err)
	}

	for _, org := range []uuid.UUID{a, b} {
		got := repo.reportsFor(org)
		if len(got) != 2 || got[1].State != domain.RunnerStopped {
			t.Fatalf("organization %s: %+v, want serving then stopped", org, got)
		}
	}
}
