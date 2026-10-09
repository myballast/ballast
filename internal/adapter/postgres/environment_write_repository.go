package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres/sqlc"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/environment"
)

// Compile-time proof that this repository satisfies the port the environment
// service declared.
var _ environment.Repository = (*EnvironmentWriteRepository)(nil)

// EnvironmentWriteRepository persists target environments.
//
// It is separate from EnvironmentRepository, which the run service uses to read
// the target while executing a run: that one answers a single question and has no
// business being able to write.
type EnvironmentWriteRepository struct {
	db *DB
}

// NewEnvironmentWriteRepository returns a repository over db.
func NewEnvironmentWriteRepository(db *DB) *EnvironmentWriteRepository {
	return &EnvironmentWriteRepository{db: db}
}

func toDomainEnvironment(row sqlc.Environment) domain.Environment {
	found := domain.Environment{
		ID:                 row.ID,
		ProjectID:          row.ProjectID,
		Name:               row.Name,
		BaseURL:            row.BaseUrl,
		VerificationStatus: domain.VerificationStatus(row.VerificationStatus),
		VerificationToken:  row.VerificationToken,
		VerifiedAt:         row.VerifiedAt,
		VerifiedBy:         row.VerifiedBy,
		MaxVUs:             int(row.MaxVus),
		MaxDurationS:       int(row.MaxDurationS),
		CreatedAt:          row.CreatedAt,
	}

	// Migration 001 holds the two bounds to each other, so either both are there
	// or there is no window.
	if row.AllowedWindowStartMinute != nil && row.AllowedWindowEndMinute != nil {
		found.Window = &domain.AllowedWindow{
			StartMinute: int(*row.AllowedWindowStartMinute),
			EndMinute:   int(*row.AllowedWindowEndMinute),
			Zone:        row.AllowedWindowTz,
		}
	}

	return found
}

// windowColumns renders a window as the three columns that hold it. No window is
// two nulls and the zone left as it was stored, since the column is not nullable
// and a zone without bounds means nothing.
func windowColumns(window *domain.AllowedWindow) (start, end *int16, zone string) {
	if window == nil {
		return nil, nil, "UTC"
	}

	//nolint:gosec // minutes of a day, bounded by AllowedWindow.Validate and by the schema
	from, to := int16(window.StartMinute), int16(window.EndMinute)
	return &from, &to, window.Zone
}

// Create writes an environment, pending verification.
func (r *EnvironmentWriteRepository) Create(
	ctx context.Context, orgID uuid.UUID, newEnvironment domain.Environment, token string, at time.Time,
) (domain.Environment, error) {
	start, end, zone := windowColumns(newEnvironment.Window)

	var created domain.Environment
	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		row, err := sqlc.New(tx).InsertEnvironment(ctx, sqlc.InsertEnvironmentParams{
			ID:                       newEnvironment.ID,
			OrgID:                    orgID,
			ProjectID:                newEnvironment.ProjectID,
			Name:                     newEnvironment.Name,
			BaseUrl:                  newEnvironment.BaseURL,
			VerificationToken:        token,
			MaxVus:                   int32(newEnvironment.MaxVUs),       //nolint:gosec // bounded by ValidateEnvironment
			MaxDurationS:             int32(newEnvironment.MaxDurationS), //nolint:gosec // bounded by ValidateEnvironment
			AllowedWindowStartMinute: start,
			AllowedWindowEndMinute:   end,
			AllowedWindowTz:          zone,
			CreatedAt:                at,
		})
		if err != nil {
			return writeError(err, "write the environment")
		}

		created = toDomainEnvironment(row)
		return nil
	})

	return created, err
}

// ByID reads one environment.
func (r *EnvironmentWriteRepository) ByID(
	ctx context.Context, orgID, id uuid.UUID,
) (domain.Environment, error) {
	var found domain.Environment

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		row, err := sqlc.New(tx).GetEnvironment(ctx, sqlc.GetEnvironmentParams{OrgID: orgID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("read the environment: %w", err)
		}

		found = toDomainEnvironment(row)
		return nil
	})

	return found, err
}

// InProject lists a project's environments.
func (r *EnvironmentWriteRepository) InProject(
	ctx context.Context, orgID, projectID uuid.UUID,
) ([]domain.Environment, error) {
	var found []domain.Environment

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		rows, err := sqlc.New(tx).ListEnvironmentsInProject(ctx,
			sqlc.ListEnvironmentsInProjectParams{OrgID: orgID, ProjectID: projectID})
		if err != nil {
			return fmt.Errorf("list the environments: %w", err)
		}

		found = make([]domain.Environment, 0, len(rows))
		for _, row := range rows {
			found = append(found, toDomainEnvironment(row))
		}
		return nil
	})

	return found, err
}

// Save writes the mutable half of an environment.
//
// Two statements rather than one with a flag: the one that clears the
// verification says so in SQL, which is what makes it readable in a query log and
// impossible to reach by passing the wrong boolean.
func (r *EnvironmentWriteRepository) Save(
	ctx context.Context, orgID uuid.UUID, updated domain.Environment, resetVerification bool,
) (domain.Environment, error) {
	start, end, zone := windowColumns(updated.Window)

	var saved domain.Environment
	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		queries := sqlc.New(tx)

		var (
			row sqlc.Environment
			err error
		)
		if resetVerification {
			row, err = queries.UpdateEnvironmentAndResetVerification(ctx,
				sqlc.UpdateEnvironmentAndResetVerificationParams{
					OrgID:                    orgID,
					ID:                       updated.ID,
					Name:                     updated.Name,
					BaseUrl:                  updated.BaseURL,
					MaxVus:                   int32(updated.MaxVUs),       //nolint:gosec // bounded by ValidateEnvironment       //nolint:gosec // bounded by ValidateEnvironment
					MaxDurationS:             int32(updated.MaxDurationS), //nolint:gosec // bounded by ValidateEnvironment
					AllowedWindowStartMinute: start,
					AllowedWindowEndMinute:   end,
					AllowedWindowTz:          zone,
				})
		} else {
			row, err = queries.UpdateEnvironment(ctx, sqlc.UpdateEnvironmentParams{
				OrgID:                    orgID,
				ID:                       updated.ID,
				Name:                     updated.Name,
				BaseUrl:                  updated.BaseURL,
				MaxVus:                   int32(updated.MaxVUs),       //nolint:gosec // bounded by ValidateEnvironment       //nolint:gosec // bounded by ValidateEnvironment
				MaxDurationS:             int32(updated.MaxDurationS), //nolint:gosec // bounded by ValidateEnvironment
				AllowedWindowStartMinute: start,
				AllowedWindowEndMinute:   end,
				AllowedWindowTz:          zone,
			})
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return writeError(err, "update the environment")
		}

		saved = toDomainEnvironment(row)
		return nil
	})

	return saved, err
}

// Delete removes an environment that holds no run.
//
// The count and the delete share one transaction on purpose. Asking first and
// deleting afterwards leaves room for a run to be queued in between, and the
// foreign key cascades: the history of everything ever measured against this
// target would go with it.
func (r *EnvironmentWriteRepository) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	return r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		queries := sqlc.New(tx)

		runs, err := queries.CountEnvironmentRuns(ctx,
			sqlc.CountEnvironmentRunsParams{OrgID: orgID, EnvironmentID: id})
		if err != nil {
			return fmt.Errorf("count the environment's runs: %w", err)
		}
		if runs > 0 {
			return fmt.Errorf("%w: %d of them", domain.ErrEnvironmentNotEmpty, runs)
		}

		affected, err := queries.DeleteEnvironment(ctx,
			sqlc.DeleteEnvironmentParams{OrgID: orgID, ID: id})
		if err != nil {
			return fmt.Errorf("delete the environment: %w", err)
		}
		if affected == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
}

// writeError turns a broken unique constraint into something a handler can map to
// a status code, and leaves everything else as it is.
func writeError(err error, what string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return domain.ErrAlreadyExists
	}
	return fmt.Errorf("%s: %w", what, err)
}

// RecordProofOK records that the target served its challenge.
//
// It leaves the environment in proof_ok, which is not a state load may be sent
// in. The statement refuses an already approved target, so this cannot be used
// to take an approval away.
func (r *EnvironmentWriteRepository) RecordProofOK(
	ctx context.Context, orgID, id uuid.UUID,
) (domain.Environment, error) {
	return r.recordProof(ctx, orgID, func(queries *sqlc.Queries) (sqlc.Environment, error) {
		return queries.MarkEnvironmentProven(ctx,
			sqlc.MarkEnvironmentProvenParams{OrgID: orgID, ID: id})
	})
}

// RecordProofFailed records an attempt that proved nothing.
func (r *EnvironmentWriteRepository) RecordProofFailed(
	ctx context.Context, orgID, id uuid.UUID,
) (domain.Environment, error) {
	return r.recordProof(ctx, orgID, func(queries *sqlc.Queries) (sqlc.Environment, error) {
		return queries.MarkEnvironmentProofFailed(ctx,
			sqlc.MarkEnvironmentProofFailedParams{OrgID: orgID, ID: id})
	})
}

func (r *EnvironmentWriteRepository) recordProof(
	ctx context.Context, orgID uuid.UUID,
	write func(*sqlc.Queries) (sqlc.Environment, error),
) (domain.Environment, error) {
	var recorded domain.Environment

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		row, err := write(sqlc.New(tx))
		if errors.Is(err, pgx.ErrNoRows) {
			// Either there is no such environment, or it is already approved.
			// The service checks the second before coming here, so by the time
			// this is reached the two are the same answer.
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("record the verification attempt: %w", err)
		}

		recorded = toDomainEnvironment(row)
		return nil
	})

	return recorded, err
}

// Approve records the decision that lets load reach a target, and allows its
// host for the project.
//
// Both in one transaction, and in this order. A target approved while the
// allowlist that bounds it stays empty would be a run refused for a reason
// nobody can see; an allowlist entry with no approval behind it would be an
// egress permission nobody granted.
func (r *EnvironmentWriteRepository) Approve(
	ctx context.Context, orgID, id uuid.UUID, by domain.Identity, at time.Time, host string,
) (domain.Environment, error) {
	var approved domain.Environment

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		queries := sqlc.New(tx)

		row, err := queries.ApproveEnvironment(ctx, sqlc.ApproveEnvironmentParams{
			OrgID:      orgID,
			ID:         id,
			VerifiedBy: &by.UserID,
			VerifiedAt: &at,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			// The statement requires proof_ok, so a row that exists and did not
			// match had no proof in. Said as such rather than as absent: the
			// caller asked about something they can see.
			return fmt.Errorf("%w: nothing was approved", domain.ErrNotProven)
		}
		if err != nil {
			return fmt.Errorf("approve the environment: %w", err)
		}

		// From the row rather than from an argument: the project is whatever the
		// environment that was just approved belongs to.
		if err := queries.AllowHostForProject(ctx, sqlc.AllowHostForProjectParams{
			OrgID:     orgID,
			ProjectID: row.ProjectID,
			Host:      host,
		}); err != nil {
			return fmt.Errorf("allow the host for the project: %w", err)
		}

		approved = toDomainEnvironment(row)
		return nil
	})

	return approved, err
}
