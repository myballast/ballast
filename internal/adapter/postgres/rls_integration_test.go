//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// PostgreSQL error codes worth naming.
const (
	sqlStateInsufficientPrivilege = "42501" // raised when WITH CHECK rejects a row
)

func newOrgID(ctx context.Context, t *testing.T, conn *pgx.Conn) string {
	t.Helper()

	var id string
	if err := conn.QueryRow(ctx, "SELECT gen_random_uuid()").Scan(&id); err != nil {
		t.Fatalf("generate uuid: %v", err)
	}
	return id
}

// asOrg runs fn inside a transaction scoped to org, exactly the way InTenantTx
// will: set_config with a third argument of true, so the setting dies with the
// transaction instead of leaking to the next user of a pooled connection.
func asOrg(ctx context.Context, t *testing.T, conn *pgx.Conn, org string, fn func(tx pgx.Tx) error) error {
	t.Helper()

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "SELECT set_config('app.current_org', $1, true)", org); err != nil {
		t.Fatalf("set app.current_org: %v", err)
	}

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// seedOrg creates one organization and one project belonging to it, through the
// application role and its policies — not through a privileged back door, which
// would prove nothing about the policies themselves.
//
// Two tables are enough here. That every other table carries the same policy is
// the job of the catalog tests in schema_integration_test.go; what these tests
// establish is that the mechanism itself works.
func seedOrg(ctx context.Context, t *testing.T, conn *pgx.Conn, name string) string {
	t.Helper()

	org := newOrgID(ctx, t, conn)

	err := asOrg(ctx, t, conn, org, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			"INSERT INTO organization (id, name) VALUES ($1, $2)", org, name); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			"INSERT INTO project (id, org_id, name, slug) VALUES (gen_random_uuid(), $1, $2, $3)",
			org, name+" project", name)
		return err
	})
	if err != nil {
		t.Fatalf("seed organization %q: %v", name, err)
	}

	t.Cleanup(func() {
		cleanup := context.WithoutCancel(ctx)
		_ = asOrg(cleanup, t, conn, org, func(tx pgx.Tx) error {
			if _, err := tx.Exec(cleanup, "DELETE FROM project WHERE org_id = $1", org); err != nil {
				return err
			}
			_, err := tx.Exec(cleanup, "DELETE FROM organization WHERE id = $1", org)
			return err
		})
	})

	return org
}

func countRows(ctx context.Context, t *testing.T, q pgx.Tx, table string) int {
	t.Helper()

	var n int
	// The table name comes from a constant in this file, never from input.
	if err := q.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// The rows exist. Read them without announcing an organization and the database
// must behave as if they did not.
func TestNothingIsReadableWithoutOrgContext(t *testing.T) {
	ctx, conn := connect(t)
	org := seedOrg(ctx, t, conn, "alpha")

	// First prove the rows are really there, otherwise the assertion below
	// would hold just as well against an empty database.
	if err := asOrg(ctx, t, conn, org, func(tx pgx.Tx) error {
		for _, table := range []string{"organization", "project"} {
			if n := countRows(ctx, t, tx, table); n != 1 {
				t.Errorf("within its own org, %s holds %d rows, want 1", table, n)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("read as owning org: %v", err)
	}

	// No set_config anywhere in this transaction.
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, table := range []string{"organization", "project"} {
		if n := countRows(ctx, t, tx, table); n != 0 {
			t.Errorf("without app.current_org, %s exposed %d rows, want 0", table, n)
		}
	}
}

// USING hides the other tenant's rows. WITH CHECK is what stops a write from
// planting a row in their organization.
func TestInsertIntoAnotherOrgIsRejected(t *testing.T) {
	ctx, conn := connect(t)

	orgA := seedOrg(ctx, t, conn, "alpha")
	orgB := seedOrg(ctx, t, conn, "beta")

	err := asOrg(ctx, t, conn, orgA, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			"INSERT INTO project (id, org_id, name, slug) VALUES (gen_random_uuid(), $1, $2, $3)",
			orgB, "smuggled", "smuggled")
		return err
	})

	if err == nil {
		t.Fatal("inserting a row owned by another organization was accepted")
	}

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("want a PostgreSQL error, got %T: %v", err, err)
	}
	if pgErr.Code != sqlStateInsufficientPrivilege {
		t.Fatalf("want SQLSTATE %s (row-level security violation), got %s: %s",
			sqlStateInsufficientPrivilege, pgErr.Code, pgErr.Message)
	}
}

// Reading another organization's rows must come back empty rather than fail, so
// a missing WHERE clause degrades into no data instead of into that tenant's
// data.
func TestReadsAreScopedToTheCurrentOrg(t *testing.T) {
	ctx, conn := connect(t)

	orgA := seedOrg(ctx, t, conn, "alpha")
	seedOrg(ctx, t, conn, "beta")

	if err := asOrg(ctx, t, conn, orgA, func(tx pgx.Tx) error {
		if n := countRows(ctx, t, tx, "organization"); n != 1 {
			t.Errorf("organization exposed %d rows to org alpha, want only its own", n)
		}
		if n := countRows(ctx, t, tx, "project"); n != 1 {
			t.Errorf("project exposed %d rows to org alpha, want only its own", n)
		}
		return nil
	}); err != nil {
		t.Fatalf("read as alpha: %v", err)
	}
}
