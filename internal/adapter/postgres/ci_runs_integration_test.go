//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// A run launched by a pipeline has no person, and says which token it was; an
// audit entry written for it names the token and not a person. What these tests
// hold in place is what the schema promises about that, so that nothing above it
// has to remember:
//
//   - a run or an entry never names a person and a token together;
//   - a run's token is one of its own organization, which the composite key says
//     and row-level security cannot;
//   - an entry outlives the token it names. The record of what a credential did
//     must not disappear with the project it belonged to, or deleting a project
//     would be a way of erasing its trail.
//
// Written before the migration, which is why they do not pass yet.

// ciRow is the rows of an organization the tests below write against.
type ciRow struct {
	user, project, token, run uuid.UUID
}

func ciRowsOf(ctx context.Context, t *testing.T, f fixture, org uuid.UUID) ciRow {
	t.Helper()

	var r ciRow
	err := f.db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		for query, dest := range map[string]*uuid.UUID{
			"SELECT id FROM app_user  WHERE org_id = $1": &r.user,
			"SELECT id FROM project   WHERE org_id = $1": &r.project,
			"SELECT id FROM api_token WHERE org_id = $1": &r.token,
			"SELECT id FROM run       WHERE org_id = $1": &r.run,
		} {
			if err := tx.QueryRow(ctx, query, org).Scan(dest); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read the fixture's rows of %s: %v", org, err)
	}
	return r
}

func exec(ctx context.Context, f fixture, org uuid.UUID, sql string, args ...any) error {
	return f.db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, sql, args...)
		return err
	})
}

// --- run.triggered_by_token --------------------------------------------------------

func TestARunNamesAPersonOrATokenAndNeverBoth(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	r := ciRowsOf(ctx, t, f, f.orgA)

	// A person alone, a token alone, and nobody: all fine.
	for name, c := range map[string]struct {
		set  string
		args []any
	}{
		"a person alone": {"triggered_by = $2, triggered_by_token = NULL", []any{f.orgA, r.user}},
		"a token alone":  {"triggered_by = NULL, triggered_by_token = $2", []any{f.orgA, r.token}},
		"nobody":         {"triggered_by = NULL, triggered_by_token = NULL", []any{f.orgA}},
	} {
		if err := exec(ctx, f, f.orgA, "UPDATE run SET "+c.set+" WHERE org_id = $1", c.args...); err != nil {
			t.Errorf("%s was refused: %v", name, err)
		}
	}

	// Both is a run nobody can say who launched.
	err := exec(ctx, f, f.orgA,
		"UPDATE run SET triggered_by = $2, triggered_by_token = $3 WHERE org_id = $1", f.orgA, r.user, r.token)
	if sqlState(err) != "23514" {
		t.Errorf("a person and a token together: %v, want a check violation", err)
	}
}

// Row-level security checks the row's org_id and nothing about what the row
// points at. The composite key is what says the token is this organization's.
func TestARunCannotNameATokenOfAnotherOrganization(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	a, b := ciRowsOf(ctx, t, f, f.orgA), ciRowsOf(ctx, t, f, f.orgB)

	err := exec(ctx, f, f.orgA, "UPDATE run SET triggered_by_token = $2 WHERE id = $1", a.run, b.token)

	if sqlState(err) != "23503" {
		t.Errorf("a run naming another organization's token: %v, want a foreign key violation", err)
	}
}

// If the token goes, the run stays and forgets only the token: clearing the whole
// composite key would try to empty org_id, which is NOT NULL.
func TestARunSurvivesItsToken(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	r := ciRowsOf(ctx, t, f, f.orgA)

	if err := exec(ctx, f, f.orgA, "UPDATE run SET triggered_by_token = $2 WHERE id = $1", r.run, r.token); err != nil {
		t.Fatalf("name the token: %v", err)
	}
	if err := exec(ctx, f, f.orgA, "DELETE FROM api_token WHERE id = $1", r.token); err != nil {
		t.Fatalf("delete the token: %v", err)
	}

	var token *uuid.UUID
	var org uuid.UUID
	err := f.db.InTenantTx(ctx, f.orgA, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT org_id, triggered_by_token FROM run WHERE id = $1", r.run).Scan(&org, &token)
	})
	if err != nil {
		t.Fatalf("the run did not survive its token: %v", err)
	}
	if token != nil || org != f.orgA {
		t.Errorf("the run says token %v in org %s, want no token in org %s", token, org, f.orgA)
	}
}

// The limit counts a token's runs over a window of an hour, for every request a
// pipeline makes. An index that starts with the organization, like every other
// here, and covers only the rows that have a token.
func TestTheRunsOfATokenAreIndexedForTheLimit(t *testing.T) {
	ctx, conn := connect(t)

	var columns, predicate string
	err := conn.QueryRow(ctx, `
		SELECT array_to_string(array_agg(a.attname ORDER BY k.ord), ','), coalesce(pg_get_expr(i.indpred, i.indrelid), '')
		FROM pg_index i
		JOIN pg_class c ON c.oid = i.indexrelid
		JOIN LATERAL unnest(i.indkey) WITH ORDINALITY AS k(attnum, ord) ON true
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = k.attnum
		WHERE c.relname = 'run_org_token_created_idx'
		GROUP BY i.indexrelid, i.indpred, i.indrelid`).Scan(&columns, &predicate)
	if err != nil {
		t.Fatalf("read the index: %v", err)
	}

	if columns != "org_id,triggered_by_token,created_at" {
		t.Errorf("the index covers %q, want org_id first and then the token and the date", columns)
	}
	if predicate == "" {
		t.Error("the index covers every run, though most have no token")
	}
}

// --- audit_log.actor_token_id ---------------------------------------------------------

func TestAnEntryNamesAPersonOrATokenAndNeverBoth(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	r := ciRowsOf(ctx, t, f, f.orgA)

	const insert = `
		INSERT INTO audit_log (org_id, actor_id, actor_token_id, action, outcome, target_type)
		VALUES ($1, $2, $3, 'run.queued', 'allowed', 'run')`

	for name, args := range map[string][]any{
		"a person":       {f.orgA, r.user, nil},
		"a token":        {f.orgA, nil, r.token},
		"Ballast, alone": {f.orgA, nil, nil},
	} {
		if err := exec(ctx, f, f.orgA, insert, args...); err != nil {
			t.Errorf("an entry by %s was refused: %v", name, err)
		}
	}

	err := exec(ctx, f, f.orgA, insert, f.orgA, r.user, r.token)
	if sqlState(err) != "23514" {
		t.Errorf("an entry naming a person and a token: %v, want a check violation", err)
	}
}

// An entry outlives the token it names, and keeps naming it. There is no foreign
// key on purpose, as there is none on project_id: a project that is deleted takes
// its tokens with it, and a key that cleared the author on the way would make
// deleting a project a way of erasing what its credentials did.
func TestAnEntryKeepsNamingATokenThatIsGone(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	r := ciRowsOf(ctx, t, f, f.orgA)

	if err := exec(ctx, f, f.orgA, `
		INSERT INTO audit_log (org_id, actor_token_id, action, outcome, target_type, project_id)
		VALUES ($1, $2, 'run.queued', 'allowed', 'run', $3)`, f.orgA, r.token, r.project); err != nil {
		t.Fatalf("write the entry: %v", err)
	}
	if err := exec(ctx, f, f.orgA, "DELETE FROM project WHERE id = $1", r.project); err != nil {
		t.Fatalf("delete the project, and its tokens with it: %v", err)
	}

	var named *uuid.UUID
	err := f.db.InTenantTx(ctx, f.orgA, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			"SELECT actor_token_id FROM audit_log WHERE org_id = $1 AND actor_token_id IS NOT NULL", f.orgA).Scan(&named)
	})
	if err != nil {
		t.Fatalf("the entry did not survive its token: %v", err)
	}
	if named == nil || *named != r.token {
		t.Errorf("the entry names %v, want the token %s that is gone", named, r.token)
	}
}
