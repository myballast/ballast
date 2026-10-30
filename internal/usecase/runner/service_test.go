package runner

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

type directory struct {
	ids []uuid.UUID
	err error
}

func (d directory) ProjectIDs(context.Context, uuid.UUID) ([]uuid.UUID, error) { return d.ids, d.err }

// authorizer says yes for the projects it was given, and records what it was
// asked, because the permission this reading needs is part of what is tested.
type authorizer struct {
	allowed map[uuid.UUID]bool
	asked   []domain.Permission
}

func (a *authorizer) Can(_ context.Context, _ domain.Identity, perm domain.Permission, project uuid.UUID) (bool, error) {
	a.asked = append(a.asked, perm)
	return a.allowed[project], nil
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newService(t *testing.T, minimum int, projects []uuid.UUID, allowedOn ...uuid.UUID) (*Service, *store, *authorizer, *clock) {
	t.Helper()

	repo := &store{}
	clk := &clock{now: time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)}
	auth := &authorizer{allowed: map[uuid.UUID]bool{}}
	for _, id := range allowedOn {
		auth.allowed[id] = true
	}

	return NewService(repo, policy{minimum: minimum}, directory{ids: projects}, auth, clk, quiet()), repo, auth, clk
}

func at(clk *clock, ago time.Duration, state domain.RunnerState, contract int, name string) domain.Runner {
	return domain.Runner{
		Name: name, Version: "a1b2c3d", Contract: contract, State: state,
		StartedAt: clk.now.Add(-time.Hour), HeartbeatAt: clk.now.Add(-ago),
	}
}

// What the screen is built from: each runner with the status read against the
// clock, whether it is below the minimum, and the minimum itself.
func TestAnAdministratorOfEveryProjectSeesEachRunnerAndItsStatus(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	service, repo, _, clk := newService(t, 2, []uuid.UUID{a, b}, a, b)

	repo.listed = []domain.Runner{
		at(clk, 3*time.Second, domain.RunnerServing, 2, "current"),
		at(clk, 3*time.Second, domain.RunnerServing, 1, "old-but-alive"),
		at(clk, time.Minute, domain.RunnerServing, 2, "silent"),
		at(clk, 3*time.Second, domain.RunnerDraining, 2, "deploying"),
		at(clk, 3*time.Second, domain.RunnerStopped, 2, "gone"),
	}

	got, err := service.Overview(context.Background(), domain.Identity{OrgID: uuid.New(), UserID: uuid.New()})
	if err != nil {
		t.Fatalf("overview: %v", err)
	}

	if got.MinimumContract != 2 {
		t.Fatalf("minimum = %d, want 2", got.MinimumContract)
	}

	want := map[string]struct {
		status   domain.RunnerStatus
		outdated bool
	}{
		"current":       {domain.RunnerOnline, false},
		"old-but-alive": {domain.RunnerOnline, true},
		"silent":        {domain.RunnerOffline, false},
		"deploying":     {domain.RunnerStatusDraining, false},
		"gone":          {domain.RunnerOffline, false},
	}
	if len(got.Runners) != len(want) {
		t.Fatalf("%d runners, want %d", len(got.Runners), len(want))
	}
	for _, v := range got.Runners {
		w := want[v.Runner.Name]
		if v.Status != w.status || v.Outdated != w.outdated {
			t.Errorf("%s: status %q outdated %v, want %q and %v", v.Runner.Name, v.Status, v.Outdated, w.status, w.outdated)
		}
	}
}

// Somebody who administers part of the organization is refused, and nothing is
// read for them. A state of the platform that leaves out what they cannot see,
// read as the state of the platform, is worse than none.
func TestSomebodyWhoAdministersPartOfTheOrganizationIsRefused(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	service, repo, _, _ := newService(t, 1, []uuid.UUID{a, b}, a)

	_, err := service.Overview(context.Background(), domain.Identity{OrgID: uuid.New(), UserID: uuid.New()})
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("err = %v, want forbidden", err)
	}
	if repo.listCalls != 0 {
		t.Fatal("the runners were read for somebody who may not see them")
	}
}

// The permission asked is the one that means "administers the project", and it
// is asked once per project: a check with no scope is a bug.
func TestEveryProjectIsAskedAboutTheAdministratorPermission(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	service, _, auth, _ := newService(t, 1, []uuid.UUID{a, b, c}, a, b, c)

	if _, err := service.Overview(context.Background(), domain.Identity{OrgID: uuid.New(), UserID: uuid.New()}); err != nil {
		t.Fatalf("overview: %v", err)
	}
	if len(auth.asked) != 3 {
		t.Fatalf("asked %d times, want once per project (3)", len(auth.asked))
	}
	for _, p := range auth.asked {
		if p != domain.PermManageProject {
			t.Fatalf("asked about %v, want PermManageProject", p)
		}
	}
}

func TestAnOrganizationThatCannotBeListedIsAnError(t *testing.T) {
	repo := &store{}
	service := NewService(repo, policy{minimum: 1}, directory{err: errors.New("the database is gone")},
		&authorizer{}, &clock{now: time.Now()}, quiet())

	if _, err := service.Overview(context.Background(), domain.Identity{OrgID: uuid.New()}); err == nil {
		t.Fatal("a failure to list the projects was swallowed")
	}
}
