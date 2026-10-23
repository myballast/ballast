//go:build integration

package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Matches the application role created by ops/sql/00-app-role.sql.
const defaultDSN = "postgres://ballast_app:ballast_app@127.0.0.1:5432/ballast?sslmode=disable"

func dsn() string {
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return defaultDSN
}

func connect(t *testing.T) (context.Context, *pgx.Conn) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	conn, err := pgx.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("connect to %q: %v", dsn(), err)
	}
	t.Cleanup(func() {
		if err := conn.Close(context.Background()); err != nil {
			t.Errorf("close connection: %v", err)
		}
	})

	return ctx, conn
}

func TestDatabaseIsReachable(t *testing.T) {
	ctx, conn := connect(t)

	var got int
	if err := conn.QueryRow(ctx, "SELECT 1").Scan(&got); err != nil {
		t.Fatalf("SELECT 1: %v", err)
	}
	if got != 1 {
		t.Fatalf("SELECT 1 returned %d, want 1", got)
	}
}

// Row-Level Security is the whole tenant isolation story, and both a superuser
// and a BYPASSRLS role walk straight through it. If the integration suite ever
// runs with such a role, every isolation test that follows passes without
// proving anything. This guards that precondition rather than assuming it.
func TestApplicationRoleCannotBypassRLS(t *testing.T) {
	ctx, conn := connect(t)

	var isSuperuser string
	if err := conn.QueryRow(ctx, "SELECT current_setting('is_superuser')").Scan(&isSuperuser); err != nil {
		t.Fatalf("read is_superuser: %v", err)
	}
	if isSuperuser != "off" {
		t.Errorf("connected as a superuser (is_superuser=%q); RLS would be bypassed", isSuperuser)
	}

	var user string
	var bypassRLS bool
	const q = "SELECT rolname, rolbypassrls FROM pg_roles WHERE rolname = current_user"
	if err := conn.QueryRow(ctx, q).Scan(&user, &bypassRLS); err != nil {
		t.Fatalf("read current role attributes: %v", err)
	}
	if bypassRLS {
		t.Errorf("role %q has BYPASSRLS; RLS would be bypassed", user)
	}
}
