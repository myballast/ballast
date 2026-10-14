//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres"
)

// Raised when a generated column is assigned anything but DEFAULT.
const sqlStateGeneratedAlways = "428C9"

// organization.org_id is generated from the row's own id, so PostgreSQL refuses
// to assign it before row-level security ever gets a say. The refusal is the
// point; only the code differs. Every other table answers 42501.
var expectedRefusalByTable = map[string]string{
	"organization": sqlStateGeneratedAlways,
}

// appendOnly names the tables the application role may write but never rewrite.
//
// The journal is one: an entry that can be edited is not evidence of anything,
// so UPDATE and DELETE were taken away from the role entirely. That is stronger
// than row-level security, not weaker — it cannot touch another organization's
// entries because it cannot touch any — and the tests below say so rather than
// stepping around it.
var appendOnly = map[string]bool{"audit_log": true}

// Returned by a callback that only wants its transaction undone.
var errRollback = errors.New("rolling back on purpose")

type fixture struct {
	db   *postgres.DB
	orgA uuid.UUID
	orgB uuid.UUID
}

func isolationContext(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	return ctx
}

func newFixture(ctx context.Context, t *testing.T) fixture {
	t.Helper()

	db, err := postgres.Open(ctx, dsn())
	if err != nil {
		t.Fatalf("open %q: %v", dsn(), err)
	}
	t.Cleanup(db.Close)

	return fixture{
		db:   db,
		orgA: seedEveryTable(ctx, t, db, "alpha"),
		orgB: seedEveryTable(ctx, t, db, "beta"),
	}
}

// seedEveryTable puts exactly one row in every table of the schema, for one
// organization.
//
// Written out by hand, and it has to be: the foreign key chain and the NOT NULL
// columns differ from one table to the next. What is not written by hand is the
// checking — every assertion below walks the catalog, so a table added later is
// covered the day it appears. TestFixtureCoversEveryTable is what makes sure
// this function was extended along with it.
//
// Every insert here is also an assertion in its own right: the WITH CHECK of
// migration 001 rejects a row whose org_id is not the current organization, so
// none of this succeeds unless InTenantTx really set the context.
func seedEveryTable(ctx context.Context, t *testing.T, db *postgres.DB, label string) uuid.UUID {
	t.Helper()

	var (
		org      = uuid.New()
		user     = uuid.New()
		project  = uuid.New()
		env      = uuid.New()
		scenario = uuid.New()
		run      = uuid.New()
	)

	err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		statements := []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO organization (id, name) VALUES ($1, $2)`,
				[]any{org, label}},

			{`INSERT INTO app_user (id, org_id, subject, email) VALUES ($1, $2, $3, $4)`,
				[]any{user, org, label + "-subject", label + "@example.test"}},

			{`INSERT INTO project (id, org_id, name, slug) VALUES ($1, $2, $3, $4)`,
				[]any{project, org, label, label}},

			{`INSERT INTO project_allowed_host (org_id, project_id, host) VALUES ($1, $2, $3)`,
				[]any{org, project, label + ".example.test"}},

			{`INSERT INTO membership (org_id, user_id, project_id, role) VALUES ($1, $2, $3, 'admin')`,
				[]any{org, user, project}},

			{`INSERT INTO environment
				(id, org_id, project_id, name, base_url, verification_token, max_vus, max_duration_s)
			  VALUES ($1, $2, $3, 'staging', $4, $5, 10, 60)`,
				[]any{env, org, project, "https://" + label + ".example.test", label + "-token"}},

			{`INSERT INTO secret (org_id, environment_id, key, value_encrypted) VALUES ($1, $2, 'API_TOKEN', $3)`,
				[]any{org, env, []byte{0x01, 0x02}}},

			{`INSERT INTO scenario (id, org_id, project_id, name, script) VALUES ($1, $2, $3, 'checkout', $4)`,
				[]any{scenario, org, project, "export default function () {}"}},

			{`INSERT INTO scenario_version
				(org_id, scenario_id, version, script, default_profile)
			  VALUES ($1, $2, 1, 'export default function () {}', 'smoke')`,
				[]any{org, scenario}},

			{`INSERT INTO threshold (org_id, scenario_id, metric, expression) VALUES ($1, $2, 'http_req_duration', 'p(95)<300')`,
				[]any{org, scenario}},

			{`INSERT INTO run
				(id, org_id, project_id, scenario_id, environment_id, scenario_version, profile)
			  VALUES ($1, $2, $3, $4, $5, 1, 'smoke')`,
				[]any{run, org, project, scenario, env}},

			// One run at a time per environment, and the row that enforces it.
			{`INSERT INTO environment_lock
				(org_id, environment_id, run_id, acquired_at, expires_at)
			  VALUES ($1, $2, $3, now(), now() + interval '1 hour')`,
				[]any{org, env, run}},

			{`INSERT INTO run_metric (org_id, run_id, name, value) VALUES ($1, $2, 'http_reqs', 42)`,
				[]any{org, run}},

			{`INSERT INTO run_threshold (org_id, run_id, metric, expression, passed)
			  VALUES ($1, $2, 'http_req_duration', 'p(95)<300', true)`,
				[]any{org, run}},

			{`INSERT INTO run_artifact (org_id, run_id, kind, storage_key) VALUES ($1, $2, 'summary', $3)`,
				[]any{org, run, fmt.Sprintf("org/%s/run/%s/summary.json", org, run)}},

			{`INSERT INTO run_log (org_id, run_id, seq, stream, message) VALUES ($1, $2, 0, 'stdout', 'run started')`,
				[]any{org, run}},

			{`INSERT INTO audit_log (org_id, actor_id, action, target_type, target_id)
			  VALUES ($1, $2, 'run.enqueued', 'run', $3)`,
				[]any{org, user, run}},
		}

		for _, s := range statements {
			if _, err := tx.Exec(ctx, s.sql, s.args...); err != nil {
				return fmt.Errorf("%s: %w", s.sql, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed organization %q: %v", label, err)
	}

	t.Cleanup(func() { dropEverything(ctx, t, db, org) })

	return org
}

// Reverse of the insertion order. The cascade from organization would be
// shorter, but referential actions and FORCE ROW LEVEL SECURITY interact in
// ways this suite has no business depending on.
//
// audit_log is the one exception, and not by choice: the application role
// cannot delete from it, so the cascade from organization is what removes its
// rows here. A test that could clean up the journal by hand would be a test
// running with rights the application does not have.
func dropEverything(ctx context.Context, t *testing.T, db *postgres.DB, org uuid.UUID) {
	t.Helper()

	tables := []string{
		"run_log", "run_artifact", "run_threshold", "run_metric",
		"environment_lock", "run", "threshold", "scenario_version", "scenario", "secret",
		"environment", "membership",
		"project_allowed_host", "project", "app_user", "organization",
	}

	cleanup := context.WithoutCancel(ctx)
	err := db.InTenantTx(cleanup, org, func(tx pgx.Tx) error {
		for _, table := range tables {
			q := "DELETE FROM " + pgx.Identifier{table}.Sanitize() + " WHERE org_id = $1"
			if _, err := tx.Exec(cleanup, q, org); err != nil {
				return fmt.Errorf("%s: %w", table, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Errorf("clean up organization %s: %v", org, err)
	}
}

// Every table of the schema, read from the catalog rather than listed here.
func schemaTables(ctx context.Context, t *testing.T, f fixture) []string {
	t.Helper()

	var tables []string
	err := f.db.InTenantTx(ctx, f.orgA, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT tablename FROM pg_tables WHERE schemaname = 'public' ORDER BY tablename`)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return err
			}
			tables = append(tables, name)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("list the schema tables: %v", err)
	}

	if len(tables) == 0 {
		t.Fatal("the schema holds no table; has migration 001 been applied?")
	}
	return tables
}

func countWhereOrg(ctx context.Context, t *testing.T, tx pgx.Tx, table string, org uuid.UUID) int {
	t.Helper()

	q := "SELECT count(*) FROM " + pgx.Identifier{table}.Sanitize() + " WHERE org_id = $1"

	var n int
	if err := tx.QueryRow(ctx, q, org).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// The trap this guards against: a table added later is walked by every generic
// assertion below and, being empty, satisfies all of them without proving
// anything. Adding a table without extending seedEveryTable fails here.
func TestFixtureCoversEveryTable(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	tables := schemaTables(ctx, t, f)

	for _, org := range []struct {
		id    uuid.UUID
		label string
	}{{f.orgA, "alpha"}, {f.orgB, "beta"}} {
		if err := f.db.InTenantTx(ctx, org.id, func(tx pgx.Tx) error {
			for _, table := range tables {
				if n := countWhereOrg(ctx, t, tx, table, org.id); n == 0 {
					t.Errorf("%s holds no row for organization %s; seedEveryTable must cover it",
						table, org.label)
				}
			}
			return nil
		}); err != nil {
			t.Fatalf("read as %s: %v", org.label, err)
		}
	}
}

// USING, on every table.
func TestNoRowOfAnotherOrgIsVisible(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	tables := schemaTables(ctx, t, f)

	if err := f.db.InTenantTx(ctx, f.orgA, func(tx pgx.Tx) error {
		for _, table := range tables {
			if n := countWhereOrg(ctx, t, tx, table, f.orgB); n != 0 {
				t.Errorf("%s exposed %d rows of the other organization, want 0", table, n)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("read as alpha: %v", err)
	}
}

// WITH CHECK, on every table. One transaction per table: the first refusal
// aborts its own, and the others still have to be tried.
func TestCannotMoveARowToAnotherOrg(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)

	for _, table := range schemaTables(ctx, t, f) {
		wantCode := sqlStateInsufficientPrivilege
		if code, ok := expectedRefusalByTable[table]; ok {
			wantCode = code
		}

		q := "UPDATE " + pgx.Identifier{table}.Sanitize() + " SET org_id = $1 WHERE org_id = $2"

		var moved int64
		err := f.db.InTenantTx(ctx, f.orgA, func(tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, q, f.orgB, f.orgA)
			if err != nil {
				return err
			}
			moved = tag.RowsAffected()
			return errRollback
		})

		switch {
		case errors.Is(err, errRollback) && moved == 0:
			// Nothing to hand over, so nothing was proved. The fixture is what
			// has to be fixed, and TestFixtureCoversEveryTable says so too.
			t.Errorf("%s: no row of this organization to move; seedEveryTable must cover it", table)
			continue
		case errors.Is(err, errRollback):
			t.Errorf("%s: %d rows were handed over to the other organization", table, moved)
			continue
		}

		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) {
			t.Errorf("%s: want a PostgreSQL error, got %T: %v", table, err, err)
			continue
		}
		if pgErr.Code != wantCode {
			t.Errorf("%s: want SQLSTATE %s, got %s: %s", table, wantCode, pgErr.Code, pgErr.Message)
		}
	}
}

// Writing to rows that are not visible must be a no-op, not an error: a missing
// WHERE clause has to degrade into touching nothing rather than into touching
// the other tenant. The delete is rolled back either way.
func TestCannotTouchAnotherOrgRows(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)

	for _, table := range schemaTables(ctx, t, f) {
		q := "DELETE FROM " + pgx.Identifier{table}.Sanitize() + " WHERE org_id = $1"

		err := f.db.InTenantTx(ctx, f.orgA, func(tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, q, f.orgB)
			if err != nil {
				return err
			}
			if n := tag.RowsAffected(); n != 0 {
				t.Errorf("%s: deleted %d rows of the other organization, want 0", table, n)
			}
			return errRollback
		})

		// An append-only table answers something stronger than "nothing was
		// touched": the statement is refused outright, for every row of every
		// organization including this one's.
		if appendOnly[table] {
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != sqlStateInsufficientPrivilege {
				t.Errorf("%s is append-only and a delete answered %v, want SQLSTATE %s",
					table, err, sqlStateInsufficientPrivilege)
			}
			continue
		}

		if err != nil && !errors.Is(err, errRollback) {
			t.Errorf("%s: %v", table, err)
		}
	}
}
