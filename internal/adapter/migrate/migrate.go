// Package migrate applies the schema migrations to a database.
//
// It exists so that a deployment can run them: the API image is a single static
// binary with no shell and no migration tool, and the migrations are read from
// the files embedded in cmd/migrate rather than from a checkout.
//
// It connects as the owner role, never as the application role, which has no
// right to change a table or a policy.
package migrate

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"net/url"
	"os"
	"strconv"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // registers the pgx5 driver
	"github.com/golang-migrate/migrate/v4/source"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
)

// bookkeepingTable is where golang-migrate records the version. It is quoted,
// and kept out of the public schema: the application role cannot see it, and
// the schema tests allow only tenant-scoped tables in public.
const bookkeepingTable = `"migrate"."schema_migrations"`

// Up applies every migration in source that the database has not seen yet.
//
// Running it on a database that is up to date changes nothing. A migration that
// fails leaves the schema as it was before that migration, and the database at
// the last version that did apply, so the next run starts from there.
func Up(ctx context.Context, rawURL string, files fs.FS, log *slog.Logger) error {
	target, err := databaseURL(rawURL)
	if err != nil {
		return err
	}
	if err := prepare(ctx, rawURL); err != nil {
		return err
	}

	m, _, err := open(target, files)
	if err != nil {
		return err
	}

	before, err := version(m)
	if err != nil {
		_ = closeAll(m)
		return err
	}
	if before.dirty {
		_ = closeAll(m)
		return fmt.Errorf("the database is marked dirty at version %s: a migration was interrupted, "+
			"and someone has to look at what it left before anything else runs", before)
	}

	log.Info("applying migrations", "from_version", before.String())

	err = m.Up()
	switch {
	case err == nil:
		after, _ := version(m)
		_ = closeAll(m)
		log.Info("migrations applied", "from_version", before.String(), "to_version", after.String())
		return nil

	case errors.Is(err, migrate.ErrNoChange):
		_ = closeAll(m)
		log.Info("the database is up to date", "version", before.String())
		return nil
	}

	// A migration failed. The connection it ran on is in an aborted
	// transaction, so look from a new one.
	_ = closeAll(m)

	if resetErr := resetFailedVersion(target, files); resetErr != nil {
		return fmt.Errorf("migration failed: %w; and the version could not be reset, "+
			"it has to be forced by hand: %v", err, resetErr)
	}

	return fmt.Errorf("migration failed, the schema is back at version %s: %w", before, err)
}

// resetFailedVersion takes the database off the version golang-migrate marked
// dirty before running the migration that failed.
//
// Left alone, the next deployment — the one that carries the fix — would stop
// at "dirty" before reading it. That is only right when nothing of the failed
// migration remains, and it holds here because every migration is one
// transaction (see docs/migrations.md): a statement that fails undoes the whole
// file. A migration that commits half of itself would make this wrong, which is
// why the rule is written down.
func resetFailedVersion(target string, files fs.FS) error {
	m, src, err := open(target, files)
	if err != nil {
		return err
	}
	defer func() { _ = closeAll(m) }()

	current, err := version(m)
	if err != nil {
		return err
	}
	if !current.dirty {
		return nil
	}

	// The migration that was running is `current`; what is applied is whatever
	// precedes it in the source, which is the version the run started from when
	// one migration was pending, and the one before the failing file when
	// several were.
	previous := database.NilVersion
	switch p, err := src.Prev(current.number); {
	case err == nil:
		if p > math.MaxInt32 {
			return fmt.Errorf("version %d is out of range", p)
		}
		previous = int(p)
	case !errors.Is(err, os.ErrNotExist):
		return err
	}

	return m.Force(previous)
}

// databaseURL turns the URL the platform hands out into the one golang-migrate's
// driver reads: it names the driver, and puts the bookkeeping table in its own
// schema. Whatever else the URL carries (sslmode, for one) is kept.
//
// The error never repeats the input. It carries the owner's password, and it
// ends up in a deployment log.
func databaseURL(raw string) (string, error) {
	invalid := errors.New("MIGRATE_DATABASE_URL is not a postgres:// URL")

	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" {
		return "", invalid
	}

	u.Scheme = "pgx5"
	q := u.Query()
	if q.Get("x-migrations-table") == "" {
		q.Set("x-migrations-table", bookkeepingTable)
		q.Set("x-migrations-table-quoted", "1")
	}
	u.RawQuery = q.Encode()

	return u.String(), nil
}

// applicationRole is the role the migrations grant to. Migrations 003, 009 and
// 013 name it, so it has to exist before the first one runs: roles belong to the
// server, not to a database, and no migration should be the one to create it.
const applicationRole = "ballast_app"

// prepare checks what the migrations take for granted, and creates the schema
// that holds their bookkeeping.
//
// The schema is normally there already, made by ops/sql/00-app-role.sql. A
// database that has never seen that script still gets its schema, but not its
// role, and the error says which script to run: a migration that fails on a
// missing role names a GRANT in the middle of a file, which sends people
// looking in the wrong place.
func prepare(ctx context.Context, rawURL string) error {
	conn, err := pgx.Connect(ctx, rawURL)
	if err != nil {
		// pgx leaves the password out of its own errors.
		return fmt.Errorf("connect to the database: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	var present bool
	if err := conn.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)`, applicationRole).Scan(&present); err != nil {
		return fmt.Errorf("look for the %s role: %w", applicationRole, err)
	}
	if !present {
		return fmt.Errorf("the %s role does not exist: run ops/sql/00-app-role.sql as the owner "+
			"before the first migration", applicationRole)
	}

	if _, err := conn.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS migrate`); err != nil {
		return fmt.Errorf("create the schema that holds the migration bookkeeping: %w", err)
	}
	return nil
}

func open(target string, files fs.FS) (*migrate.Migrate, source.Driver, error) {
	src, err := iofs.New(files, ".")
	if err != nil {
		return nil, nil, fmt.Errorf("read the migrations: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", src, target)
	if err != nil {
		_ = src.Close()
		return nil, nil, fmt.Errorf("open the database for migration: %w", redact(err))
	}
	return m, src, nil
}

// applied is what the database says it is at.
type applied struct {
	number uint
	// none is a database that has never had a migration.
	none  bool
	dirty bool
}

func (a applied) String() string {
	if a.none {
		return "none"
	}
	return strconv.FormatUint(uint64(a.number), 10)
}

func version(m *migrate.Migrate) (applied, error) {
	v, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return applied{none: true}, nil
	}
	if err != nil {
		return applied{}, fmt.Errorf("read the applied version: %w", err)
	}
	return applied{number: v, dirty: dirty}, nil
}

func closeAll(m *migrate.Migrate) error {
	srcErr, dbErr := m.Close()
	return errors.Join(srcErr, dbErr)
}

// redact keeps a connection error from carrying the URL it was opened with.
func redact(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return fmt.Errorf("%s: %w", urlErr.Op, urlErr.Err)
	}
	return err
}
