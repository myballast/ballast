package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres/sqlc"
	"github.com/youssefmahersi/ballast/internal/domain"
)

// defaultAuditPage is how many entries a reading returns when the caller asks
// for no particular number, and maxAuditPage the most it may ask for. A journal
// only grows, so no reading is ever unbounded.
const (
	defaultAuditPage = 100
	maxAuditPage     = 500
)

// AuditRepository writes and reads the journal.
type AuditRepository struct {
	db *DB
}

// NewAuditRepository returns a repository over db.
func NewAuditRepository(db *DB) *AuditRepository {
	return &AuditRepository{db: db}
}

// Record writes one entry, in a transaction of its own.
//
// Used by the actions whose record is written just after they happened. The
// ones where a missing entry would be a hole rather than a gap — approving a
// target, changing somebody's role — hand their entry to the repository doing
// the work instead, which writes it in the same transaction through
// insertAudit below.
func (r *AuditRepository) Record(ctx context.Context, entry domain.AuditEntry) error {
	return r.db.InTenantTx(ctx, entry.OrgID, func(tx pgx.Tx) error {
		return insertAudit(ctx, tx, entry)
	})
}

// InProject reads one project's journal, newest first, including the entries
// that belong to the organization rather than to any project.
func (r *AuditRepository) InProject(
	ctx context.Context, orgID, projectID uuid.UUID, limit int,
) ([]domain.AuditEntry, error) {
	if limit <= 0 || limit > maxAuditPage {
		limit = defaultAuditPage
	}

	var entries []domain.AuditEntry

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		rows, err := sqlc.New(tx).ListProjectAudit(ctx, sqlc.ListProjectAuditParams{
			OrgID:     orgID,
			ProjectID: &projectID,
			Limit:     int32(limit), //nolint:gosec // bounded just above
		})
		if err != nil {
			return fmt.Errorf("read the journal: %w", err)
		}

		entries = make([]domain.AuditEntry, 0, len(rows))
		for _, row := range rows {
			entry, err := toDomainEntry(row)
			if err != nil {
				return err
			}
			entries = append(entries, entry)
		}
		return nil
	})

	return entries, err
}

// insertAudit writes one entry on the transaction it is given.
//
// Package-level rather than a method, because the repositories that have to
// write an entry inside their own work — approving a target, changing a role —
// are other types in this package. The alternative would be handing them an
// audit repository, which is an adapter importing another adapter.
func insertAudit(ctx context.Context, tx pgx.Tx, entry domain.AuditEntry) error {
	details, err := json.Marshal(detailsOrEmpty(entry.Details))
	if err != nil {
		return fmt.Errorf("render the entry's details: %w", err)
	}

	if _, err := sqlc.New(tx).RecordAudit(ctx, sqlc.RecordAuditParams{
		OrgID:      entry.OrgID,
		ActorID:    entry.ActorID,
		ProjectID:  entry.ProjectID,
		Action:     string(entry.Action),
		Outcome:    string(entry.Outcome),
		TargetType: entry.TargetType,
		TargetID:   entry.TargetID,
		Metadata:   details,
		CreatedAt:  entry.At,
	}); err != nil {
		return fmt.Errorf("record the audit entry: %w", err)
	}

	return nil
}

// detailsOrEmpty keeps a null out of the column: the schema's default is an
// empty object, and a reader should not have to tell "no details" from "no
// details, written differently".
func detailsOrEmpty(details map[string]string) map[string]string {
	if details == nil {
		return map[string]string{}
	}
	return details
}

func toDomainEntry(row sqlc.AuditLog) (domain.AuditEntry, error) {
	details := map[string]string{}
	if len(row.Metadata) > 0 {
		if err := json.Unmarshal(row.Metadata, &details); err != nil {
			return domain.AuditEntry{}, fmt.Errorf("read the entry's details: %w", err)
		}
	}

	return domain.AuditEntry{
		OrgID:      row.OrgID,
		ActorID:    row.ActorID,
		ProjectID:  row.ProjectID,
		Action:     domain.AuditAction(row.Action),
		Outcome:    domain.AuditOutcome(row.Outcome),
		TargetType: row.TargetType,
		TargetID:   row.TargetID,
		Details:    details,
		At:         row.CreatedAt,
	}, nil
}
