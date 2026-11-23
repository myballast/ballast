//go:build integration

package postgres_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/run"
)

// What a pipeline said about itself is written with the run and read back exactly
// as it was kept: a run rebuilt from the database says what the run that was
// queued said, and a field nobody gave is absent in the row and empty in the run.
//
// Written before the code, which is why they do not pass yet.

func aKeptPipeline() domain.RunCI {
	return domain.RunCI{
		Provider:   domain.CIProviderGitHubActions,
		Repository: "myballast/ballast",
		Branch:     "feature/ci-metadata",
		Commit:     "3f9c2a1b7d4e5f60718293a4b5c6d7e8f9012345",
		JobURL:     "https://github.com/myballast/ballast/actions/runs/123456789",
	}
}

func queueWithCI(ctx context.Context, t *testing.T, world queueWorld, ci *domain.RunCI) (domain.Run, error) {
	t.Helper()

	r := domain.NewRun(world.org, world.scenario, world.env, domain.ProfileSmoke,
		domain.ProfileParams{TargetVUs: 1, PlateauS: 5}, &world.user)
	if ci != nil {
		r = r.WithCI(*ci)
	}
	return world.runs.Create(ctx, r)
}

// columns reads the five columns as they are in the row, with nothing between them
// and the domain: a null stays a null.
func columns(ctx context.Context, t *testing.T, world queueWorld, id uuid.UUID) [5]*string {
	t.Helper()

	var c [5]*string
	err := world.db.InTenantTx(ctx, world.org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT ci_provider, ci_repository, ci_branch, ci_commit, ci_job_url
			FROM run WHERE org_id = $1 AND id = $2`, world.org, id).
			Scan(&c[0], &c[1], &c[2], &c[3], &c[4])
	})
	if err != nil {
		t.Fatalf("read the columns: %v", err)
	}
	return c
}

func TestARunIsWrittenWithWhatThePipelineSaidAndReadBackTheSame(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	ci := aKeptPipeline()

	created, err := queueWithCI(ctx, t, world, &ci)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	for name, got := range map[string]*domain.RunCI{
		"what Create returns": created.CI,
		"what ByID reads":     mustByID(ctx, t, world, created.ID).CI,
	} {
		if got == nil || *got != ci {
			t.Errorf("%s: %+v, want %+v", name, got, ci)
		}
	}
}

// What is stored is what was kept, in its own column, and the run that is
// rebuilt from the row is the run that was written.
func TestEachFieldGoesToItsOwnColumn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	ci := aKeptPipeline()
	created, err := queueWithCI(ctx, t, world, &ci)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	c := columns(ctx, t, world, created.ID)
	want := [5]string{string(ci.Provider), ci.Repository, ci.Branch, ci.Commit, ci.JobURL}
	for i, name := range []string{"ci_provider", "ci_repository", "ci_branch", "ci_commit", "ci_job_url"} {
		if c[i] == nil || *c[i] != want[i] {
			t.Errorf("%s holds %v, want %q", name, c[i], want[i])
		}
	}
}

// A run nobody said anything about has no CI, and its columns are null: the
// absence of data is not a pipeline with empty fields.
func TestARunWithNoCIDataIsReadBackWithNone(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)

	created, err := queueWithCI(ctx, t, world, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if created.CI != nil || mustByID(ctx, t, world, created.ID).CI != nil {
		t.Errorf("a run with no CI data came back with %+v", mustByID(ctx, t, world, created.ID).CI)
	}
	for i, v := range columns(ctx, t, world, created.ID) {
		if v != nil {
			t.Errorf("column %d holds %q, want null", i, *v)
		}
	}
}

// A pipeline that could say only who it was says only that. The fields it left out
// are null in the row, not empty strings: the schema refuses an empty one — and are
// empty in the run, which is how the domain spells "not said".
func TestAFieldThePipelineLeftOutIsNullInTheRowAndEmptyInTheRun(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	ci := domain.RunCI{Provider: domain.CIProviderGitLab, Commit: "abcdef1"}

	created, err := queueWithCI(ctx, t, world, &ci)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got := mustByID(ctx, t, world, created.ID).CI
	if got == nil || *got != ci {
		t.Errorf("read back %+v, want %+v", got, ci)
	}

	c := columns(ctx, t, world, created.ID)
	if c[0] == nil || *c[0] != "gitlab_ci" || c[3] == nil || *c[3] != "abcdef1" {
		t.Errorf("provider and commit are %v and %v", c[0], c[3])
	}
	for _, i := range []int{1, 2, 4} {
		if c[i] != nil {
			t.Errorf("column %d holds %q, want null", i, *c[i])
		}
	}
}

func TestTheLongestValuesRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	ci := domain.RunCI{
		Provider:   domain.CIProviderOther,
		Repository: strings.Repeat("a", domain.MaxCIRepositoryLen),
		Branch:     strings.Repeat("b", domain.MaxCIBranchLen),
		Commit:     strings.Repeat("c", domain.MaxCICommitLen),
		JobURL:     "https://ci.example.test/" + strings.Repeat("j", domain.MaxCIJobURLLen-len("https://ci.example.test/")),
	}
	// The domain keeps them as they are: these are the bounds.
	kept, err := domain.ParseRunCI(ci)
	if err != nil || kept != ci {
		t.Fatalf("the domain does not keep the longest values as they are: %+v, %v", kept, err)
	}

	created, err := queueWithCI(ctx, t, world, &kept)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := mustByID(ctx, t, world, created.ID).CI; got == nil || *got != kept {
		t.Errorf("read back %+v", got)
	}
}

// The list a project's runs page reads is the same run, so it carries the same CI
// data, and a run in it that said nothing carries none.
func TestTheListCarriesTheCIDataOfEachRun(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	ci := aKeptPipeline()
	with, err := queueWithCI(ctx, t, world, &ci)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	without, err := queueWithCI(ctx, t, world, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	listed, err := world.runs.InProject(ctx, world.org, world.scenario.ProjectID, run.Filter{}, run.Window{}, 10)
	if err != nil {
		t.Fatalf("InProject: %v", err)
	}

	for _, l := range listed {
		switch l.ID {
		case with.ID:
			if l.CI == nil || *l.CI != ci {
				t.Errorf("the run with CI data lists %+v", l.CI)
			}
		case without.ID:
			if l.CI != nil {
				t.Errorf("the run with none lists %+v", l.CI)
			}
		}
	}
}

// What the schema refuses stops the write: a value that did not go through the
// domain — a script, a mistake — cannot make a run that stores a branch that reads
// backwards, and no run is left behind.
func TestACIValueTheSchemaRefusesStopsTheWholeRun(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	before := countRuns(ctx, t, world)

	for name, ci := range map[string]domain.RunCI{
		"a branch that reads backwards": {Provider: domain.CIProviderOther, Branch: "release\u202egnp.exe"},
		"a link with a password":        {Provider: domain.CIProviderOther, JobURL: "https://u:hunter2@ci.example.test/job"},
		"a provider nobody knows":       {Provider: "jenkins"},
		"a commit that is not a hash":   {Provider: domain.CIProviderOther, Commit: "main"},
	} {
		ci := ci
		if _, err := queueWithCI(ctx, t, world, &ci); err == nil {
			t.Errorf("%s: the run was written", name)
		}
	}

	if after := countRuns(ctx, t, world); after != before {
		t.Errorf("%d runs were left behind", after-before)
	}
}

func countRuns(ctx context.Context, t *testing.T, world queueWorld) int {
	t.Helper()

	var n int
	err := world.db.InTenantTx(ctx, world.org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM run WHERE org_id = $1`, world.org).Scan(&n)
	})
	if err != nil {
		t.Fatalf("count the runs: %v", err)
	}
	return n
}
