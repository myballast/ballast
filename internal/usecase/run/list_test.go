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

func TestInProjectListsTheProjectsRuns(t *testing.T) {
	project := uuid.New()
	runs := &fakeRuns{listed: []domain.Run{{ID: uuid.New(), ProjectID: project}}}
	who := domain.Identity{UserID: uuid.New(), OrgID: uuid.New()}

	got, err := listService(runs, allowAll{}).InProject(context.Background(), who, project, 5)
	if err != nil {
		t.Fatalf("InProject: %v", err)
	}
	if len(got) != 1 || runs.listedFor != project || runs.listedLimit != 5 {
		t.Errorf("got %d runs, asked for project %s limit %d", len(got), runs.listedFor, runs.listedLimit)
	}
}

// The middleware already checks the path's project. The service asks again:
// a route registered without the middleware must not become a way to list
// another project's runs.
func TestInProjectRefusesWithoutViewProject(t *testing.T) {
	runs := &fakeRuns{listed: []domain.Run{{ID: uuid.New()}}}

	_, err := listService(runs, refuseAll{}).InProject(context.Background(), domain.Identity{UserID: uuid.New(), OrgID: uuid.New()}, uuid.New(), 5)
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

	_, _ = listService(&fakeRuns{}, asked).InProject(context.Background(), domain.Identity{UserID: uuid.New(), OrgID: uuid.New()}, project, 5)

	if asked.perm != domain.PermViewProject || asked.projectID != project {
		t.Errorf("asked %v on %s, want view_project on %s", asked.perm, asked.projectID, project)
	}
}

// A limit is a request, not a promise: none means the default, too many means
// the ceiling. A page of a million runs is a way to hold a connection, not a
// view anyone needs.
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
		if _, err := listService(runs, allowAll{}).InProject(context.Background(), domain.Identity{UserID: uuid.New(), OrgID: uuid.New()}, uuid.New(), tc.asked); err != nil {
			t.Fatalf("InProject: %v", err)
		}
		if runs.listedLimit != tc.want {
			t.Errorf("asked %d, the repository was asked %d, want %d", tc.asked, runs.listedLimit, tc.want)
		}
	}
}
