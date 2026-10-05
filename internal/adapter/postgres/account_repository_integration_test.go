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
	"github.com/youssefmahersi/ballast/internal/usecase/auth"
)

func newAccount(subject string) auth.NewAccount {
	return auth.NewAccount{
		OrgID:       uuid.New(),
		OrgName:     "Ada's organization",
		UserID:      uuid.New(),
		Subject:     subject,
		Email:       subject + "@example.test",
		Name:        "Ada",
		ProjectID:   uuid.New(),
		ProjectName: "My first project",
		ProjectSlug: "my-first-project",
		Role:        domain.RoleAdmin,
		CreatedAt:   time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC),
	}
}

func dropAccount(t *testing.T, db *postgres.DB, org uuid.UUID) {
	t.Cleanup(func() {
		ctx := context.Background()
		_ = db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "DELETE FROM organization WHERE id = $1", org)
			return err
		})
	})
}

// A first sign-in, end to end through the application role: the account is
// created inside its own tenant transaction, then found again from outside any.
func TestCreatedAccountIsFoundBySubject(t *testing.T) {
	ctx := context.Background()
	db := openDB(ctx, t)
	repo := postgres.NewAccountRepository(db)

	account := newAccount("test|" + uuid.NewString())
	dropAccount(t, db, account.OrgID)

	if err := repo.Create(ctx, account); err != nil {
		t.Fatalf("Create: %v", err)
	}

	found, err := repo.Lookup(ctx, account.Subject)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	want := domain.Identity{UserID: account.UserID, OrgID: account.OrgID}
	if len(found) != 1 || found[0] != want {
		t.Fatalf("Lookup returned %v, want [%v]", found, want)
	}

	// The user administers the first project of their own organization.
	err = db.InTenantTx(ctx, account.OrgID, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx,
			"SELECT role FROM membership WHERE org_id = $1 AND user_id = $2 AND project_id = $3",
			account.OrgID, account.UserID, account.ProjectID).Scan(&role); err != nil {
			return err
		}
		if role != string(domain.RoleAdmin) {
			t.Errorf("membership role is %q, want admin", role)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read the membership: %v", err)
	}
}

// Half an account is worse than none: a user with no organization to sign in
// to, or an organization nobody administers. A failure on the last insert must
// leave nothing behind.
func TestAccountCreationIsAllOrNothing(t *testing.T) {
	ctx := context.Background()
	db := openDB(ctx, t)
	repo := postgres.NewAccountRepository(db)

	account := newAccount("test|" + uuid.NewString())
	account.Role = "owner" // refused by membership's CHECK constraint
	dropAccount(t, db, account.OrgID)

	if err := repo.Create(ctx, account); err == nil {
		t.Fatal("an account with an invalid role was created")
	}

	found, err := repo.Lookup(ctx, account.Subject)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if len(found) != 0 {
		t.Errorf("a failed creation left a user behind: %v", found)
	}

	err = db.InTenantTx(ctx, account.OrgID, func(tx pgx.Tx) error {
		if n := countRows(ctx, t, tx, "organization"); n != 0 {
			t.Errorf("a failed creation left %d organizations behind", n)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("count organizations: %v", err)
	}
}
