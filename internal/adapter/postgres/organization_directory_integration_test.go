//go:build integration

package postgres_test

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres"
)

// Row-level security hides every organization from every other, and so from a
// worker that serves all of them: it cannot ask "who has work?" through a table
// it can read. organizations_with_work() and organization_ids() are the second
// place, after auth_lookup, where the application reads across tenants — and these
// tests pin down how narrow that opening is. Each of them would pass against a
// function that leaked more, were it not checking for the leak explicitly.

// setRunStatus moves the one run the fixture gave an organization to a status.
func setRunStatus(ctx context.Context, t *testing.T, f fixture, org uuid.UUID, status string) {
	t.Helper()

	err := f.db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE run SET status = $2, started_at = now(),
			        ended_at = CASE WHEN $2 IN ('queued', 'running', 'orphaned') THEN NULL ELSE now() END
			 WHERE org_id = $1`, org, status)
		return err
	})
	if err != nil {
		t.Fatalf("set the run of %s to %s: %v", org, status, err)
	}
}

func TestOrganizationsWithWorkAreThoseWithAQueuedRunningOrOrphanedRun(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	directory := postgres.NewOrganizationDirectory(f.db)

	for _, tc := range []struct {
		status string
		listed bool
	}{
		{"queued", true},
		{"running", true},
		{"orphaned", true},
		// A finished run is not work. An organization whose runs are all over is
		// asked about by nobody, which is what keeps the poll cheap.
		{"passed", false},
		{"failed", false},
		{"errored", false},
		{"timeout", false},
		{"cancelled", false},
		{"rejected", false},
		{"skipped", false},
	} {
		setRunStatus(ctx, t, f, f.orgB, tc.status)

		got, err := directory.WithWork(ctx)
		if err != nil {
			t.Fatalf("%s: with work: %v", tc.status, err)
		}
		if listed := slices.Contains(got, f.orgB); listed != tc.listed {
			t.Errorf("an organization whose run is %s: listed = %v, want %v", tc.status, listed, tc.listed)
		}
		// The other organization's queued run is there throughout: one
		// organization's state does not move another.
		if !slices.Contains(got, f.orgA) {
			t.Errorf("%s: the organization with a queued run was missing", tc.status)
		}
	}
}

// Running and orphaned count because the sweep and the collector need them: a
// worker that restarts must know which organizations hold a run it has not seen.
// An organization with only a running run has nothing queued, and still appears.
func TestAnOrganizationWithOnlyARunningRunIsListed(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)

	setRunStatus(ctx, t, f, f.orgA, "running")

	got, err := postgres.NewOrganizationDirectory(f.db).WithWork(ctx)
	if err != nil {
		t.Fatalf("with work: %v", err)
	}
	if !slices.Contains(got, f.orgA) {
		t.Fatal("an organization with a run in flight was not listed")
	}
}

// An organization with nothing to do still has a worker that serves it, and has
// to be able to see it online.
func TestEveryOrganizationIsListedWhetherOrNotItHasWork(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)

	setRunStatus(ctx, t, f, f.orgB, "passed")

	got, err := postgres.NewOrganizationDirectory(f.db).All(ctx)
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if !slices.Contains(got, f.orgA) || !slices.Contains(got, f.orgB) {
		t.Fatalf("an organization was missing from the list of all of them: %v", got)
	}
}

// What the opening does not open. Calling the functions in a session that has
// announced no organization leaves every table as unreadable as before: the
// functions return identifiers, they do not switch row-level security off.
func TestAskingForTheListDoesNotOpenAnyTable(t *testing.T) {
	ctx, conn := connect(t)
	_ = newFixture(ctx, t)

	for _, fn := range []string{"organizations_with_work", "organization_ids"} {
		rows, err := conn.Query(ctx, "SELECT * FROM "+fn+"()")
		if err != nil {
			t.Fatalf("%s: %v", fn, err)
		}
		rows.Close()
	}

	for _, table := range []string{"run", "organization", "app_user", "environment", "secret"} {
		var n int
		if err := conn.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%d row(s) of %s are readable with no organization announced after asking for the list", n, table)
		}
	}
}

// One column, and it is an identifier. A function that grew a name, a status or a
// count of runs would be telling every worker things about every customer.
func TestTheListsHaveOneColumnAndItIsAnIdentifier(t *testing.T) {
	ctx, conn := connect(t)

	const uuidOID = 2950

	for _, fn := range []string{"organizations_with_work", "organization_ids"} {
		rows, err := conn.Query(ctx, "SELECT * FROM "+fn+"()")
		if err != nil {
			t.Fatalf("%s: %v", fn, err)
		}
		fields := rows.FieldDescriptions()
		rows.Close()

		if len(fields) != 1 || fields[0].Name != "org_id" || fields[0].DataTypeOID != uuidOID {
			t.Errorf("%s returns %d column(s) %+v, want one uuid called org_id", fn, len(fields), fields)
		}
	}
}

// Who may ask. PUBLIC may not; the application role may; and the function runs as
// a role nobody can sign in as, with a search path of its own so that a caller
// cannot put an object of theirs where the function would find it first.
func TestTheListsAreForTheApplicationRoleOnlyAndRunAsARoleNobodyCanSignInAs(t *testing.T) {
	ctx, conn := connect(t)

	for _, fn := range []string{"organizations_with_work", "organization_ids"} {
		var (
			grantedToPublic int
			hasACL          bool
			appMayRun       bool
			definer         bool
			pinned          bool
			canLogin        bool
			isSuper         bool
		)
		err := conn.QueryRow(ctx, `
			SELECT
			    (SELECT count(*) FROM aclexplode(p.proacl) a WHERE a.grantee = 0),
			    p.proacl IS NOT NULL,
			    has_function_privilege('ballast_app', p.oid, 'EXECUTE'),
			    p.prosecdef,
			    coalesce(p.proconfig::text[] @> ARRAY['search_path=pg_catalog, public'], false),
			    r.rolcanlogin,
			    r.rolsuper
			FROM pg_proc p
			JOIN pg_roles r ON r.oid = p.proowner
			WHERE p.proname = $1`, fn).
			Scan(&grantedToPublic, &hasACL, &appMayRun, &definer, &pinned, &canLogin, &isSuper)
		if err != nil {
			t.Fatalf("%s: read the catalog: %v", fn, err)
		}

		if !hasACL || grantedToPublic != 0 {
			t.Errorf("%s: EXECUTE is granted to PUBLIC (acl set: %v)", fn, hasACL)
		}
		if !appMayRun {
			t.Errorf("%s: the application role may not run it", fn)
		}
		if !definer || !pinned {
			t.Errorf("%s: security definer = %v, search_path pinned = %v; both are required", fn, definer, pinned)
		}
		if canLogin || isSuper {
			t.Errorf("%s: it runs as a role that can sign in (%v) or is a superuser (%v)", fn, canLogin, isSuper)
		}
	}
}

// What the role the functions run as can read, and nothing else: one status and
// one organization column of the runs, and the identifier of an organization. It
// reads nothing it cannot name here, and it writes nothing at all.
func TestTheRoleBehindTheListsReadsAFewColumnsAndWritesNothing(t *testing.T) {
	ctx, conn := connect(t)

	readable := func(table string) []string {
		rows, err := conn.Query(ctx, `
			SELECT a.attname
			FROM pg_attribute a
			WHERE a.attrelid = $1::regclass
			  AND a.attnum > 0 AND NOT a.attisdropped
			  AND has_column_privilege('ballast_scheduler', a.attrelid, a.attnum, 'SELECT')
			ORDER BY a.attname`, table)
		if err != nil {
			t.Fatalf("columns of %s: %v", table, err)
		}
		defer rows.Close()

		var names []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				t.Fatalf("scan: %v", err)
			}
			names = append(names, name)
		}
		return names
	}

	if got := readable("run"); !slices.Equal(got, []string{"org_id", "status"}) {
		t.Errorf("the role reads %v of run, want org_id and status", got)
	}
	if got := readable("organization"); !slices.Equal(got, []string{"id"}) {
		t.Errorf("the role reads %v of organization, want id", got)
	}

	var writes int
	err := conn.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.role_table_grants
		WHERE grantee = 'ballast_scheduler'
		  AND privilege_type IN ('INSERT', 'UPDATE', 'DELETE', 'TRUNCATE')`).Scan(&writes)
	if err != nil {
		t.Fatalf("grants: %v", err)
	}
	if writes != 0 {
		t.Errorf("the role holds %d write privilege(s)", writes)
	}
}
