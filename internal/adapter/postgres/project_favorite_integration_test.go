//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres"
	"github.com/youssefmahersi/ballast/internal/domain"
)

// aSecondUser puts another person in the organization, in the same project.
func aSecondUser(ctx context.Context, t *testing.T, db *postgres.DB, who domain.Identity, project uuid.UUID) domain.Identity {
	t.Helper()

	other := domain.Identity{OrgID: who.OrgID, UserID: uuid.New()}
	if err := db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO app_user (id, org_id, subject, email) VALUES ($1, $2, $3, $4)`,
			other.UserID, who.OrgID, "subject-"+other.UserID.String(), other.UserID.String()+"@example.test"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO membership (org_id, user_id, project_id, role) VALUES ($1, $2, $3, 'viewer')`,
			who.OrgID, other.UserID, project)
		return err
	}); err != nil {
		t.Fatalf("add a second user: %v", err)
	}
	return other
}

// The acceptance criterion at the one layer where it can fail: row-level
// security knows organizations and not people, so a star that leaked from one
// person to another inside an organization would pass every policy.
func TestAFavouriteOfOnePersonIsNotAnothers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	ada, project := newEnvironmentWorld(ctx, t, db)
	grace := aSecondUser(ctx, t, db, ada, project)
	projects := postgres.NewProjectRepository(db)

	if err := projects.SetFavorite(ctx, ada, project, true); err != nil {
		t.Fatalf("SetFavorite: %v", err)
	}

	mine, err := projects.FavoritesOf(ctx, ada)
	if err != nil || !mine[project] {
		t.Fatalf("Ada's own star: %v %v", mine, err)
	}
	theirs, err := projects.FavoritesOf(ctx, grace)
	if err != nil || len(theirs) != 0 {
		t.Fatalf("Grace sees %v of Ada's stars (%v)", theirs, err)
	}
	if starred, _ := projects.IsFavorite(ctx, grace, project); starred {
		t.Error("IsFavorite answers yes for Grace on Ada's star")
	}

	// Grace removing "her" star does not take Ada's.
	if err := projects.SetFavorite(ctx, grace, project, false); err != nil {
		t.Fatalf("SetFavorite off: %v", err)
	}
	if starred, _ := projects.IsFavorite(ctx, ada, project); !starred {
		t.Error("Grace unstarring took Ada's star with it")
	}
}

func TestStarringTwiceIsOneRowAndUnstarringWhatIsNotStarredIsFine(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	ada, project := newEnvironmentWorld(ctx, t, db)
	projects := postgres.NewProjectRepository(db)

	if err := projects.SetFavorite(ctx, ada, project, false); err != nil {
		t.Fatalf("unstarring what is not starred: %v", err)
	}
	for range 2 {
		if err := projects.SetFavorite(ctx, ada, project, true); err != nil {
			t.Fatalf("SetFavorite: %v", err)
		}
	}

	var rows int
	if err := db.InTenantTx(ctx, ada.OrgID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM project_favorite WHERE project_id = $1`, project).Scan(&rows)
	}); err != nil || rows != 1 {
		t.Fatalf("%d rows after starring twice (%v)", rows, err)
	}
}

func TestDeletingAProjectOrAPersonTakesTheirStars(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	ada, project := newEnvironmentWorld(ctx, t, db)
	projects := postgres.NewProjectRepository(db)
	if err := projects.SetFavorite(ctx, ada, project, true); err != nil {
		t.Fatalf("SetFavorite: %v", err)
	}

	if err := projects.Delete(ctx, ada.OrgID, project); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	var rows int
	if err := db.InTenantTx(ctx, ada.OrgID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM project_favorite`).Scan(&rows)
	}); err != nil || rows != 0 {
		t.Fatalf("%d stars outlived their project (%v)", rows, err)
	}
}

func TestDetailsSurviveTheRoundTripAndTheSchemaBoundsThem(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	ada, project := newEnvironmentWorld(ctx, t, db)
	projects := postgres.NewProjectRepository(db)

	read, err := projects.ByID(ctx, ada.OrgID, project)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	// A project written before details existed reads back with nothing said.
	if read.Details.Infra != "" || len(read.Details.Stack) != 0 {
		t.Fatalf("a fresh project says %+v", read.Details)
	}

	read.Details = domain.ProjectDetails{Infra: "k8s", Region: "eu-west-1", Owner: "payments", Stack: []string{"go", "pg"}}
	saved, err := projects.Save(ctx, ada.OrgID, read)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if saved.Details.Infra != "k8s" || len(saved.Details.Stack) != 2 || saved.Details.Stack[1] != "pg" {
		t.Errorf("saved %+v", saved.Details)
	}

	// A writer that skips the domain is stopped by the schema.
	err = db.InTenantTx(ctx, ada.OrgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE project SET stack = $2 WHERE id = $1`, project,
			[]string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"})
		return err
	})
	if err == nil {
		t.Error("thirteen stack labels were stored")
	}
}
