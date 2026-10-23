//go:build integration

// These tests live inside the package rather than beside it. They need to run a
// query on the raw pool, outside any tenant transaction, and that is exactly
// what production code must never be able to do: exposing an accessor for it
// would punch the hole these tests exist to prove is closed.
package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Matches the application role created by ops/sql/00-app-role.sql.
const defaultTenantTestDSN = "postgres://ballast_app:ballast_app@127.0.0.1:5432/ballast?sslmode=disable"

// A pool of exactly one connection, so that "the same physical connection" is a
// property of the setup rather than something the test hopes for.
func openSingleConnDB(ctx context.Context, t *testing.T) *DB {
	t.Helper()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = defaultTenantTestDSN
	}
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	dsn += separator + "pool_max_conns=1"

	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open %q: %v", dsn, err)
	}
	t.Cleanup(db.Close)

	return db
}

func testContext(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	return ctx
}

// The insert below is itself an assertion. The WITH CHECK of migration 001
// rejects any row whose org_id is not the current organization, so this only
// succeeds if InTenantTx really did set the context.
func seedOrganization(ctx context.Context, t *testing.T, db *DB) uuid.UUID {
	t.Helper()

	org := uuid.New()
	err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			"INSERT INTO organization (id, name) VALUES ($1, $2)", org, "tx test")
		return err
	})
	if err != nil {
		t.Fatalf("seed organization: %v", err)
	}

	t.Cleanup(func() {
		cleanup := context.WithoutCancel(ctx)
		_ = db.InTenantTx(cleanup, org, func(tx pgx.Tx) error {
			_, err := tx.Exec(cleanup, "DELETE FROM organization WHERE id = $1", org)
			return err
		})
	})

	return org
}

// Reads the table straight from the pool, with no tenant transaction around it.
// With a single connection in the pool this is necessarily the connection that
// just served InTenantTx.
func organizationsVisibleOnTheRawConnection(ctx context.Context, t *testing.T, db *DB) int {
	t.Helper()

	var n int
	if err := db.pool.QueryRow(ctx, "SELECT count(*) FROM organization").Scan(&n); err != nil {
		t.Fatalf("count on the raw connection: %v", err)
	}
	return n
}

func countOrganizations(ctx context.Context, t *testing.T, tx pgx.Tx) int {
	t.Helper()

	var n int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM organization").Scan(&n); err != nil {
		t.Fatalf("count within the tenant transaction: %v", err)
	}
	return n
}

// The row is committed and the connection goes back to the pool carrying
// whatever the transaction left behind. Nothing must remain.
func TestSettingIsGoneAfterCommit(t *testing.T) {
	ctx := testContext(t)
	db := openSingleConnDB(ctx, t)
	org := seedOrganization(ctx, t, db)

	if err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		if n := countOrganizations(ctx, t, tx); n != 1 {
			t.Errorf("within its own organization, saw %d rows, want 1", n)
		}
		return nil
	}); err != nil {
		t.Fatalf("read within the tenant transaction: %v", err)
	}

	if n := organizationsVisibleOnTheRawConnection(ctx, t, db); n != 0 {
		t.Errorf("after commit, the connection still exposed %d rows, want 0", n)
	}
}

// A failed transaction is the more dangerous case: the setting was applied, the
// work was undone, and the connection is handed back to the pool anyway.
func TestSettingIsGoneAfterRollback(t *testing.T) {
	ctx := testContext(t)
	db := openSingleConnDB(ctx, t)
	org := seedOrganization(ctx, t, db)

	failure := errors.New("deliberate failure inside the transaction")

	err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		if n := countOrganizations(ctx, t, tx); n != 1 {
			t.Errorf("within its own organization, saw %d rows, want 1", n)
		}
		return failure
	})
	if !errors.Is(err, failure) {
		t.Fatalf("got %v, want the callback's own error", err)
	}

	if n := organizationsVisibleOnTheRawConnection(ctx, t, db); n != 0 {
		t.Errorf("after rollback, the connection still exposed %d rows, want 0", n)
	}
}

// The connection is returned to the pool and taken out again, which is what
// happens between two unrelated HTTP requests.
func TestSettingDoesNotSurviveAcquireRelease(t *testing.T) {
	ctx := testContext(t)
	db := openSingleConnDB(ctx, t)
	org := seedOrganization(ctx, t, db)

	if err := db.InTenantTx(ctx, org, func(pgx.Tx) error { return nil }); err != nil {
		t.Fatalf("tenant transaction: %v", err)
	}

	conn, err := db.pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()

	var n int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM organization").Scan(&n); err != nil {
		t.Fatalf("count on the reacquired connection: %v", err)
	}
	if n != 0 {
		t.Errorf("a reacquired connection exposed %d rows, want 0", n)
	}
}

// Two organizations, one connection, one after the other. The second must not
// inherit anything from the first.
func TestOneTenantTxCannotSeeAnotherOrg(t *testing.T) {
	ctx := testContext(t)
	db := openSingleConnDB(ctx, t)

	orgA := seedOrganization(ctx, t, db)
	seedOrganization(ctx, t, db)

	if err := db.InTenantTx(ctx, orgA, func(tx pgx.Tx) error {
		if n := countOrganizations(ctx, t, tx); n != 1 {
			t.Errorf("organization A saw %d rows, want only its own", n)
		}
		return nil
	}); err != nil {
		t.Fatalf("read as organization A: %v", err)
	}
}
