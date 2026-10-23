//go:build integration

package migrate_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/migrate"
	"github.com/youssefmahersi/ballast/migrations"
)

// The owner role of the development database. Creating a database needs it; the
// application role cannot, which is the point of that role.
const defaultOwnerURL = "postgres://ballast:ballast@127.0.0.1:5432/ballast?sslmode=disable"

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// freshDatabase creates an empty database of its own and returns the URL of the
// owner role on it. Never the development database: these tests apply and
// break migrations, and a dirty or half-migrated development database is hours
// of someone's data.
func freshDatabase(t *testing.T) (string, *pgx.Conn) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)

	ownerURL := os.Getenv("MIGRATE_DATABASE_URL")
	if ownerURL == "" {
		ownerURL = defaultOwnerURL
	}

	admin, err := pgx.Connect(ctx, ownerURL)
	if err != nil {
		t.Fatalf("connect as the owner: %v", err)
	}

	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	name := "ballast_migrate_test_" + hex.EncodeToString(raw)

	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}

	u, err := url.Parse(ownerURL)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	target := u.String()

	conn, err := pgx.Connect(ctx, target)
	if err != nil {
		t.Fatalf("connect to %s: %v", name, err)
	}

	t.Cleanup(func() {
		cleanup := context.WithoutCancel(ctx)
		_ = conn.Close(cleanup)
		_, _ = admin.Exec(cleanup, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
		_ = admin.Close(cleanup)
	})

	return target, conn
}

// latest is the highest version the embedded files carry.
func latest(t *testing.T, files fs.FS) int {
	t.Helper()

	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		t.Fatal(err)
	}
	highest := 0
	for _, e := range entries {
		prefix, _, _ := strings.Cut(e.Name(), "_")
		if n, err := strconv.Atoi(prefix); err == nil && n > highest {
			highest = n
		}
	}
	return highest
}

// withMigration is the real migrations plus one more, as a file set of its own.
func withMigration(t *testing.T, name, up string) fstest.MapFS {
	t.Helper()

	files := fstest.MapFS{}
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		body, err := fs.ReadFile(migrations.FS, e.Name())
		if err != nil {
			t.Fatal(err)
		}
		files[e.Name()] = &fstest.MapFile{Data: body}
	}

	next := latest(t, migrations.FS) + 1
	files[fmt.Sprintf("%03d_%s.up.sql", next, name)] = &fstest.MapFile{Data: []byte(up)}
	files[fmt.Sprintf("%03d_%s.down.sql", next, name)] = &fstest.MapFile{Data: []byte("DROP TABLE IF EXISTS half_done;")}
	return files
}

func applied(t *testing.T, conn *pgx.Conn) (version int, dirty bool, found bool) {
	t.Helper()

	err := conn.QueryRow(context.Background(),
		`SELECT version, dirty FROM migrate.schema_migrations`).Scan(&version, &dirty)
	if err == pgx.ErrNoRows {
		return 0, false, false
	}
	if err != nil {
		t.Fatalf("read the applied version: %v", err)
	}
	return version, dirty, true
}

func exists(t *testing.T, conn *pgx.Conn, table string) bool {
	t.Helper()

	var found bool
	if err := conn.QueryRow(context.Background(),
		`SELECT to_regclass($1) IS NOT NULL`, table).Scan(&found); err != nil {
		t.Fatalf("look for %s: %v", table, err)
	}
	return found
}

const brokenHalfWay = `BEGIN;
CREATE TABLE half_done (id int);
INSERT INTO a_table_that_does_not_exist VALUES (1);
COMMIT;
`

const fixed = `BEGIN;
CREATE TABLE half_done (id int);
COMMIT;
`

func TestAnEmptyDatabaseGetsTheWholeSchema(t *testing.T) {
	target, conn := freshDatabase(t)

	if err := migrate.Up(context.Background(), target, migrations.FS, quiet()); err != nil {
		t.Fatalf("Up on an empty database: %v", err)
	}

	version, dirty, found := applied(t, conn)
	if !found || dirty || version != latest(t, migrations.FS) {
		t.Errorf("applied version = %d (dirty %v, recorded %v), want %d clean",
			version, dirty, found, latest(t, migrations.FS))
	}

	// What the isolation rests on: every table of the schema is under
	// row-level security, forced, on a database built by this path.
	rows, err := conn.Query(context.Background(), `
		SELECT c.relname, c.relrowsecurity, c.relforcerowsecurity
		  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = 'public' AND c.relkind = 'r'
		 ORDER BY c.relname`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	tables := 0
	for rows.Next() {
		var name string
		var enabled, forced bool
		if err := rows.Scan(&name, &enabled, &forced); err != nil {
			t.Fatal(err)
		}
		tables++
		if !enabled || !forced {
			t.Errorf("table %s: row-level security enabled=%v forced=%v", name, enabled, forced)
		}
	}
	if tables == 0 {
		t.Fatal("the schema has no table")
	}

	// The bookkeeping is out of public, where only tenant tables may be.
	if exists(t, conn, "public.schema_migrations") {
		t.Error("the bookkeeping table is in the public schema")
	}
}

func TestRunningAgainOnAnUpToDateDatabaseChangesNothing(t *testing.T) {
	target, conn := freshDatabase(t)

	if err := migrate.Up(context.Background(), target, migrations.FS, quiet()); err != nil {
		t.Fatalf("first Up: %v", err)
	}

	count := func() (n int) {
		if err := conn.QueryRow(context.Background(),
			`SELECT count(*) FROM information_schema.columns WHERE table_schema = 'public'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := count()

	if err := migrate.Up(context.Background(), target, migrations.FS, quiet()); err != nil {
		t.Fatalf("second Up on an up-to-date database: %v", err)
	}

	version, dirty, _ := applied(t, conn)
	if version != latest(t, migrations.FS) || dirty {
		t.Errorf("version moved to %d (dirty %v)", version, dirty)
	}
	if after := count(); after != before {
		t.Errorf("the schema changed: %d columns, then %d", before, after)
	}
}

func TestABrokenMigrationLeavesTheSchemaAsItWasAndTheFixCanFollow(t *testing.T) {
	target, conn := freshDatabase(t)
	ctx := context.Background()

	if err := migrate.Up(ctx, target, migrations.FS, quiet()); err != nil {
		t.Fatalf("Up: %v", err)
	}
	good := latest(t, migrations.FS)

	// The deploy that carries a broken migration stops, and says so.
	if err := migrate.Up(ctx, target, withMigration(t, "broken", brokenHalfWay), quiet()); err == nil {
		t.Fatal("a broken migration was reported as applied")
	}

	version, dirty, _ := applied(t, conn)
	if version != good || dirty {
		t.Errorf("after the failure the version is %d (dirty %v), want %d clean", version, dirty, good)
	}
	// Atomic: the statement before the broken one is undone with it.
	if exists(t, conn, "public.half_done") {
		t.Error("half of the failed migration was left behind")
	}

	// The next deploy, with the file corrected, does not stop at "dirty".
	if err := migrate.Up(ctx, target, withMigration(t, "broken", fixed), quiet()); err != nil {
		t.Fatalf("Up with the migration fixed: %v", err)
	}
	if version, dirty, _ := applied(t, conn); version != good+1 || dirty {
		t.Errorf("after the fix the version is %d (dirty %v), want %d clean", version, dirty, good+1)
	}
	if !exists(t, conn, "public.half_done") {
		t.Error("the fixed migration did not run")
	}
}

func TestAFirstMigrationThatFailsLeavesNoVersionAtAll(t *testing.T) {
	target, conn := freshDatabase(t)
	ctx := context.Background()

	broken := fstest.MapFS{
		"001_broken.up.sql":   {Data: []byte(brokenHalfWay)},
		"001_broken.down.sql": {Data: []byte("DROP TABLE IF EXISTS half_done;")},
	}
	if err := migrate.Up(ctx, target, broken, quiet()); err == nil {
		t.Fatal("a broken migration was reported as applied")
	}

	if version, dirty, found := applied(t, conn); found {
		t.Errorf("a version was left recorded: %d (dirty %v)", version, dirty)
	}

	fixedFirst := fstest.MapFS{
		"001_broken.up.sql":   {Data: []byte(fixed)},
		"001_broken.down.sql": {Data: []byte("DROP TABLE IF EXISTS half_done;")},
	}
	if err := migrate.Up(ctx, target, fixedFirst, quiet()); err != nil {
		t.Fatalf("Up with the first migration fixed: %v", err)
	}
}

func TestADirtyDatabaseIsRefusedUntouched(t *testing.T) {
	target, conn := freshDatabase(t)
	ctx := context.Background()

	if err := migrate.Up(ctx, target, migrations.FS, quiet()); err != nil {
		t.Fatalf("Up: %v", err)
	}
	// What a process killed in the middle of a migration leaves.
	if _, err := conn.Exec(ctx, `UPDATE migrate.schema_migrations SET dirty = true`); err != nil {
		t.Fatal(err)
	}

	err := migrate.Up(ctx, target, withMigration(t, "after_dirty", fixed), quiet())
	if err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("a dirty database was not refused: %v", err)
	}
	if exists(t, conn, "public.half_done") {
		t.Error("a migration ran on a dirty database")
	}
	// Still dirty: only someone looking at it clears that.
	if _, dirty, _ := applied(t, conn); !dirty {
		t.Error("the dirty mark was cleared by a run that did nothing")
	}
}
