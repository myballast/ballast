//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/project"
)

// seedOrgAndUser lays down the two rows a project needs to hang off, through the
// application role and its policies.
func seedOrgAndUser(ctx context.Context, t *testing.T, db *postgres.DB) (domain.Identity, func()) {
	t.Helper()

	who := domain.Identity{OrgID: uuid.New(), UserID: uuid.New()}

	err := db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO organization (id, name) VALUES ($1, 'project repository test')`,
			who.OrgID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO app_user (id, org_id, subject, email) VALUES ($1, $2, $3, $4)`,
			who.UserID, who.OrgID, "subject-"+who.UserID.String(), who.UserID.String()+"@example.test")
		return err
	})
	if err != nil {
		t.Fatalf("seed the organization: %v", err)
	}

	cleanup := func() {
		ctx := context.WithoutCancel(ctx)
		_ = db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
			for _, table := range []string{
				"run_threshold", "run_metric", "run", "threshold", "scenario_version", "scenario",
				"secret", "environment", "membership", "project_allowed_host",
				"project", "app_user", "organization",
			} {
				if _, err := tx.Exec(ctx,
					"DELETE FROM "+pgx.Identifier{table}.Sanitize()+" WHERE org_id = $1", who.OrgID); err != nil {
					return err
				}
			}
			return nil
		})
	}
	t.Cleanup(cleanup)

	return who, cleanup
}

func anotherUser(ctx context.Context, t *testing.T, db *postgres.DB, org uuid.UUID) uuid.UUID {
	t.Helper()

	id := uuid.New()
	err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`INSERT INTO app_user (id, org_id, subject, email) VALUES ($1, $2, $3, $4)`,
			id, org, "subject-"+id.String(), id.String()+"@example.test")
		return err
	})
	if err != nil {
		t.Fatalf("seed a second user: %v", err)
	}
	return id
}

// Creating a project and making its creator an administrator is one
// transaction. A project nobody can administer would have to be repaired by
// hand.
func TestProjectRepositoryCreatesAnAdminMembership(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, _ := seedOrgAndUser(ctx, t, db)

	projects := postgres.NewProjectRepository(db)
	memberships := postgres.NewMembershipRepository(db)

	created, err := projects.Create(ctx, domain.Project{
		ID: uuid.New(), OrgID: who.OrgID, Name: "Checkout", Slug: "checkout",
	}, who, time.Now().UTC())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	membership, found, err := memberships.OfMember(ctx, who, created.ID)
	if err != nil || !found {
		t.Fatalf("the creator holds no membership: found=%t err=%v", found, err)
	}
	if membership.Role != domain.RoleAdmin {
		t.Errorf("the creator is %s, want admin", membership.Role)
	}
	if membership.Override != nil {
		t.Error("a permission mask was written; nothing in Ballast should write one")
	}

	mask, known := membership.Permissions()
	if !known || !mask.Has(domain.PermManageMembers) {
		t.Errorf("the creator cannot manage members: known=%t mask=%d", known, int64(mask))
	}
}

// Everything a project and its members go through, in order.
func TestProjectRepositoryRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, _ := seedOrgAndUser(ctx, t, db)
	other := anotherUser(ctx, t, db, who.OrgID)

	projects := postgres.NewProjectRepository(db)
	memberships := postgres.NewMembershipRepository(db)

	at := time.Now().UTC().Truncate(time.Millisecond)
	created, err := projects.Create(ctx, domain.Project{
		ID: uuid.New(), OrgID: who.OrgID, Name: "Checkout", Slug: "checkout",
	}, who, at)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !created.CreatedAt.Equal(at) {
		t.Errorf("created_at is %s, want the injected %s", created.CreatedAt, at)
	}

	read, err := projects.ByID(ctx, who.OrgID, created.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if read.Slug != "checkout" || read.Name != "Checkout" {
		t.Errorf("read back %+v", read)
	}

	mine, err := projects.OfMember(ctx, who)
	if err != nil {
		t.Fatalf("OfMember: %v", err)
	}
	if len(mine) != 1 || mine[0].ID != created.ID {
		t.Errorf("the creator's projects: %+v", mine)
	}

	// Somebody with no membership sees nothing, though the row is right there
	// in their own organization.
	theirs, err := projects.OfMember(ctx, domain.Identity{OrgID: who.OrgID, UserID: other})
	if err != nil {
		t.Fatalf("OfMember: %v", err)
	}
	if len(theirs) != 0 {
		t.Errorf("a non-member sees %d project(s)", len(theirs))
	}

	created.Name = "Checkout and payments"
	renamed, err := projects.Save(ctx, who.OrgID, created)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if renamed.Name != "Checkout and payments" || renamed.Slug != "checkout" {
		t.Errorf("after renaming: %+v", renamed)
	}

	if err := memberships.SetRole(ctx, who.OrgID, created.ID, other, domain.RoleRunner, roleChange(who, created.ID, other)); err != nil {
		t.Fatalf("SetRole: %v", err)
	}

	members, err := memberships.InProject(ctx, who.OrgID, created.ID)
	if err != nil {
		t.Fatalf("InProject: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("the project holds %d member(s), want 2", len(members))
	}
	byUser := map[uuid.UUID]project.Member{}
	for _, member := range members {
		byUser[member.UserID] = member
		if member.Email == "" {
			t.Error("a member has no email address")
		}
	}
	if byUser[other].Role != domain.RoleRunner {
		t.Errorf("the second member is %s, want runner", byUser[other].Role)
	}

	// Changing a role keeps the row, it does not add one.
	if err := memberships.SetRole(ctx, who.OrgID, created.ID, other, domain.RoleMaintainer, roleChange(who, created.ID, other)); err != nil {
		t.Fatalf("SetRole again: %v", err)
	}
	members, err = memberships.InProject(ctx, who.OrgID, created.ID)
	if err != nil {
		t.Fatalf("InProject: %v", err)
	}
	if len(members) != 2 {
		t.Errorf("changing a role left %d member(s)", len(members))
	}

	if err := memberships.Remove(ctx, who.OrgID, created.ID, other, roleChange(who, created.ID, other)); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := memberships.Remove(ctx, who.OrgID, created.ID, other, roleChange(who, created.ID, other)); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("removing twice: got %v, want ErrNotFound", err)
	}

	if err := projects.Delete(ctx, who.OrgID, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := projects.ByID(ctx, who.OrgID, created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("after deleting: got %v, want ErrNotFound", err)
	}
}

// The rows cascade, so a project with runs must not be deletable: it would take
// the history with it, and that is not recoverable.
func TestProjectRepositoryRefusesToDeleteAProjectWithRuns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, _ := seedOrgAndUser(ctx, t, db)

	projects := postgres.NewProjectRepository(db)

	created, err := projects.Create(ctx, domain.Project{
		ID: uuid.New(), OrgID: who.OrgID, Name: "Checkout", Slug: "checkout",
	}, who, time.Now().UTC())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	var runID uuid.UUID
	if err := db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
		env, scenario := uuid.New(), uuid.New()

		if _, err := tx.Exec(ctx, `INSERT INTO environment
			(id, org_id, project_id, name, base_url, verification_token,
			 verification_status, verified_at, verified_by, max_vus, max_duration_s)
			VALUES ($1, $2, $3, 'local', 'https://local.example.test', 'token',
			 'verified', now(), $4, 5, 60)`,
			env, who.OrgID, created.ID, who.UserID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO scenario (id, org_id, project_id, name, script)
			VALUES ($1, $2, $3, 'health', 'export default function () {}')`,
			scenario, who.OrgID, created.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO scenario_version
			(org_id, scenario_id, version, script, default_profile)
			VALUES ($1, $2, 1, 'export default function () {}', 'smoke')`,
			who.OrgID, scenario); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `INSERT INTO run
			(org_id, project_id, scenario_id, environment_id, scenario_version, profile)
			VALUES ($1, $2, $3, $4, 1, 'smoke') RETURNING id`,
			who.OrgID, created.ID, scenario, env).Scan(&runID)
	}); err != nil {
		t.Fatalf("seed a run: %v", err)
	}

	if err := projects.Delete(ctx, who.OrgID, created.ID); !errors.Is(err, domain.ErrProjectNotEmpty) {
		t.Fatalf("got %v, want ErrProjectNotEmpty", err)
	}

	// And the run is still there.
	if _, err := projects.ByID(ctx, who.OrgID, created.ID); err != nil {
		t.Errorf("the project was deleted anyway: %v", err)
	}
	var runs int
	if err := db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM run WHERE id = $1", runID).Scan(&runs)
	}); err != nil {
		t.Fatalf("count the runs: %v", err)
	}
	if runs != 1 {
		t.Error("the run history was lost")
	}
}

// Two slugs cannot collide inside one organization, and the error says which
// problem it is rather than naming a constraint.
func TestProjectRepositoryRefusesADuplicateSlug(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, _ := seedOrgAndUser(ctx, t, db)

	projects := postgres.NewProjectRepository(db)

	if _, err := projects.Create(ctx, domain.Project{
		ID: uuid.New(), OrgID: who.OrgID, Name: "Checkout", Slug: "checkout",
	}, who, time.Now().UTC()); err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err := projects.Create(ctx, domain.Project{
		ID: uuid.New(), OrgID: who.OrgID, Name: "Checkout again", Slug: "checkout",
	}, who, time.Now().UTC())
	if !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("got %v, want ErrAlreadyExists", err)
	}
}

// The same slug in another organization is not a collision: the uniqueness is
// per tenant, and the two must not be able to see each other at all.
func TestProjectRepositoryKeepsOrganizationsApart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	alpha, _ := seedOrgAndUser(ctx, t, db)
	beta, _ := seedOrgAndUser(ctx, t, db)

	projects := postgres.NewProjectRepository(db)
	memberships := postgres.NewMembershipRepository(db)

	alphaProject, err := projects.Create(ctx, domain.Project{
		ID: uuid.New(), OrgID: alpha.OrgID, Name: "Checkout", Slug: "checkout",
	}, alpha, time.Now().UTC())
	if err != nil {
		t.Fatalf("Create for alpha: %v", err)
	}
	if _, err := projects.Create(ctx, domain.Project{
		ID: uuid.New(), OrgID: beta.OrgID, Name: "Checkout", Slug: "checkout",
	}, beta, time.Now().UTC()); err != nil {
		t.Fatalf("the same slug in another organization was refused: %v", err)
	}

	if _, err := projects.ByID(ctx, beta.OrgID, alphaProject.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("beta read alpha's project: %v", err)
	}

	// And beta's identity holds no membership there, whatever alpha granted.
	if _, found, err := memberships.OfMember(ctx, beta, alphaProject.ID); err != nil || found {
		t.Errorf("beta holds a membership in alpha's project: found=%t err=%v", found, err)
	}
}

// roleChange is the audit entry a membership change carries into the
// transaction that performs it. What it holds is tested in usecase/project;
// here it only has to be a valid entry, because the same transaction writes it.
func roleChange(who domain.Identity, projectID, userID uuid.UUID) domain.AuditEntry {
	return domain.Allow(
		who.OrgID, &who.UserID, domain.ActionRoleChanged, domain.TargetMembership, time.Now().UTC(),
	).InProject(projectID).On(userID)
}
