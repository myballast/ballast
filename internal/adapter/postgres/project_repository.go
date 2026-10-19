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
	"github.com/youssefmahersi/ballast/internal/usecase/project"
)

// Compile-time proof that this repository satisfies the ports the project
// service declared.
var _ project.Repository = (*ProjectRepository)(nil)

// uniqueViolation is PostgreSQL's code for a broken unique constraint, which is
// how a slug already in use arrives here.
const uniqueViolation = "23505"

// ProjectRepository persists projects and who belongs to them.
type ProjectRepository struct {
	db *DB
}

// NewProjectRepository returns a repository over db.
func NewProjectRepository(db *DB) *ProjectRepository {
	return &ProjectRepository{db: db}
}

func toDomainProject(row sqlc.Project) domain.Project {
	return domain.Project{
		ID:    row.ID,
		OrgID: row.OrgID,
		Name:  row.Name,
		Slug:  row.Slug,
		Details: domain.ProjectDetails{
			Infra: row.Infra, Region: row.Region, Owner: row.Owner, Stack: row.Stack,
		},
		CreatedAt: row.CreatedAt,
	}
}

func toDomainMembership(row sqlc.Membership) domain.Membership {
	return domain.Membership{
		OrgID:     row.OrgID,
		UserID:    row.UserID,
		ProjectID: row.ProjectID,
		Role:      domain.Role(row.Role),
		Override:  row.Permissions,
		CreatedAt: row.CreatedAt,
	}
}

// Create writes a project and the membership that makes its creator an
// administrator, in one transaction. A project nobody can administer would have
// to be repaired by hand.
func (r *ProjectRepository) Create(
	ctx context.Context, newProject domain.Project, owner domain.Identity, at time.Time,
) (domain.Project, error) {
	var created domain.Project

	err := r.db.InTenantTx(ctx, newProject.OrgID, func(tx pgx.Tx) error {
		queries := sqlc.New(tx)

		row, err := queries.InsertProject(ctx, sqlc.InsertProjectParams{
			ID:        newProject.ID,
			OrgID:     newProject.OrgID,
			Name:      newProject.Name,
			Slug:      newProject.Slug,
			Infra:     newProject.Details.Infra,
			Region:    newProject.Details.Region,
			Owner:     newProject.Details.Owner,
			Stack:     stackOrEmpty(newProject.Details.Stack),
			CreatedAt: at,
		})
		if err != nil {
			return fmt.Errorf("insert the project: %w", translate(err))
		}

		if err := queries.UpsertMembership(ctx, sqlc.UpsertMembershipParams{
			OrgID:     newProject.OrgID,
			UserID:    owner.UserID,
			ProjectID: row.ID,
			Role:      string(domain.RoleAdmin),
		}); err != nil {
			return fmt.Errorf("make the creator an administrator: %w", err)
		}

		created = toDomainProject(row)
		return nil
	})

	return created, err
}

// ByID reads one project.
func (r *ProjectRepository) ByID(ctx context.Context, orgID, id uuid.UUID) (domain.Project, error) {
	var found domain.Project

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		row, err := sqlc.New(tx).GetProject(ctx, sqlc.GetProjectParams{OrgID: orgID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("read the project: %w", err)
		}

		found = toDomainProject(row)
		return nil
	})

	return found, err
}

// OfMember lists the projects a person belongs to.
func (r *ProjectRepository) OfMember(
	ctx context.Context, who domain.Identity,
) ([]domain.Project, error) {
	var projects []domain.Project

	err := r.db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
		rows, err := sqlc.New(tx).ListProjectsOfMember(ctx, sqlc.ListProjectsOfMemberParams{
			OrgID:  who.OrgID,
			UserID: who.UserID,
		})
		if err != nil {
			return fmt.Errorf("list the projects: %w", err)
		}

		projects = make([]domain.Project, 0, len(rows))
		for _, row := range rows {
			projects = append(projects, toDomainProject(row))
		}
		return nil
	})

	return projects, err
}

// Save writes a project's display name and details. Its slug does not move: it
// appears in URLs and in object storage keys, where a change would orphan what is
// already filed under it.
func (r *ProjectRepository) Save(
	ctx context.Context, orgID uuid.UUID, updated domain.Project,
) (domain.Project, error) {
	var saved domain.Project

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		row, err := sqlc.New(tx).UpdateProject(ctx, sqlc.UpdateProjectParams{
			OrgID:  orgID,
			ID:     updated.ID,
			Name:   updated.Name,
			Infra:  updated.Details.Infra,
			Region: updated.Details.Region,
			Owner:  updated.Details.Owner,
			Stack:  stackOrEmpty(updated.Details.Stack),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("save the project: %w", translate(err))
		}

		saved = toDomainProject(row)
		return nil
	})

	return saved, err
}

// stackOrEmpty keeps a null out of the column: an unset stack is an empty array,
// which is what the schema's default says too.
func stackOrEmpty(stack []string) []string {
	if stack == nil {
		return []string{}
	}
	return stack
}

// FavoritesOf lists the projects this person has starred, as a set. Read for the
// caller alone: the statement names the user as well as the organization.
func (r *ProjectRepository) FavoritesOf(ctx context.Context, who domain.Identity) (map[uuid.UUID]bool, error) {
	favorites := map[uuid.UUID]bool{}

	err := r.db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
		ids, err := sqlc.New(tx).ListFavoriteProjects(ctx, sqlc.ListFavoriteProjectsParams{
			OrgID: who.OrgID, UserID: who.UserID,
		})
		if err != nil {
			return fmt.Errorf("list the favourites: %w", err)
		}
		for _, id := range ids {
			favorites[id] = true
		}
		return nil
	})

	return favorites, err
}

// IsFavorite reports whether this person has starred the project.
func (r *ProjectRepository) IsFavorite(ctx context.Context, who domain.Identity, projectID uuid.UUID) (bool, error) {
	var starred bool

	err := r.db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
		var err error
		starred, err = sqlc.New(tx).IsFavoriteProject(ctx, sqlc.IsFavoriteProjectParams{
			OrgID: who.OrgID, UserID: who.UserID, ProjectID: projectID,
		})
		if err != nil {
			return fmt.Errorf("read the favourite: %w", err)
		}
		return nil
	})

	return starred, err
}

// SetFavorite stars or unstars a project for this person alone.
func (r *ProjectRepository) SetFavorite(
	ctx context.Context, who domain.Identity, projectID uuid.UUID, on bool,
) error {
	return r.db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
		queries := sqlc.New(tx)

		if on {
			return queries.AddFavoriteProject(ctx, sqlc.AddFavoriteProjectParams{
				OrgID: who.OrgID, UserID: who.UserID, ProjectID: projectID,
			})
		}
		return queries.RemoveFavoriteProject(ctx, sqlc.RemoveFavoriteProjectParams{
			OrgID: who.OrgID, UserID: who.UserID, ProjectID: projectID,
		})
	})
}

// Delete removes a project that holds no run.
//
// The count and the delete share one transaction on purpose. Checked outside,
// two callers could each see an empty project and the second would delete one
// that had meanwhile been used — and the foreign keys cascade, so that would
// take a run history with it.
func (r *ProjectRepository) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	return r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		queries := sqlc.New(tx)

		runs, err := queries.CountProjectRuns(ctx, sqlc.CountProjectRunsParams{
			OrgID:     orgID,
			ProjectID: id,
		})
		if err != nil {
			return fmt.Errorf("count the project's runs: %w", err)
		}
		if runs > 0 {
			return fmt.Errorf("%w: %d run(s)", domain.ErrProjectNotEmpty, runs)
		}

		affected, err := queries.DeleteProject(ctx, sqlc.DeleteProjectParams{OrgID: orgID, ID: id})
		if err != nil {
			return fmt.Errorf("delete the project: %w", err)
		}
		if affected == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
}

// translate turns a unique constraint violation into something a caller can
// match on, and leaves everything else alone.
func translate(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return domain.ErrAlreadyExists
	}
	return err
}

// ProjectIDs is every project of the organization.
//
// Read by the emergency stop, which asks the authorizer about each one: an
// organization's runs are an administrator's to stop only if they administer
// all of it.
func (r *ProjectRepository) ProjectIDs(ctx context.Context, orgID uuid.UUID) ([]uuid.UUID, error) {
	var ids []uuid.UUID

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		found, err := sqlc.New(tx).ListProjectIDs(ctx, orgID)
		if err != nil {
			return fmt.Errorf("list the projects: %w", err)
		}

		ids = found
		return nil
	})

	return ids, err
}
