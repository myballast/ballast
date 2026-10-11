//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

// Every table the schema is expected to define. This is the one list in this
// file that is written by hand, and it exists for a single reason: every other
// test here asks "which tables violate the rule?" and passes when the answer is
// none. Against an empty database they would all pass while proving nothing.
// This one fails instead.
var expectedTables = []string{
	"app_user",
	"audit_log",
	"environment",
	"environment_lock",
	"membership",
	"organization",
	"project",
	"project_allowed_host",
	"run",
	"run_artifact",
	"run_log",
	"run_metric",
	"run_threshold",
	"scenario",
	"scenario_version",
	"secret",
	"threshold",
}

// violations runs a query that is expected to return the name of every table
// breaking an invariant, and fails the test with that list.
func violations(ctx context.Context, t *testing.T, conn *pgx.Conn, rule, query string) {
	t.Helper()

	rows, err := conn.Query(ctx, query)
	if err != nil {
		t.Fatalf("%s: query: %v", rule, err)
	}
	defer rows.Close()

	var offenders []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("%s: scan: %v", rule, err)
		}
		offenders = append(offenders, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("%s: rows: %v", rule, err)
	}

	if len(offenders) > 0 {
		t.Errorf("%s — offending tables: %v", rule, offenders)
	}
}

func TestSchemaDefinesEveryExpectedTable(t *testing.T) {
	ctx, conn := connect(t)

	const q = `
		SELECT c.relname
		FROM unnest($1::text[]) AS c(relname)
		WHERE NOT EXISTS (
			SELECT 1
			FROM pg_class t
			JOIN pg_namespace n ON n.oid = t.relnamespace
			WHERE n.nspname = 'public'
			  AND t.relkind = 'r'
			  AND t.relname = c.relname
		)`

	rows, err := conn.Query(ctx, q, expectedTables)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	var missing []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		missing = append(missing, name)
	}
	if len(missing) > 0 {
		t.Fatalf("migration 001 did not create: %v", missing)
	}
}

// The public schema must hold nothing but the application tables. Bookkeeping
// that belongs to the migration tool lives in its own schema, so that the rules
// below can be stated over every table in public without an exception list —
// an exception list is where the next unprotected table hides.
func TestPublicSchemaHoldsOnlyApplicationTables(t *testing.T) {
	ctx, conn := connect(t)

	const q = `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public'
		  AND c.relkind = 'r'
		  AND NOT (c.relname = ANY($1::text[]))`

	rows, err := conn.Query(ctx, q, expectedTables)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	var unexpected []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		unexpected = append(unexpected, name)
	}
	if len(unexpected) > 0 {
		t.Errorf("unexpected tables in public: %v", unexpected)
	}
}

func TestEveryTableCarriesOrgID(t *testing.T) {
	ctx, conn := connect(t)

	violations(ctx, t, conn, "every table must carry org_id uuid NOT NULL", `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public'
		  AND c.relkind = 'r'
		  AND NOT EXISTS (
			SELECT 1
			FROM pg_attribute a
			WHERE a.attrelid = c.oid
			  AND a.attname = 'org_id'
			  AND NOT a.attisdropped
			  AND a.atttypid = 'uuid'::regtype
			  AND a.attnotnull
		  )`)
}

// ENABLE alone is not enough: without FORCE, the role that owns the table reads
// straight past every policy.
func TestEveryTableEnablesAndForcesRLS(t *testing.T) {
	ctx, conn := connect(t)

	violations(ctx, t, conn, "every table must ENABLE and FORCE row level security", `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public'
		  AND c.relkind = 'r'
		  AND NOT (c.relrowsecurity AND c.relforcerowsecurity)`)
}

// A policy with USING but no WITH CHECK hides other tenants' rows while still
// accepting a write that carries their org_id.
func TestEveryTableHasPolicyWithUsingAndWithCheck(t *testing.T) {
	ctx, conn := connect(t)

	violations(ctx, t, conn, "every table must have a policy with both USING and WITH CHECK", `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public'
		  AND c.relkind = 'r'
		  AND NOT EXISTS (
			SELECT 1
			FROM pg_policies p
			WHERE p.schemaname = 'public'
			  AND p.tablename = c.relname
			  AND p.qual IS NOT NULL
			  AND p.with_check IS NOT NULL
		  )`)
}

func TestEveryTableIsIndexedByOrgIDFirst(t *testing.T) {
	ctx, conn := connect(t)

	violations(ctx, t, conn, "every table must have an index whose first column is org_id", `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public'
		  AND c.relkind = 'r'
		  AND NOT EXISTS (
			SELECT 1
			FROM pg_index i
			JOIN pg_attribute a
			  ON a.attrelid = c.oid
			 AND a.attnum = i.indkey[0]
			WHERE i.indrelid = c.oid
			  AND a.attname = 'org_id'
		  )`)
}

// FORCE ROW LEVEL SECURITY closes the owner's bypass, but owning the tables
// would still let the application drop a policy. It owns none of them.
func TestApplicationRoleOwnsNoTable(t *testing.T) {
	ctx, conn := connect(t)

	violations(ctx, t, conn, "the application role must own no table", `
		SELECT tablename
		FROM pg_tables
		WHERE schemaname = 'public'
		  AND tableowner = current_user`)
}
