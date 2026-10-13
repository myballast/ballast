package run

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

func listService(runs *fakeRuns, authz Authorizer) *Service {
	return NewService(
		runs,
		fakeScenarios{},
		fakeEnvironments{},
		&fakeAllowlist{},
		noSecrets{},
		nil, nil, nil, nil, nil, nil,
		authz,
		fixedClock{at: time.Unix(0, 0).UTC()},
		Accepting(true),
		discardLogger(),
	)
}

func someone() domain.Identity {
	return domain.Identity{UserID: uuid.New(), OrgID: uuid.New()}
}

// runsAt returns n runs, newest first, a minute apart.
func runsAt(n int) []domain.Run {
	start := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	out := make([]domain.Run, n)
	for i := range out {
		out[i] = domain.Run{ID: uuid.New(), CreatedAt: start.Add(-time.Duration(i) * time.Minute)}
	}
	return out
}

func TestInProjectListsTheProjectsRuns(t *testing.T) {
	project := uuid.New()
	runs := &fakeRuns{listed: runsAt(1)}

	page, err := listService(runs, allowAll{}).InProject(context.Background(), someone(), project, nil, 5)
	if err != nil {
		t.Fatalf("InProject: %v", err)
	}
	if len(page.Runs) != 1 || runs.listedFor != project {
		t.Errorf("got %d runs for project %s", len(page.Runs), runs.listedFor)
	}
}

// The middleware already checks the path's project. The service asks again:
// a route registered without the middleware must not become a way to list
// another project's runs.
func TestInProjectRefusesWithoutViewProject(t *testing.T) {
	runs := &fakeRuns{listed: runsAt(1)}

	_, err := listService(runs, refuseAll{}).InProject(context.Background(), someone(), uuid.New(), nil, 5)
	if !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("InProject returned %v, want ErrForbidden", err)
	}
	if runs.listedFor != uuid.Nil {
		t.Error("the runs were read before the permission was checked")
	}
}

func TestInProjectAsksOnlyForViewProject(t *testing.T) {
	asked := &askedFor{}
	project := uuid.New()

	_, _ = listService(&fakeRuns{}, asked).InProject(context.Background(), someone(), project, nil, 5)

	if asked.perm != domain.PermViewProject || asked.projectID != project {
		t.Errorf("asked %v on %s, want view_project on %s", asked.perm, asked.projectID, project)
	}
}

// A limit is a request, not a promise: none means the default, too many means
// the ceiling. One more row than the page is read, to learn whether another
// page follows without counting the whole history.
func TestInProjectBoundsTheLimit(t *testing.T) {
	for _, tc := range []struct{ asked, want int }{
		{0, DefaultListLimit},
		{-3, DefaultListLimit},
		{1, 1},
		{MaxListLimit, MaxListLimit},
		{MaxListLimit + 1, MaxListLimit},
		{1_000_000, MaxListLimit},
	} {
		runs := &fakeRuns{}
		if _, err := listService(runs, allowAll{}).InProject(context.Background(), someone(), uuid.New(), nil, tc.asked); err != nil {
			t.Fatalf("InProject: %v", err)
		}
		if runs.listedLimit != tc.want+1 {
			t.Errorf("asked %d, the repository was asked %d, want %d (the page and one more)", tc.asked, runs.listedLimit, tc.want+1)
		}
	}
}

func TestInProjectSaysWhereTheNextPageStarts(t *testing.T) {
	all := runsAt(5)
	runs := &fakeRuns{listed: all}

	page, err := listService(runs, allowAll{}).InProject(context.Background(), someone(), uuid.New(), nil, 2)
	if err != nil {
		t.Fatalf("InProject: %v", err)
	}
	if len(page.Runs) != 2 {
		t.Fatalf("a page of 2 held %d runs; the extra row read to look ahead must not be shown", len(page.Runs))
	}
	want := Cursor{CreatedAt: all[1].CreatedAt, ID: all[1].ID}
	if page.Next == nil || *page.Next != want {
		t.Errorf("next = %+v, want the last run shown, %+v", page.Next, want)
	}
}

func TestInProjectLastPageHasNoNext(t *testing.T) {
	runs := &fakeRuns{listed: runsAt(2)}

	page, err := listService(runs, allowAll{}).InProject(context.Background(), someone(), uuid.New(), nil, 2)
	if err != nil {
		t.Fatalf("InProject: %v", err)
	}
	if len(page.Runs) != 2 || page.Next != nil {
		t.Errorf("a history that fits on one page returned %d runs and next %+v", len(page.Runs), page.Next)
	}
}

func TestInProjectPassesTheCursorOn(t *testing.T) {
	before := &Cursor{CreatedAt: time.Date(2026, 10, 13, 11, 0, 0, 0, time.UTC), ID: uuid.New()}
	runs := &fakeRuns{}

	if _, err := listService(runs, allowAll{}).InProject(context.Background(), someone(), uuid.New(), before, 2); err != nil {
		t.Fatalf("InProject: %v", err)
	}
	if runs.listedBefore == nil || *runs.listedBefore != *before {
		t.Errorf("the repository was asked for runs before %+v, want %+v", runs.listedBefore, before)
	}
}
