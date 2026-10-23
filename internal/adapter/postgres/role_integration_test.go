//go:build integration

package postgres_test

import (
	"testing"
)

// TestApplicationRoleIsLeastPrivileged pins down what ops/sql/00-app-role.sql
// promises, on the role this suite really connects as. It is the check that
// runs in CI before a merge: a role that could rewrite a policy, create a table,
// or read the migration bookkeeping is a role whose isolation tests would still
// pass, and prove less than they appear to.
//
// Each assertion reads the catalog rather than trying the forbidden thing, so
// that a failure names the privilege that is too wide.
func TestApplicationRoleIsLeastPrivileged(t *testing.T) {
	ctx, conn := connect(t)

	t.Run("attributes", func(t *testing.T) {
		var (
			name                                                string
			super, bypassRLS, createDB, createRole, replication bool
			canLogin                                            bool
		)
		const q = `SELECT rolname, rolsuper, rolbypassrls, rolcreatedb, rolcreaterole, rolreplication, rolcanlogin
		             FROM pg_roles WHERE rolname = current_user`
		if err := conn.QueryRow(ctx, q).Scan(&name, &super, &bypassRLS, &createDB, &createRole, &replication, &canLogin); err != nil {
			t.Fatalf("read role attributes: %v", err)
		}

		for attribute, got := range map[string]bool{
			"SUPERUSER": super, "BYPASSRLS": bypassRLS, "CREATEDB": createDB,
			"CREATEROLE": createRole, "REPLICATION": replication,
		} {
			if got {
				t.Errorf("role %q has %s", name, attribute)
			}
		}
		if !canLogin {
			t.Errorf("role %q cannot log in", name)
		}
	})

	t.Run("member of no other role", func(t *testing.T) {
		// Membership inherits privileges: a role in pg_write_all_data or in the
		// owner role has everything this test is about to deny.
		var memberOf []string
		const q = `SELECT coalesce(array_agg(r.rolname ORDER BY r.rolname), '{}')
		             FROM pg_auth_members m
		             JOIN pg_roles r ON r.oid = m.roleid
		            WHERE m.member = (SELECT oid FROM pg_roles WHERE rolname = current_user)`
		if err := conn.QueryRow(ctx, q).Scan(&memberOf); err != nil {
			t.Fatalf("read memberships: %v", err)
		}
		if len(memberOf) != 0 {
			t.Errorf("role is a member of %v", memberOf)
		}
	})

	t.Run("owns nothing", func(t *testing.T) {
		// The owner of a table is exempt from its policies unless FORCE is set;
		// owning nothing is the simpler guarantee.
		var owned []string
		const q = `SELECT coalesce(array_agg(c.relname ORDER BY c.relname), '{}')
		             FROM pg_class c
		            WHERE c.relowner = (SELECT oid FROM pg_roles WHERE rolname = current_user)
		              AND c.relkind IN ('r', 'p', 'v', 'm', 'S', 'f')`
		if err := conn.QueryRow(ctx, q).Scan(&owned); err != nil {
			t.Fatalf("read owned relations: %v", err)
		}
		if len(owned) != 0 {
			t.Errorf("role owns %v", owned)
		}
	})

	t.Run("no DDL", func(t *testing.T) {
		for _, c := range []struct{ what, query string }{
			{"CREATE on schema public", `SELECT has_schema_privilege(current_user, 'public', 'CREATE')`},
			{"CREATE on the database", `SELECT has_database_privilege(current_user, current_database(), 'CREATE')`},
			{"TEMPORARY on the database", `SELECT has_database_privilege(current_user, current_database(), 'TEMPORARY')`},
		} {
			var has bool
			if err := conn.QueryRow(ctx, c.query).Scan(&has); err != nil {
				t.Fatalf("%s: %v", c.what, err)
			}
			if has {
				t.Errorf("role has %s", c.what)
			}
		}
	})

	t.Run("tables carry only the four data privileges", func(t *testing.T) {
		// TRUNCATE ignores row-level security entirely: it empties the whole
		// table, every organization's rows included. REFERENCES and TRIGGER are
		// DDL in everything but name.
		var wide []string
		const q = `SELECT t.tablename || ':' || p.privilege
		             FROM pg_tables t
		             CROSS JOIN (VALUES ('TRUNCATE'), ('REFERENCES'), ('TRIGGER')) AS p(privilege)
		            WHERE t.schemaname = 'public'
		              AND has_table_privilege(current_user, format('public.%I', t.tablename), p.privilege)
		            ORDER BY 1`
		rows, err := conn.Query(ctx, q)
		if err != nil {
			t.Fatalf("read table privileges: %v", err)
		}
		defer rows.Close()
		for rows.Next() {
			var finding string
			if err := rows.Scan(&finding); err != nil {
				t.Fatalf("scan: %v", err)
			}
			wide = append(wide, finding)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("read table privileges: %v", err)
		}
		if len(wide) != 0 {
			t.Errorf("privileges wider than SELECT/INSERT/UPDATE/DELETE: %v", wide)
		}
	})

	t.Run("sequences carry only USAGE and SELECT", func(t *testing.T) {
		var wide []string
		const q = `SELECT coalesce(array_agg(c.relname ORDER BY c.relname), '{}')
		             FROM pg_class c
		             JOIN pg_namespace n ON n.oid = c.relnamespace
		            WHERE n.nspname = 'public' AND c.relkind = 'S'
		              AND has_sequence_privilege(current_user, format('public.%I', c.relname), 'UPDATE')`
		if err := conn.QueryRow(ctx, q).Scan(&wide); err != nil {
			t.Fatalf("read sequence privileges: %v", err)
		}
		if len(wide) != 0 {
			t.Errorf("UPDATE on sequences %v", wide)
		}
	})

	t.Run("the journal is append-only", func(t *testing.T) {
		for _, privilege := range []string{"UPDATE", "DELETE"} {
			var has bool
			if err := conn.QueryRow(ctx,
				`SELECT has_table_privilege(current_user, 'public.audit_log', $1)`, privilege).Scan(&has); err != nil {
				t.Fatalf("audit_log %s: %v", privilege, err)
			}
			if has {
				t.Errorf("role has %s on audit_log", privilege)
			}
		}
	})

	t.Run("the migration bookkeeping is out of reach", func(t *testing.T) {
		var usage bool
		if err := conn.QueryRow(ctx,
			`SELECT has_schema_privilege(current_user, 'migrate', 'USAGE')`).Scan(&usage); err != nil {
			t.Fatalf("schema migrate: %v", err)
		}
		if usage {
			t.Error("role can use schema migrate")
		}
	})
}
