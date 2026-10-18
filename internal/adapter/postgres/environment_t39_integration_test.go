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

// The days of a window, and when a target's challenge was last fetched, as the
// disk keeps them.

func TestTheDaysOfAWindowSurviveTheRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, project := newEnvironmentWorld(ctx, t, db)
	environments := postgres.NewEnvironmentWriteRepository(db)

	window := &domain.AllowedWindow{
		StartMinute: 22 * 60, EndMinute: 4 * 60, Zone: "Europe/Paris",
		Days: domain.DaysOf(time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday),
	}
	created, err := environments.Create(ctx, who.OrgID, declared(project, window), "token", time.Now().UTC())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	read, err := environments.ByID(ctx, who.OrgID, created.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if read.Window == nil || *read.Window != *window {
		t.Fatalf("stored %+v, read back %+v", *window, read.Window)
	}

	// Changing the days is saved; removing the window takes them with it, so a
	// window declared again later does not come back narrowed.
	read.Window.Days = domain.DaysOf(time.Saturday, time.Sunday)
	saved, err := environments.Save(ctx, who.OrgID, read, false)
	if err != nil || saved.Window.Days != domain.DaysOf(time.Saturday, time.Sunday) {
		t.Fatalf("Save days: %+v %v", saved.Window, err)
	}

	saved.Window = nil
	cleared, err := environments.Save(ctx, who.OrgID, saved, false)
	if err != nil || cleared.Window != nil {
		t.Fatalf("Save without a window: %+v %v", cleared.Window, err)
	}
	var days int16
	if err := db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT allowed_window_days FROM environment WHERE id = $1`, created.ID).Scan(&days)
	}); err != nil || days != 0 {
		t.Fatalf("days left behind a removed window: %d %v", days, err)
	}
}

// Days narrow a window. With none, there is nothing to narrow, and the schema
// refuses the row rather than trusting every writer.
func TestTheSchemaRefusesDaysWithNoWindow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, project := newEnvironmentWorld(ctx, t, db)

	err := db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO environment (id, org_id, project_id, name, base_url, verification_token,
			                         max_vus, max_duration_s, allowed_window_days)
			VALUES ($1, $2, $3, 'x', 'https://x.example.test', 't', 1, 1, 31)`,
			uuid.New(), who.OrgID, project)
		return err
	})
	if err == nil {
		t.Fatal("days with no window were accepted")
	}

	err = db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO environment (id, org_id, project_id, name, base_url, verification_token,
			                         max_vus, max_duration_s,
			                         allowed_window_start_minute, allowed_window_end_minute,
			                         allowed_window_days)
			VALUES ($1, $2, $3, 'y', 'https://y.example.test', 't', 1, 1, 60, 120, 128)`,
			uuid.New(), who.OrgID, project)
		return err
	})
	if err == nil {
		t.Fatal("a day bit past Sunday was accepted")
	}
}

func TestEveryAttemptToVerifyIsDated(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, project := newEnvironmentWorld(ctx, t, db)
	environments := postgres.NewEnvironmentWriteRepository(db)

	created, err := environments.Create(ctx, who.OrgID, declared(project, nil), "token", time.Now().UTC())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.VerificationCheckedAt != nil {
		t.Fatalf("a target nobody has checked says it was checked at %v", created.VerificationCheckedAt)
	}

	failedAt := time.Now().UTC().Truncate(time.Microsecond)
	failed, err := environments.RecordProofFailed(ctx, who.OrgID, created.ID, failedAt)
	if err != nil || failed.VerificationCheckedAt == nil || !failed.VerificationCheckedAt.Equal(failedAt) {
		t.Fatalf("a failed attempt: %+v %v", failed.VerificationCheckedAt, err)
	}

	provenAt := failedAt.Add(time.Minute)
	proven, err := environments.RecordProofOK(ctx, who.OrgID, created.ID, provenAt)
	if err != nil || proven.VerificationCheckedAt == nil || !proven.VerificationCheckedAt.Equal(provenAt) {
		t.Fatalf("a proven one: %+v %v", proven.VerificationCheckedAt, err)
	}
	if proven.VerificationStatus != domain.VerificationProofOK {
		t.Errorf("status %s", proven.VerificationStatus)
	}
}

// The page says who approved, and an unnamed person is said by their address.
func TestTheApproverIsReadAsAName(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, project := newEnvironmentWorld(ctx, t, db)
	environments := postgres.NewEnvironmentWriteRepository(db)

	if err := db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE app_user SET name = 'Maëlle Ferrand' WHERE id = $1`, who.UserID)
		return err
	}); err != nil {
		t.Fatalf("name the user: %v", err)
	}

	created, err := environments.Create(ctx, who.OrgID, declared(project, nil), "token", time.Now().UTC())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	at := time.Now().UTC()
	if _, err := environments.RecordProofOK(ctx, who.OrgID, created.ID, at); err != nil {
		t.Fatalf("RecordProofOK: %v", err)
	}
	entry := domain.Allow(who.OrgID, &who.UserID, domain.ActionTargetApproved, domain.TargetEnvironment, at).
		InProject(project).On(created.ID).With("host", "staging.example.test")
	if _, err := environments.Approve(ctx, who.OrgID, created.ID, who, at, "staging.example.test", entry); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	read, err := environments.ByID(ctx, who.OrgID, created.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if read.VerifiedByName != "Maëlle Ferrand" {
		t.Errorf("approved by %q", read.VerifiedByName)
	}

	// Without a name, the address.
	if err := db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE app_user SET name = '' WHERE id = $1`, who.UserID)
		return err
	}); err != nil {
		t.Fatalf("unname the user: %v", err)
	}
	read, _ = environments.ByID(ctx, who.OrgID, created.ID)
	if read.VerifiedByName == "" || read.VerifiedByName == "Maëlle Ferrand" {
		t.Errorf("an unnamed approver is said as %q", read.VerifiedByName)
	}
}

func TestAQueuedRunCanBeRejectedOnceAndOnlyWhileQueued(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	org := uuid.New()
	scenario, environment := seedProject(ctx, t, db, org)
	runs := postgres.NewRunRepository(db)

	params := domain.ProfileParams{TargetVUs: 1, PlateauS: 10}
	first, err := runs.Create(ctx, domain.NewRun(org, scenario, environment, domain.ProfileSmoke, params, nil))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	second, err := runs.Create(ctx, domain.NewRun(org, scenario, environment, domain.ProfileSmoke, params, nil))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	waiting, err := runs.QueuedRuns(ctx, org, 10)
	if err != nil || len(waiting) != 2 || waiting[0].ID != first.ID {
		t.Fatalf("QueuedRuns: %d runs, oldest %v, %v", len(waiting), waiting, err)
	}

	at := time.Now().UTC()
	rejected, err := runs.RejectQueued(ctx, org, first.ID, at, "the environment accepts no load at this hour")
	if err != nil || !rejected {
		t.Fatalf("RejectQueued: %v %v", rejected, err)
	}

	read, err := runs.ByID(ctx, org, first.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if read.Status != domain.StatusRejected || read.ErrorMessage == "" || read.EndedAt == nil {
		t.Errorf("a rejected run reads back as %s %q %v", read.Status, read.ErrorMessage, read.EndedAt)
	}

	// Not twice, and not once it is claimed.
	if again, _ := runs.RejectQueued(ctx, org, first.ID, at, "again"); again {
		t.Error("a run was rejected twice")
	}
	claimed, ok, err := runs.ClaimNextQueued(ctx, org, at)
	if err != nil || !ok || claimed.ID != second.ID {
		t.Fatalf("claim: %v %v %v", claimed.ID, ok, err)
	}
	if late, _ := runs.RejectQueued(ctx, org, second.ID, at, "too late"); late {
		t.Error("a run that was already running was rejected")
	}

	// And only the queued ones are listed.
	left, _ := runs.QueuedRuns(ctx, org, 10)
	if len(left) != 0 {
		t.Errorf("%d runs still listed as waiting", len(left))
	}
}
