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
		nil, nil, nil, nil, &fakeJournal{}, nil, nil,
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
func runsAt(n int) []Listed {
	start := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	out := make([]Listed, n)
	for i := range out {
		out[i] = Listed{Run: domain.Run{ID: uuid.New(), CreatedAt: start.Add(-time.Duration(i) * time.Minute)}}
	}
	return out
}

func list(t *testing.T, runs *fakeRuns, query ListQuery) Page {
	t.Helper()
	page, err := listService(runs, allowAll{}).InProject(context.Background(), someone(), uuid.New(), query)
	if err != nil {
		t.Fatalf("InProject: %v", err)
	}
	return page
}

func cursorAt(l Listed) Cursor { return Cursor{CreatedAt: l.CreatedAt, ID: l.ID} }

func TestInProjectListsTheProjectsRuns(t *testing.T) {
	project := uuid.New()
	runs := &fakeRuns{listed: runsAt(1)}

	page, err := listService(runs, allowAll{}).InProject(context.Background(), someone(), project, ListQuery{Limit: 5})
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

	_, err := listService(runs, refuseAll{}).InProject(context.Background(), someone(), uuid.New(), ListQuery{Limit: 5})
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

	_, _ = listService(&fakeRuns{}, asked).InProject(context.Background(), someone(), project, ListQuery{Limit: 5})

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
		list(t, runs, ListQuery{Limit: tc.asked})
		if runs.listedLimit != tc.want+1 {
			t.Errorf("asked %d, the repository was asked %d, want %d (the page and one more)", tc.asked, runs.listedLimit, tc.want+1)
		}
	}
}

func TestInProjectSaysWhereTheNextPageStarts(t *testing.T) {
	all := runsAt(5)

	page := list(t, &fakeRuns{listed: all}, ListQuery{Limit: 2})

	if len(page.Runs) != 2 {
		t.Fatalf("a page of 2 held %d runs; the extra row read to look ahead must not be shown", len(page.Runs))
	}
	if want := cursorAt(all[1]); page.Next == nil || *page.Next != want {
		t.Errorf("next = %+v, want the last run shown, %+v", page.Next, want)
	}
	if page.Prev != nil {
		t.Errorf("the newest page has a prev, %+v", page.Prev)
	}
}

func TestInProjectLastPageHasNoNext(t *testing.T) {
	page := list(t, &fakeRuns{listed: runsAt(2)}, ListQuery{Limit: 2})

	if len(page.Runs) != 2 || page.Next != nil {
		t.Errorf("a history that fits on one page returned %d runs and next %+v", len(page.Runs), page.Next)
	}
}

func TestInProjectPassesTheWindowAndTheFilterOn(t *testing.T) {
	scenario := uuid.New()
	before := &Cursor{CreatedAt: time.Date(2026, 10, 13, 11, 0, 0, 0, time.UTC), ID: uuid.New()}
	runs := &fakeRuns{}

	list(t, runs, ListQuery{
		Filter: Filter{Status: domain.StatusFailed, ScenarioID: &scenario},
		Window: Window{Before: before},
		Limit:  2,
	})

	if runs.listedWindow.Before == nil || *runs.listedWindow.Before != *before {
		t.Errorf("the repository was asked for runs before %+v, want %+v", runs.listedWindow.Before, before)
	}
	if runs.listedFilter.Status != domain.StatusFailed || runs.listedFilter.ScenarioID == nil || *runs.listedFilter.ScenarioID != scenario {
		t.Errorf("the filter was %+v", runs.listedFilter)
	}
}

// Having read toward older runs from a cursor, there are newer ones: the ones
// the cursor came from. The page before is found by going back to its first run.
func TestAPageReachedByBeforeCanGoBack(t *testing.T) {
	all := runsAt(5)
	cursor := Cursor{CreatedAt: all[0].CreatedAt.Add(time.Minute), ID: uuid.New()}

	page := list(t, &fakeRuns{listed: all}, ListQuery{Limit: 2, Window: Window{Before: &cursor}})

	if want := cursorAt(all[0]); page.Prev == nil || *page.Prev != want {
		t.Errorf("prev = %+v, want the first run shown, %+v", page.Prev, want)
	}
	if want := cursorAt(all[1]); page.Next == nil || *page.Next != want {
		t.Errorf("next = %+v, want the last run shown, %+v", page.Next, want)
	}
}

// Reading toward newer runs, the repository hands back the nearest ones newest
// first, with the extra one — the newest — at the head. It is dropped, and it is
// what says there is a newer page still.
func TestAPageReachedByAfterDropsTheExtraNewestRun(t *testing.T) {
	all := runsAt(5) // newest first; the repository was asked for 3 and gives 3
	read := all[:3]
	cursor := Cursor{CreatedAt: all[3].CreatedAt, ID: all[3].ID}

	page := list(t, &fakeRuns{listed: read}, ListQuery{Limit: 2, Window: Window{After: &cursor}})

	if len(page.Runs) != 2 || page.Runs[0].ID != all[1].ID || page.Runs[1].ID != all[2].ID {
		t.Fatalf("the page is %v, want the two runs nearest the cursor", page.Runs)
	}
	if want := cursorAt(all[1]); page.Prev == nil || *page.Prev != want {
		t.Errorf("prev = %+v, want the newest run shown, %+v", page.Prev, want)
	}
	if want := cursorAt(all[2]); page.Next == nil || *page.Next != want {
		t.Errorf("next = %+v, want the oldest run shown, %+v", page.Next, want)
	}
}

func TestTheNewestPageReachedByAfterHasNoPrev(t *testing.T) {
	all := runsAt(2)
	cursor := Cursor{CreatedAt: all[1].CreatedAt.Add(-time.Minute), ID: uuid.New()}

	page := list(t, &fakeRuns{listed: all}, ListQuery{Limit: 5, Window: Window{After: &cursor}})

	if len(page.Runs) != 2 || page.Prev != nil {
		t.Errorf("got %d runs and prev %+v; nothing is newer", len(page.Runs), page.Prev)
	}
	if page.Next == nil {
		t.Error("the cursor is older than these runs, so there is an older page to go back to")
	}
}

func TestAnUnknownStatusIsRefusedBeforeTheDatabaseIsAsked(t *testing.T) {
	runs := &fakeRuns{listed: runsAt(1)}

	_, err := listService(runs, allowAll{}).InProject(context.Background(), someone(), uuid.New(),
		ListQuery{Filter: Filter{Status: "sleeping"}})

	if !errors.Is(err, domain.ErrInvalidRunFilter) {
		t.Errorf("got %v, want ErrInvalidRunFilter", err)
	}
	if runs.listedFor != uuid.Nil {
		t.Error("the repository was asked for runs of a status that does not exist")
	}
}

func TestEveryKnownStatusIsAcceptedAsAFilter(t *testing.T) {
	for _, status := range []domain.RunStatus{
		domain.StatusQueued, domain.StatusRunning, domain.StatusPassed, domain.StatusFailed, domain.StatusErrored,
		domain.StatusTimeout, domain.StatusCancelled, domain.StatusRejected, domain.StatusSkipped, domain.StatusOrphaned,
	} {
		if _, err := listService(&fakeRuns{}, allowAll{}).InProject(context.Background(), someone(), uuid.New(),
			ListQuery{Filter: Filter{Status: status}}); err != nil {
			t.Errorf("%q was refused: %v", status, err)
		}
	}
}

func TestAPageCannotBeBothBeforeAndAfter(t *testing.T) {
	at := &Cursor{CreatedAt: time.Now(), ID: uuid.New()}

	_, err := listService(&fakeRuns{}, allowAll{}).InProject(context.Background(), someone(), uuid.New(),
		ListQuery{Window: Window{Before: at, After: at}})

	if !errors.Is(err, domain.ErrInvalidRunFilter) {
		t.Errorf("got %v, want ErrInvalidRunFilter", err)
	}
}
