//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

// auth_lookup is the one place where the application reads across tenants: at
// sign-in, the organization is precisely what is not known yet. These tests pin
// down how narrow that opening is. Each of them would pass against a function
// that leaked more, were it not checking for the leak explicitly.

// seedUser creates a user in org through the application role and its
// policies, and returns the user's id.
func seedUser(ctx context.Context, t *testing.T, conn *pgx.Conn, org, subject, email string) string {
	t.Helper()

	var id string
	err := asOrg(ctx, t, conn, org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			"INSERT INTO app_user (org_id, subject, email) VALUES ($1, $2, $3) RETURNING id",
			org, subject, email).Scan(&id)
	})
	if err != nil {
		t.Fatalf("seed user %q: %v", subject, err)
	}
	return id
}

type identity struct {
	orgID  string
	userID string
}

// lookup calls auth_lookup in a transaction that never announces an
// organization, which is the situation at sign-in.
func lookup(ctx context.Context, t *testing.T, conn *pgx.Conn, subject string) []identity {
	t.Helper()

	rows, err := conn.Query(ctx, "SELECT org_id, user_id FROM auth_lookup($1)", subject)
	if err != nil {
		t.Fatalf("auth_lookup(%q): %v", subject, err)
	}
	defer rows.Close()

	var found []identity
	for rows.Next() {
		var id identity
		if err := rows.Scan(&id.orgID, &id.userID); err != nil {
			t.Fatalf("scan: %v", err)
		}
		found = append(found, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return found
}

// The reason the function exists: without any organization in the context, a
// subject is resolved to the organization and user it belongs to.
func TestAuthLookupResolvesASubjectWithoutOrgContext(t *testing.T) {
	ctx, conn := connect(t)

	orgA := seedOrg(ctx, t, conn, "alpha")
	orgB := seedOrg(ctx, t, conn, "beta")
	userA := seedUser(ctx, t, conn, orgA, "test|alpha-user", "a@example.test")
	seedUser(ctx, t, conn, orgB, "test|beta-user", "b@example.test")

	got := lookup(ctx, t, conn, "test|alpha-user")

	if len(got) != 1 {
		t.Fatalf("auth_lookup returned %d rows, want exactly 1: %v", len(got), got)
	}
	if got[0].orgID != orgA || got[0].userID != userA {
		t.Errorf("auth_lookup returned %+v, want org %s and user %s", got[0], orgA, userA)
	}
}

func TestAuthLookupReturnsNothingForAnUnknownSubject(t *testing.T) {
	ctx, conn := connect(t)

	org := seedOrg(ctx, t, conn, "alpha")
	seedUser(ctx, t, conn, org, "test|known", "known@example.test")

	if got := lookup(ctx, t, conn, "test|unknown"); len(got) != 0 {
		t.Errorf("an unknown subject resolved to %v", got)
	}
}

// The subject is compared whole. A prefix or a pattern must not match, or a
// crafted subject could resolve to someone else.
func TestAuthLookupMatchesTheWholeSubjectOnly(t *testing.T) {
	ctx, conn := connect(t)

	org := seedOrg(ctx, t, conn, "alpha")
	seedUser(ctx, t, conn, org, "test|12345", "n@example.test")

	for _, subject := range []string{"test|1234", "test|%", "test|12345 ", "TEST|12345"} {
		if got := lookup(ctx, t, conn, subject); len(got) != 0 {
			t.Errorf("subject %q resolved to %v", subject, got)
		}
	}
}

// Two identifiers and nothing else: no email, no name, nothing a caller could
// use to learn about a tenant it has not signed in to.
func TestAuthLookupReturnsOnlyTwoIdentifiers(t *testing.T) {
	ctx, conn := connect(t)

	rows, err := conn.Query(ctx, "SELECT * FROM auth_lookup($1)", "test|nobody")
	if err != nil {
		t.Fatalf("auth_lookup: %v", err)
	}
	fields := rows.FieldDescriptions()
	rows.Close()

	var names []string
	for _, f := range fields {
		names = append(names, f.Name)
	}
	if len(names) != 2 || names[0] != "org_id" || names[1] != "user_id" {
		t.Errorf("auth_lookup returns columns %v, want [org_id user_id]", names)
	}
}

// Calling the function must not leave the caller with any wider view than it
// had: row-level security still hides every row afterwards, in the same
// transaction.
func TestAuthLookupDoesNotOpenRowLevelSecurity(t *testing.T) {
	ctx, conn := connect(t)

	org := seedOrg(ctx, t, conn, "alpha")
	seedUser(ctx, t, conn, org, "test|alpha-user", "a@example.test")

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var n int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM auth_lookup($1)", "test|alpha-user").Scan(&n); err != nil {
		t.Fatalf("auth_lookup: %v", err)
	}
	if n != 1 {
		t.Fatalf("auth_lookup found %d rows, want 1; the check below would prove nothing", n)
	}

	for _, table := range []string{"app_user", "organization", "project"} {
		if n := countRows(ctx, t, tx, table); n != 0 {
			t.Errorf("after auth_lookup, %s exposed %d rows without an organization", table, n)
		}
	}
}

// The function runs with its owner's rights, so the owner is what bounds it.
// It must be a role nobody can sign in as, able to read the four columns the
// lookup needs and nothing else in the schema.
func TestAuthLookupOwnerCanReadNothingElse(t *testing.T) {
	ctx, conn := connect(t)

	var owner string
	var canLogin, bypassRLS bool
	err := conn.QueryRow(ctx, `
		SELECT r.rolname, r.rolcanlogin, r.rolbypassrls
		FROM pg_proc p
		JOIN pg_roles r ON r.oid = p.proowner
		WHERE p.proname = 'auth_lookup'`).Scan(&owner, &canLogin, &bypassRLS)
	if err != nil {
		t.Fatalf("read the owner of auth_lookup: %v", err)
	}

	if owner != "ballast_auth" {
		t.Errorf("auth_lookup is owned by %q, want the dedicated role ballast_auth", owner)
	}
	if canLogin {
		t.Errorf("%s can log in; it must exist only to own auth_lookup", owner)
	}
	// Without it, FORCE ROW LEVEL SECURITY would hide every row from the
	// function as well, and sign-in would find nobody.
	if !bypassRLS {
		t.Errorf("%s lacks BYPASSRLS; auth_lookup would see no row", owner)
	}

	violations(ctx, t, conn, "the auth_lookup owner must not read any other table", `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public'
		  AND c.relkind = 'r'
		  AND c.relname <> 'app_user'
		  AND has_table_privilege('ballast_auth', c.oid, 'SELECT')`)

	violations(ctx, t, conn, "the auth_lookup owner must read only id, org_id, subject and created_at", `
		SELECT a.attname
		FROM pg_attribute a
		WHERE a.attrelid = 'public.app_user'::regclass
		  AND a.attnum > 0
		  AND NOT a.attisdropped
		  AND a.attname NOT IN ('id', 'org_id', 'subject', 'created_at')
		  AND has_column_privilege('ballast_auth', a.attrelid, a.attnum, 'SELECT')`)

	violations(ctx, t, conn, "the auth_lookup owner must not write anywhere", `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public'
		  AND c.relkind = 'r'
		  AND (has_table_privilege('ballast_auth', c.oid, 'INSERT')
		    OR has_table_privilege('ballast_auth', c.oid, 'UPDATE')
		    OR has_table_privilege('ballast_auth', c.oid, 'DELETE'))`)
}

// A SECURITY DEFINER function that resolves names through the caller's
// search_path can be made to run the caller's own objects with the owner's
// rights. The path is pinned, and only the application role may call it.
func TestAuthLookupIsPinnedAndNotPublic(t *testing.T) {
	ctx, conn := connect(t)

	var securityDefiner, pinned, publicCanExecute, appCanExecute bool
	err := conn.QueryRow(ctx, `
		SELECT p.prosecdef,
		       coalesce(array_to_string(p.proconfig, ',') LIKE '%search_path=%', false),
		       EXISTS (SELECT 1 FROM aclexplode(coalesce(p.proacl, acldefault('f', p.proowner))) a
		               WHERE a.grantee = 0 AND a.privilege_type = 'EXECUTE'),
		       has_function_privilege('ballast_app', p.oid, 'EXECUTE')
		FROM pg_proc p
		WHERE p.proname = 'auth_lookup'`).Scan(&securityDefiner, &pinned, &publicCanExecute, &appCanExecute)
	if err != nil {
		t.Fatalf("read auth_lookup: %v", err)
	}

	if !securityDefiner {
		t.Error("auth_lookup is not SECURITY DEFINER")
	}
	if !pinned {
		t.Error("auth_lookup does not pin its search_path")
	}
	if publicCanExecute {
		t.Error("PUBLIC may execute auth_lookup; only ballast_app should")
	}
	if !appCanExecute {
		t.Error("ballast_app may not execute auth_lookup")
	}
}
