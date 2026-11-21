//go:build integration

package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// What a pipeline says about itself is kept on the run, in five optional columns,
// and the database repeats the bounds the domain applies. The domain is the
// authority and is tested on its own; these tests hold in place that the table
// refuses what the domain would have refused, so that nothing that reaches it by
// another road — a script, a migration, a mistake — can store a branch that reads
// backwards or a link with a password in it.
//
// Written before the migration, which is why they do not pass yet.

type ciColumns struct {
	provider, repository, branch, commit, jobURL any
}

func setRunCI(ctx context.Context, f fixture, org, run uuid.UUID, c ciColumns) error {
	return exec(ctx, f, org, `
		UPDATE run
		SET ci_provider = $3, ci_repository = $4, ci_branch = $5, ci_commit = $6, ci_job_url = $7
		WHERE org_id = $1 AND id = $2`,
		org, run, c.provider, c.repository, c.branch, c.commit, c.jobURL)
}

func TestARunWithNoCIDataHasNone(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	r := ciRowsOf(ctx, t, f, f.orgA)

	// The fixture's run was written before these columns existed, in effect: it
	// says nothing about a pipeline, and nothing about it changes.
	if n := countWhere(ctx, t, f, f.orgA, `ci_provider IS NULL AND ci_repository IS NULL
		AND ci_branch IS NULL AND ci_commit IS NULL AND ci_job_url IS NULL AND id = $2`, r.run); n != 1 {
		t.Errorf("the run has CI data it was never given")
	}
}

func countWhere(ctx context.Context, t *testing.T, f fixture, org uuid.UUID, where string, args ...any) int {
	t.Helper()

	var n int
	err := f.db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM run WHERE org_id = $1 AND "+where, append([]any{org}, args...)...).Scan(&n)
	})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestAWellFormedRunIsAccepted(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	r := ciRowsOf(ctx, t, f, f.orgA)

	for name, c := range map[string]ciColumns{
		"everything": {
			"github_actions", "myballast/ballast", "feature/ci-metadata",
			"3f9c2a1b7d4e5f60718293a4b5c6d7e8f9012345",
			"https://github.com/myballast/ballast/actions/runs/123456789",
		},
		"the provider alone":  {"gitlab_ci", nil, nil, nil, nil},
		"another provider":    {"other", "group/sub/project", "release-1.2.3", "abcdef1", "https://ci.example.test:8443/job/7"},
		"a host with no path": {"other", nil, nil, nil, "https://ci.example.test"},
		"the longest values": {
			"other", strings.Repeat("a", 200), strings.Repeat("b", 255), strings.Repeat("c", 64),
			"https://ci.example.test/" + strings.Repeat("j", 450),
		},
	} {
		if err := setRunCI(ctx, f, f.orgA, r.run, c); err != nil {
			t.Errorf("%s was refused: %v", name, err)
		}
	}
}

func TestAProviderIsOneOfTheThree(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	r := ciRowsOf(ctx, t, f, f.orgA)

	for _, bad := range []string{"", "github", "GitHub", "github_actions ", "jenkins", "other; drop table run"} {
		err := setRunCI(ctx, f, f.orgA, r.run, ciColumns{bad, nil, nil, nil, nil})
		if sqlState(err) != "23514" {
			t.Errorf("the provider %q was accepted or failed oddly: %v", bad, err)
		}
	}
}

// Without a provider there is nothing to say what the rest is about. A repository
// with no pipeline behind it is a column somebody wrote by hand.
func TestNothingIsKeptWithoutAProvider(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	r := ciRowsOf(ctx, t, f, f.orgA)

	for name, c := range map[string]ciColumns{
		"a repository": {nil, "owner/repo", nil, nil, nil},
		"a branch":     {nil, nil, "main", nil, nil},
		"a commit":     {nil, nil, nil, "abcdef1", nil},
		"a link":       {nil, nil, nil, nil, "https://ci.example.test/job"},
	} {
		if sqlState(setRunCI(ctx, f, f.orgA, r.run, c)) != "23514" {
			t.Errorf("%s with no provider was accepted", name)
		}
	}
}

// Absence is a null. An empty string would be a value that says nothing, and the
// two would have to be told apart by whoever reads the column.
func TestAColumnIsNullOrHoldsSomething(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	r := ciRowsOf(ctx, t, f, f.orgA)

	for name, c := range map[string]ciColumns{
		"an empty repository": {"other", "", nil, nil, nil},
		"an empty branch":     {"other", nil, "", nil, nil},
		"an empty commit":     {"other", nil, nil, "", nil},
		"an empty link":       {"other", nil, nil, nil, ""},
	} {
		if sqlState(setRunCI(ctx, f, f.orgA, r.run, c)) != "23514" {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestTheTableRefusesAWrongRepository(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	r := ciRowsOf(ctx, t, f, f.orgA)

	for _, bad := range []string{
		strings.Repeat("a", 201), "/owner/repo", "owner/repo/", "owner//repo",
		"owner/repo name", "owner/re\npo", "owner/repo`id`", "https://github.com/owner/repo",
	} {
		err := setRunCI(ctx, f, f.orgA, r.run, ciColumns{"other", bad, nil, nil, nil})
		if sqlState(err) != "23514" {
			t.Errorf("the repository %.30q was accepted or failed oddly: %v", bad, err)
		}
	}
}

func TestTheTableRefusesABranchThatReadsAsSomethingItIsNot(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	r := ciRowsOf(ctx, t, f, f.orgA)

	for name, bad := range map[string]string{
		"too long":                 strings.Repeat("b", 256),
		"a space":                  "my branch",
		"a line break":             "main\nBallast: passed",
		"a tab":                    "ma\tin",
		"an escape":                "main\x1b[31m",
		"a bidirectional override": "release\u202egnp.exe",
		"a zero-width space":       "ma\u200bin",
		"a byte order mark":        "\ufeffmain",
		"a line separator":         "ma\u2028in",
	} {
		err := setRunCI(ctx, f, f.orgA, r.run, ciColumns{"other", nil, bad, nil, nil})
		if err == nil {
			t.Errorf("a branch with %s was accepted", name)
		}
	}
}

func TestTheTableRefusesAWrongCommit(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	r := ciRowsOf(ctx, t, f, f.orgA)

	for _, bad := range []string{"abcdef", strings.Repeat("a", 65), "ABCDEF1", "abcdefg", "main", "abc def1", "abcdef1; rm -rf /"} {
		err := setRunCI(ctx, f, f.orgA, r.run, ciColumns{"other", nil, nil, bad, nil})
		if sqlState(err) != "23514" {
			t.Errorf("the commit %q was accepted or failed oddly: %v", bad, err)
		}
	}
}

// What the table will not keep in a link: anything that is not an https address
// with a host, and the two places a token hides — credentials, and what follows
// the path.
func TestTheTableRefusesALinkThatIsNotAnHTTPSAddress(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	r := ciRowsOf(ctx, t, f, f.orgA)

	for _, bad := range []string{
		"http://ci.example.test/job/1",
		"javascript:alert(1)",
		"data:text/html,<script>alert(1)</script>",
		"//ci.example.test/job",
		"ci.example.test/job",
		"https://",
		"https:///job",
		"https://user:hunter2@ci.example.test/job",
		"https://hunter2@ci.example.test/job",
		"https://ci.example.test/job?token=abc",
		"https://ci.example.test/job#L10",
		"https://ci.example.test/job one",
		"https://ci.example.test/jo\nb",
		"https://ci.example.test/" + strings.Repeat("j", 500),
	} {
		err := setRunCI(ctx, f, f.orgA, r.run, ciColumns{"other", nil, nil, nil, bad})
		if sqlState(err) != "23514" {
			t.Errorf("the link %.40q was accepted or failed oddly: %v", bad, err)
		}
	}
}
