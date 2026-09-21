package postgres

import (
	"context"
	"errors"
	"fmt"

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
	pool, err := pgxpool.New(ctx, dsn)
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
