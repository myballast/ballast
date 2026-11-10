package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNoOrganization is returned when a tenant transaction is asked for without
// an organization. It is a caller bug, not a business rule: there is no such
// thing as a query that belongs to no tenant.
var ErrNoOrganization = errors.New("postgres: tenant transaction without an organization")

// setCurrentOrg installs the organization the row-level security policies read.
//
// It is not SET LOCAL. SET does not accept a bound parameter, so the only way
// to write it as SET LOCAL would be to concatenate the value into the
// statement, which is an injection. set_config takes the value as a parameter,
// and its third argument, true, is what makes the setting local to the
// transaction — it is gone when the transaction ends, so nothing leaks from one
// user of a pooled connection to the next.
const setCurrentOrg = `SELECT set_config('app.current_org', $1, true)`

// DB owns the connection pool and is the only way into PostgreSQL.
//
// The pool must be opened with the application role, which is neither a
// superuser nor BYPASSRLS and owns no table. Connected as anything else, every
// policy below is decoration.
type DB struct {
	pool *pgxpool.Pool
}

// Open builds a pool from dsn and checks that it answers.
func Open(ctx context.Context, dsn string) (*DB, error) {
	return OpenSized(ctx, dsn, 0)
}

// OpenSized is Open with a ceiling on the connections of the pool. Zero keeps the
// driver's own default. A worker that executes several runs at once holds several
// connections at once (each run's heartbeat and cancellation watch, on top of
// the one that claims), and the default is sized for neither.
func OpenSized(ctx context.Context, dsn string, maxConns int) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("read the postgres address: %w", err)
	}
	if maxConns > 0 {
		cfg.MaxConns = int32(min(maxConns, math.MaxInt32)) //nolint:gosec // clamped to the int32 range on this line
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return &DB{pool: pool}, nil
}

// NewDB wraps an already built pool.
func NewDB(pool *pgxpool.Pool) *DB {
	return &DB{pool: pool}
}

// Ping reports whether the database answers, which is what the API's readiness
// endpoint asks. It takes no tenant: it establishes that there is a database to
// talk to, not that anything in it can be read.
func (db *DB) Ping(ctx context.Context) error {
	return db.pool.Ping(ctx)
}

// Close releases the pool.
func (db *DB) Close() {
	if db.pool != nil {
		db.pool.Close()
	}
}

// InTenantTx runs fn inside a transaction scoped to orgID.
//
// Every read and every write goes through here. Row-level security filters on
// a setting that only this function installs, so a query run outside a tenant
// transaction does not return another tenant's rows — it returns nothing at
// all. That is the whole isolation story, and it holds only as long as this
// stays the single entry point.
//
// The transaction commits when fn returns nil and rolls back otherwise. fn's
// error is returned unchanged, so callers can match on their own sentinels.
func (db *DB) InTenantTx(ctx context.Context, orgID uuid.UUID, fn func(pgx.Tx) error) error {
	// Before anything is opened. A nil organization would otherwise reach the
	// policies as the nil UUID, which matches no row — a silent empty result
	// where a loud failure belongs.
	if orgID == uuid.Nil {
		return ErrNoOrganization
	}

	return pgx.BeginFunc(ctx, db.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, setCurrentOrg, orgID.String()); err != nil {
			return fmt.Errorf("set tenant context: %w", err)
		}
		return fn(tx)
	})
}

// lookupSubject is the only statement that runs outside InTenantTx.
const lookupSubject = `SELECT org_id, user_id FROM auth_lookup($1)`

// LookupSubject resolves a sign-in subject to the organizations and users it
// belongs to, oldest first.
//
// It is the one exception to InTenantTx, and the reason is structural: at
// sign-in the organization is what is being looked for, so there is none to
// scope a transaction to. It does not loosen row-level security. It calls
// auth_lookup, the function migration 003 grants for exactly this and nothing
// else, which answers with two identifiers per match. Any other query belongs
// in InTenantTx.
func (db *DB) LookupSubject(ctx context.Context, subject string) ([]Match, error) {
	rows, err := db.pool.Query(ctx, lookupSubject, subject)
	if err != nil {
		return nil, fmt.Errorf("look the subject up: %w", err)
	}

	matches, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Match])
	if err != nil {
		return nil, fmt.Errorf("read the subject's matches: %w", err)
	}
	return matches, nil
}

// Match is one organization and user a subject resolves to.
type Match struct {
	OrgID  uuid.UUID
	UserID uuid.UUID
}
