package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// OrganizationDirectory lists organizations to a worker that serves several.
//
// Like LookupSubject, it runs outside InTenantTx, and for the same structural
// reason: which organizations to serve is the thing being asked, so there is none
// to scope a transaction to. It does not loosen row-level security. It calls two
// functions that migration 020 grants for exactly this, each of which answers with
// a column of identifiers and nothing else. Any other query belongs in
// InTenantTx.
type OrganizationDirectory struct {
	db *DB
}

// NewOrganizationDirectory returns a directory over db.
func NewOrganizationDirectory(db *DB) *OrganizationDirectory {
	return &OrganizationDirectory{db: db}
}

// WithWork returns the organizations with a run that is queued, running or
// orphaned.
func (d *OrganizationDirectory) WithWork(ctx context.Context) ([]uuid.UUID, error) {
	return d.list(ctx, `SELECT org_id FROM organizations_with_work()`, "with work")
}

// All returns every organization.
func (d *OrganizationDirectory) All(ctx context.Context) ([]uuid.UUID, error) {
	return d.list(ctx, `SELECT org_id FROM organization_ids()`, "that exist")
}

func (d *OrganizationDirectory) list(ctx context.Context, query, what string) ([]uuid.UUID, error) {
	rows, err := d.db.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list the organizations %s: %w", what, err)
	}

	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("read the organizations %s: %w", what, err)
	}
	return ids, nil
}
