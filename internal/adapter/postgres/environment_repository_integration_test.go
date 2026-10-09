//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/infra/crypto"
)

// The value exists in this file to be looked for in places it must not be.
const storedSecret = "ghp_a_token_that_would_be_worth_stealing"

func testCipher(t *testing.T) *crypto.Cipher {
	t.Helper()

	cipher, err := crypto.NewCipher([]byte(strings.Repeat("p", crypto.KeyLength)))
	if err != nil {
		t.Fatalf("build the cipher: %v", err)
	}
	return cipher
}

// newEnvironmentWorld lays down an organization with a project, through the
// application role and its policies.
func newEnvironmentWorld(ctx context.Context, t *testing.T, db *postgres.DB) (domain.Identity, uuid.UUID) {
	t.Helper()

	who, _ := seedOrgAndUser(ctx, t, db)
	project := uuid.New()

	if err := db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`INSERT INTO project (id, org_id, name, slug) VALUES ($1, $2, 'p', $3)`,
			project, who.OrgID, project.String())
		return err
	}); err != nil {
		t.Fatalf("seed the project: %v", err)
	}

	return who, project
}

func declared(project uuid.UUID, window *domain.AllowedWindow) domain.Environment {
	return domain.Environment{
		ID:           uuid.New(),
		ProjectID:    project,
		Name:         "staging",
		BaseURL:      "https://staging.example.test",
		MaxVUs:       25,
		MaxDurationS: 300,
		Window:       window,
	}
}

// The ticket's acceptance criterion, at the one layer where it can actually be
// checked: what is on the disk. Read the column directly rather than through the
// repository, because a repository that returned the plaintext it was given
// without ever encrypting it would satisfy a round-trip test perfectly.
func TestWhatIsWrittenToTheSecretColumnIsNotReadable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, project := newEnvironmentWorld(ctx, t, db)

	environments := postgres.NewEnvironmentWriteRepository(db)
	created, err := environments.Create(ctx, who.OrgID, declared(project, nil), "token", time.Now().UTC())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	secrets := postgres.NewSecretRepository(db, testCipher(t))
	if _, err := secrets.Put(ctx, who.OrgID, created.ID, "API_TOKEN", storedSecret, time.Now().UTC()); err != nil {
		t.Fatalf("Put: %v", err)
	}

	var ciphertext []byte
	if err := db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT value_encrypted FROM secret WHERE org_id = $1 AND environment_id = $2 AND key = 'API_TOKEN'`,
			who.OrgID, created.ID).Scan(&ciphertext)
	}); err != nil {
		t.Fatalf("read the column: %v", err)
	}

	if len(ciphertext) == 0 {
		t.Fatal("the column is empty")
	}
	if bytes.Contains(ciphertext, []byte(storedSecret)) {
		t.Fatal("the secret is readable in the column")
	}
	if bytes.Contains(ciphertext, []byte("ghp_")) {
		t.Fatal("part of the secret is readable in the column")
	}

	// And it comes back, which is what makes the above a statement about
	// encryption rather than about the value having been lost.
	injected, err := secrets.SecretsOf(ctx, who.OrgID, created.ID)
	if err != nil {
		t.Fatalf("SecretsOf: %v", err)
	}
	if injected["API_TOKEN"] != storedSecret {
		t.Fatalf("the secret came back as %q", injected["API_TOKEN"])
	}
}

// Another key cannot open what this one sealed, and the failure stops the run
// rather than handing the generator a scenario with no credential.
func TestASecretSealedUnderAnotherKeyIsNotHandedOver(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, project := newEnvironmentWorld(ctx, t, db)

	environments := postgres.NewEnvironmentWriteRepository(db)
	created, err := environments.Create(ctx, who.OrgID, declared(project, nil), "token", time.Now().UTC())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	written := postgres.NewSecretRepository(db, testCipher(t))
	if _, err := written.Put(ctx, who.OrgID, created.ID, "API_TOKEN", storedSecret, time.Now().UTC()); err != nil {
		t.Fatalf("Put: %v", err)
	}

	otherKey, err := crypto.NewCipher([]byte(strings.Repeat("q", crypto.KeyLength)))
	if err != nil {
		t.Fatalf("build the other cipher: %v", err)
	}

	_, err = postgres.NewSecretRepository(db, otherKey).SecretsOf(ctx, who.OrgID, created.ID)
	if err == nil {
		t.Fatal("a repository with the wrong key handed the secret over")
	}
	if strings.Contains(err.Error(), "ghp_") {
		t.Fatalf("the error carries the secret: %v", err)
	}
	// The key is named, because somebody has to be able to find out which row is
	// the problem.
	if !strings.Contains(err.Error(), "API_TOKEN") {
		t.Fatalf("the error does not say which secret failed: %v", err)
	}
}

// The ciphertext is bound to the row it was written to. Moving one — which is
// what somebody with SQL access and no key would try — breaks it, rather than
// being decrypted into an environment of their own.
func TestACiphertextMovedToAnotherEnvironmentNoLongerOpens(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, project := newEnvironmentWorld(ctx, t, db)

	environments := postgres.NewEnvironmentWriteRepository(db)

	source, err := environments.Create(ctx, who.OrgID, declared(project, nil), "token", time.Now().UTC())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	elsewhere := declared(project, nil)
	elsewhere.Name = "another"
	target, err := environments.Create(ctx, who.OrgID, elsewhere, "token", time.Now().UTC())
	if err != nil {
		t.Fatalf("Create the second environment: %v", err)
	}

	secrets := postgres.NewSecretRepository(db, testCipher(t))
	if _, err := secrets.Put(ctx, who.OrgID, source.ID, "API_TOKEN", storedSecret, time.Now().UTC()); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Straight SQL, which is the whole point: this is what a stolen database
	// connection can do.
	if err := db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO secret (org_id, environment_id, key, value_encrypted)
			SELECT $1, $2, key, value_encrypted FROM secret
			WHERE org_id = $1 AND environment_id = $3`,
			who.OrgID, target.ID, source.ID)
		return err
	}); err != nil {
		t.Fatalf("copy the row: %v", err)
	}

	if _, err := secrets.SecretsOf(ctx, who.OrgID, target.ID); err == nil {
		t.Fatal("a ciphertext copied into another environment was decrypted")
	}
}

func TestAWindowSurvivesTheRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, project := newEnvironmentWorld(ctx, t, db)

	environments := postgres.NewEnvironmentWriteRepository(db)

	// A window that crosses midnight, in a zone that is not UTC: the two things
	// the storage could flatten without anybody noticing.
	window := &domain.AllowedWindow{StartMinute: 22 * 60, EndMinute: 6 * 60, Zone: "Europe/Paris"}
	created, err := environments.Create(ctx, who.OrgID, declared(project, window), "token", time.Now().UTC())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	read, err := environments.ByID(ctx, who.OrgID, created.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if read.Window == nil {
		t.Fatal("the window was not stored")
	}
	if *read.Window != *window {
		t.Fatalf("stored %+v, read back %+v", *window, *read.Window)
	}

	// And removing it leaves no half a window behind.
	read.Window = nil
	saved, err := environments.Save(ctx, who.OrgID, read, false)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if saved.Window != nil {
		t.Fatalf("the window is still %+v", *saved.Window)
	}
}

// Half a window would be a rule nobody can read: a start with no end says
// nothing. Migration 005 refuses it in the schema rather than trusting every
// future statement to write both.
func TestTheSchemaRefusesHalfAWindow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, project := newEnvironmentWorld(ctx, t, db)

	err := db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO environment
			(org_id, project_id, name, base_url, verification_token, max_vus,
			 max_duration_s, allowed_window_start_minute)
			VALUES ($1, $2, 'half', 'https://half.example.test', 'token', 5, 60, 600)`,
			who.OrgID, project)
		return err
	})
	if err == nil {
		t.Fatal("an environment with a start and no end was stored")
	}

	outOfRange := db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO environment
			(org_id, project_id, name, base_url, verification_token, max_vus,
			 max_duration_s, allowed_window_start_minute, allowed_window_end_minute)
			VALUES ($1, $2, 'late', 'https://late.example.test', 'token', 5, 60, 0, 1440)`,
			who.OrgID, project)
		return err
	})
	if outOfRange == nil {
		t.Fatal("a window ending at minute 1440 of the day was stored")
	}
}

// Pointing an environment somewhere else must clear the proof in the same
// statement that moves it, and migration 001 holds the status, the timestamp and
// the approver to each other — so clearing one without the others would be
// refused by the schema. This is the check that the three are written together.
func TestRetargetingAnEnvironmentClearsTheProofInTheDatabase(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, project := newEnvironmentWorld(ctx, t, db)

	environments := postgres.NewEnvironmentWriteRepository(db)
	created, err := environments.Create(ctx, who.OrgID, declared(project, nil), "token", time.Now().UTC())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Approved, the only state that lets load through.
	if err := db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE environment
			SET verification_status = 'verified', verified_at = now(), verified_by = $3
			WHERE org_id = $1 AND id = $2`, who.OrgID, created.ID, who.UserID)
		return err
	}); err != nil {
		t.Fatalf("approve the target: %v", err)
	}

	retargeted := created
	retargeted.BaseURL = "https://not-ours.example.test"
	if _, err := environments.Save(ctx, who.OrgID, retargeted, true); err != nil {
		t.Fatalf("Save: %v", err)
	}

	var (
		status     string
		verifiedAt *time.Time
		verifiedBy *uuid.UUID
	)
	if err := db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT verification_status, verified_at, verified_by FROM environment WHERE org_id = $1 AND id = $2`,
			who.OrgID, created.ID).Scan(&status, &verifiedAt, &verifiedBy)
	}); err != nil {
		t.Fatalf("read the environment: %v", err)
	}

	if status != string(domain.VerificationPending) {
		t.Fatalf("the status is %q after the target moved", status)
	}
	if verifiedAt != nil || verifiedBy != nil {
		t.Fatalf("the approval is still there: at=%v by=%v", verifiedAt, verifiedBy)
	}
}

// The rows cascade, so deleting an environment that holds runs would take the
// history of everything ever measured there with it. The count and the delete
// share one transaction, which is what this exercises.
func TestDeletingAnEnvironmentWithRunsIsRefused(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, project, environment := newScenarioWorld(ctx, t, db)

	scenario := uuid.New()
	if err := db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO scenario (id, org_id, project_id, name, script) VALUES ($1, $2, $3, 'checkout', 'x')`,
			scenario, who.OrgID, project); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO scenario_version (org_id, scenario_id, version, script, default_profile)
			 VALUES ($1, $2, 1, 'x', 'smoke')`, who.OrgID, scenario)
		return err
	}); err != nil {
		t.Fatalf("seed the scenario: %v", err)
	}

	runs := postgres.NewRunRepository(db)
	if _, err := runs.Create(ctx, domain.NewRun(
		who.OrgID,
		domain.Scenario{ID: scenario, ProjectID: project, Version: 1},
		domain.Environment{ID: environment, ProjectID: project, MaxVUs: 5, MaxDurationS: 60},
		domain.ProfileSmoke,
		domain.ProfileParams{TargetVUs: 1, PlateauS: 5},
		nil,
	)); err != nil {
		t.Fatalf("record a run: %v", err)
	}

	err := postgres.NewEnvironmentWriteRepository(db).Delete(ctx, who.OrgID, environment)
	if !errors.Is(err, domain.ErrEnvironmentNotEmpty) {
		t.Fatalf("Delete answered %v, wanted ErrEnvironmentNotEmpty", err)
	}

	var runsLeft int
	if err := db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM run WHERE org_id = $1`, who.OrgID).Scan(&runsLeft)
	}); err != nil {
		t.Fatalf("count the runs: %v", err)
	}
	if runsLeft != 1 {
		t.Fatalf("%d runs survived the refused deletion", runsLeft)
	}
}

func TestDeletingAnEnvironmentTakesItsSecretsWithIt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, project := newEnvironmentWorld(ctx, t, db)

	environments := postgres.NewEnvironmentWriteRepository(db)
	created, err := environments.Create(ctx, who.OrgID, declared(project, nil), "token", time.Now().UTC())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	secrets := postgres.NewSecretRepository(db, testCipher(t))
	if _, err := secrets.Put(ctx, who.OrgID, created.ID, "API_TOKEN", storedSecret, time.Now().UTC()); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if err := environments.Delete(ctx, who.OrgID, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	var left int
	if err := db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM secret WHERE org_id = $1 AND environment_id = $2`,
			who.OrgID, created.ID).Scan(&left)
	}); err != nil {
		t.Fatalf("count the secrets: %v", err)
	}
	if left != 0 {
		t.Fatalf("%d secrets outlived their environment", left)
	}
}

func TestSettingASecretAgainReplacesItAndRecordsWhen(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, project := newEnvironmentWorld(ctx, t, db)

	environments := postgres.NewEnvironmentWriteRepository(db)
	created, err := environments.Create(ctx, who.OrgID, declared(project, nil), "token", time.Now().UTC())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	secrets := postgres.NewSecretRepository(db, testCipher(t))

	written := time.Now().UTC().Truncate(time.Millisecond)
	first, err := secrets.Put(ctx, who.OrgID, created.ID, "API_TOKEN", "the-first-one", written)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	rotated := written.Add(time.Hour)
	second, err := secrets.Put(ctx, who.OrgID, created.ID, "API_TOKEN", storedSecret, rotated)
	if err != nil {
		t.Fatalf("Put again: %v", err)
	}

	if second.ID != first.ID {
		t.Fatal("replacing a secret made a second row")
	}
	if !second.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("created_at moved from %v to %v", first.CreatedAt, second.CreatedAt)
	}
	if !second.UpdatedAt.Equal(rotated) {
		t.Fatalf("updated_at is %v, wanted %v", second.UpdatedAt, rotated)
	}

	injected, err := secrets.SecretsOf(ctx, who.OrgID, created.ID)
	if err != nil {
		t.Fatalf("SecretsOf: %v", err)
	}
	if injected["API_TOKEN"] != storedSecret {
		t.Fatalf("the value is %q", injected["API_TOKEN"])
	}

	keys, err := secrets.Keys(ctx, who.OrgID, created.ID)
	if err != nil {
		t.Fatalf("Keys: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("%d keys are set", len(keys))
	}
}

func TestTwoEnvironmentsOfAProjectCannotShareAName(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, project := newEnvironmentWorld(ctx, t, db)

	environments := postgres.NewEnvironmentWriteRepository(db)
	if _, err := environments.Create(ctx, who.OrgID, declared(project, nil), "token", time.Now().UTC()); err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err := environments.Create(ctx, who.OrgID, declared(project, nil), "token", time.Now().UTC())
	if !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("the second one answered %v, wanted ErrAlreadyExists", err)
	}
}

// The run service reads its target through EnvironmentRepository, not through the
// write side, and that is the read the window has to survive: it is what decides
// whether a queued run may start. A mapping that dropped it would leave every
// window stored and none enforced, with every unit test still passing.
func TestTheRunSideReadsTheWindowToo(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, project := newEnvironmentWorld(ctx, t, db)

	window := &domain.AllowedWindow{StartMinute: 22 * 60, EndMinute: 6 * 60, Zone: "Europe/Paris"}
	created, err := postgres.NewEnvironmentWriteRepository(db).
		Create(ctx, who.OrgID, declared(project, window), "token", time.Now().UTC())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	read, err := postgres.NewEnvironmentRepository(db).EnvironmentByID(ctx, who.OrgID, created.ID)
	if err != nil {
		t.Fatalf("EnvironmentByID: %v", err)
	}

	if read.Window == nil {
		t.Fatal("the run side reads no window, so no window would ever be enforced")
	}
	if *read.Window != *window {
		t.Fatalf("read %+v, stored %+v", *read.Window, *window)
	}

	// And the rule itself, through the type the run service actually holds.
	night, err := time.Parse(time.RFC3339, "2026-10-06T23:30:00+02:00")
	if err != nil {
		t.Fatalf("the test's own timestamp is wrong: %v", err)
	}
	if err := read.RequireWindowAllows(night, 30*time.Minute); err != nil {
		t.Errorf("half past eleven at night was refused by a window open from 22:00: %v", err)
	}
	if err := read.RequireWindowAllows(night.Add(10*time.Hour), 30*time.Minute); err == nil {
		t.Error("half past nine in the morning was allowed")
	}
	// And the length is part of the question, through the type the run service
	// actually holds.
	if err := read.RequireWindowAllows(night, 8*time.Hour); err == nil {
		t.Error("an eight hour run was allowed into a window closing at 06:00")
	}
}
