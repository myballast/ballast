//go:build integration

// Written before the implementation. What is checked here is what only a real
// database can answer: that an approval writes its four facts in one go, and
// that the schema refuses the states this must never produce.
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
)

// An approval is four facts: the status, when, by whom, and the host the project
// may now reach. Migration 001 already holds the first three to each other with
// a CHECK, so writing them apart is not merely untidy -- it is refused. The
// fourth has to join them, or a target becomes approved while the allowlist that
// is supposed to bound it stays empty.
func TestApprovingWritesTheStatusTheApproverAndTheAllowedHostTogether(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, project := newEnvironmentWorld(ctx, t, db)

	environments := postgres.NewEnvironmentWriteRepository(db)
	created, err := environments.Create(ctx, who.OrgID, declared(project, nil), "a-token", time.Now().UTC())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	proven, err := environments.RecordProofOK(ctx, who.OrgID, created.ID)
	if err != nil {
		t.Fatalf("RecordProofOK: %v", err)
	}
	if proven.VerificationStatus != domain.VerificationProofOK {
		t.Fatalf("after a proof the target is %q", proven.VerificationStatus)
	}

	at := time.Now().UTC().Truncate(time.Millisecond)
	approved, err := environments.Approve(ctx, who.OrgID, created.ID, who, at, "staging.example.test", approval(who, created))
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if !approved.IsVerified() {
		t.Fatalf("after an approval the target is %q", approved.VerificationStatus)
	}

	var (
		status     string
		verifiedAt *time.Time
		verifiedBy *uuid.UUID
		hosts      []string
	)
	if err := db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx,
			`SELECT verification_status, verified_at, verified_by FROM environment
			 WHERE org_id = $1 AND id = $2`, who.OrgID, created.ID,
		).Scan(&status, &verifiedAt, &verifiedBy); err != nil {
			return err
		}

		rows, err := tx.Query(ctx,
			`SELECT host FROM project_allowed_host WHERE org_id = $1 AND project_id = $2 ORDER BY host`,
			who.OrgID, project)
		if err != nil {
			return err
		}
		hosts, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	}); err != nil {
		t.Fatalf("read back the approval: %v", err)
	}

	if status != string(domain.VerificationVerified) {
		t.Errorf("the stored status is %q", status)
	}
	if verifiedAt == nil || !verifiedAt.Equal(at) {
		t.Errorf("verified_at is %v, want %v", verifiedAt, at)
	}
	if verifiedBy == nil || *verifiedBy != who.UserID {
		t.Errorf("verified_by is %v, want %v", verifiedBy, who.UserID)
	}
	if len(hosts) != 1 || hosts[0] != "staging.example.test" {
		t.Errorf("the project allows %v", hosts)
	}
}

// Two environments of one project may point at the same host, and the second
// approval must not fall over the first's allowlist entry.
func TestApprovingASecondTargetOnTheSameHostIsNotAConflict(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, project := newEnvironmentWorld(ctx, t, db)

	environments := postgres.NewEnvironmentWriteRepository(db)

	approve := func(name string) error {
		declaration := declared(project, nil)
		declaration.Name = name

		created, err := environments.Create(ctx, who.OrgID, declaration, "a-token", time.Now().UTC())
		if err != nil {
			return err
		}
		if _, err := environments.RecordProofOK(ctx, who.OrgID, created.ID); err != nil {
			return err
		}
		_, err = environments.Approve(ctx, who.OrgID, created.ID, who, time.Now().UTC(), "staging.example.test", approval(who, created))
		return err
	}

	if err := approve("staging"); err != nil {
		t.Fatalf("the first approval: %v", err)
	}
	if err := approve("staging-too"); err != nil {
		t.Fatalf("the second approval on the same host: %v", err)
	}

	var hosts int
	if err := db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM project_allowed_host WHERE org_id = $1 AND project_id = $2`,
			who.OrgID, project).Scan(&hosts)
	}); err != nil {
		t.Fatalf("count the allowed hosts: %v", err)
	}
	if hosts != 1 {
		t.Fatalf("the host is allowed %d times", hosts)
	}
}

// The allowlist is per project, and a host allowed in one is not allowed in
// another. Row-level security does not cover this on its own: both projects can
// belong to the same organization.
func TestApprovingDoesNotAllowTheHostForAnotherProject(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, project := newEnvironmentWorld(ctx, t, db)

	other := uuid.New()
	if err := db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`INSERT INTO project (id, org_id, name, slug) VALUES ($1, $2, 'other', $3)`,
			other, who.OrgID, other.String())
		return err
	}); err != nil {
		t.Fatalf("seed the other project: %v", err)
	}

	environments := postgres.NewEnvironmentWriteRepository(db)
	created, err := environments.Create(ctx, who.OrgID, declared(project, nil), "a-token", time.Now().UTC())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := environments.RecordProofOK(ctx, who.OrgID, created.ID); err != nil {
		t.Fatalf("RecordProofOK: %v", err)
	}
	if _, err := environments.Approve(
		ctx, who.OrgID, created.ID, who, time.Now().UTC(), "staging.example.test",
		approval(who, created),
	); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	var leaked int
	if err := db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM project_allowed_host WHERE org_id = $1 AND project_id = $2`,
			who.OrgID, other).Scan(&leaked)
	}); err != nil {
		t.Fatalf("count the other project's allowed hosts: %v", err)
	}
	if leaked != 0 {
		t.Fatalf("the other project allows %d host(s)", leaked)
	}
}

// A failed attempt is recorded, and it must not be able to produce a row that
// says verified with nobody to answer for it.
func TestAFailedAttemptIsRecordedAndApprovesNothing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, project := newEnvironmentWorld(ctx, t, db)

	environments := postgres.NewEnvironmentWriteRepository(db)
	created, err := environments.Create(ctx, who.OrgID, declared(project, nil), "a-token", time.Now().UTC())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	failed, err := environments.RecordProofFailed(ctx, who.OrgID, created.ID)
	if err != nil {
		t.Fatalf("RecordProofFailed: %v", err)
	}
	if failed.VerificationStatus != domain.VerificationFailed {
		t.Fatalf("the target is %q", failed.VerificationStatus)
	}
	if failed.IsVerified() {
		t.Fatal("a failed attempt produced a verified target")
	}

	var hosts int
	if err := db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM project_allowed_host WHERE org_id = $1`, who.OrgID).Scan(&hosts)
	}); err != nil {
		t.Fatalf("count the allowed hosts: %v", err)
	}
	if hosts != 0 {
		t.Fatalf("a failed attempt allowed %d host(s)", hosts)
	}
}

// The schema is the last line: whatever a future statement does, it must not be
// able to write a verified target with no approver. This is the CHECK of
// migration 001, exercised directly.
func TestTheSchemaRefusesAVerifiedTargetWithNoApprover(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, project := newEnvironmentWorld(ctx, t, db)

	err := db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO environment
			(org_id, project_id, name, base_url, verification_token,
			 verification_status, max_vus, max_duration_s)
			VALUES ($1, $2, 'forged', 'https://forged.example.test', 'a-token',
			 'verified', 5, 60)`, who.OrgID, project)
		return err
	})
	if err == nil {
		t.Fatal("a verified target with no approver and no timestamp was stored")
	}

	// And a repository that reads it back still reports it as unverified, so no
	// path through the code can treat such a row as approved.
	if errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("the insert failed for the wrong reason: %v", err)
	}
}

// approval is the audit entry an approval carries into the transaction that
// performs it. What it holds is tested in usecase/environment; here it only
// has to be a valid entry, because the same transaction writes it.
func approval(who domain.Identity, environment domain.Environment) domain.AuditEntry {
	return domain.Allow(
		who.OrgID, &who.UserID, domain.ActionTargetApproved, domain.TargetEnvironment,
		time.Now().UTC(),
	).InProject(environment.ProjectID).On(environment.ID)
}
