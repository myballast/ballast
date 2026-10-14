//go:build integration

// The journal, against a real database: what it stores, what one project's
// reading returns, and — the one that matters most — that an organization's
// journal is unreadable from another. A journal read sideways would be worse
// than no journal at all: it is read by the people trusted to read everything.
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

func TestAnEntryComesBackAsItWasWritten(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	journal := postgres.NewAuditRepository(world.db)
	run := world.queue(ctx, t, world.env)

	at := time.Now().UTC().Truncate(time.Microsecond)
	written := domain.Allow(world.org, &world.user, domain.ActionRunQueued, domain.TargetRun, at).
		InProject(world.scenario.ProjectID).
		On(run.ID).
		With("profile", "smoke").
		With("target_vus", "3")

	if err := journal.Record(ctx, written); err != nil {
		t.Fatalf("Record: %v", err)
	}

	entries, err := journal.InProject(ctx, world.org, world.scenario.ProjectID, 0)
	if err != nil {
		t.Fatalf("InProject: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("%d entries came back", len(entries))
	}

	read := entries[0]
	switch {
	case read.ActorID == nil || *read.ActorID != world.user:
		t.Errorf("the author is %v", read.ActorID)
	case read.Action != domain.ActionRunQueued || read.Outcome != domain.Allowed:
		t.Errorf("the entry says %s / %s", read.Action, read.Outcome)
	case read.TargetType != domain.TargetRun || read.TargetID == nil || *read.TargetID != run.ID:
		t.Errorf("the target is %s %v", read.TargetType, read.TargetID)
	case !read.At.Equal(at):
		t.Errorf("the entry is dated %s, wrote %s", read.At, at)
	case read.Details["profile"] != "smoke" || read.Details["target_vus"] != "3":
		t.Errorf("the details are %v", read.Details)
	}
}

// An action on the whole organization is written once and read from every
// project's journal. Written per project instead, one action would become
// several entries and nobody could tell it had happened once.
func TestAnOrganizationWideEntryIsReadFromEveryProjectsJournal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	journal := postgres.NewAuditRepository(world.db)

	at := time.Now().UTC().Truncate(time.Microsecond)
	if err := journal.Record(ctx, domain.Allow(
		world.org, &world.user, domain.ActionRunsStopped, domain.TargetOrg, at,
	).With("cancelled", "3")); err != nil {
		t.Fatalf("Record: %v", err)
	}

	// A project of this organization, and another one entirely: both see it,
	// because both are in the organization that was stopped.
	another := uuid.New()
	for _, project := range []uuid.UUID{world.scenario.ProjectID, another} {
		entries, err := journal.InProject(ctx, world.org, project, 0)
		if err != nil {
			t.Fatalf("InProject: %v", err)
		}
		if len(entries) != 1 || entries[0].Action != domain.ActionRunsStopped {
			t.Fatalf("the journal of %s holds %+v", project, entries)
		}
		if entries[0].ProjectID != nil {
			t.Fatalf("an organization-wide entry was filed under %v", entries[0].ProjectID)
		}
	}
}

// One project's journal is one project's: an entry about another project of the
// same organization does not appear in it.
func TestOneProjectsJournalHoldsNoOtherProjectsEntries(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	journal := postgres.NewAuditRepository(world.db)

	elsewhere := uuid.New()
	at := time.Now().UTC().Truncate(time.Microsecond)
	if err := journal.Record(ctx, domain.Refuse(
		world.org, &world.user, domain.ActionRunRefused, domain.TargetRun,
		"the target has not been verified", at,
	).InProject(elsewhere)); err != nil {
		t.Fatalf("Record: %v", err)
	}

	entries, err := journal.InProject(ctx, world.org, world.scenario.ProjectID, 0)
	if err != nil {
		t.Fatalf("InProject: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("another project's entries appeared: %+v", entries)
	}
}

// Newest first, and a page at a time: a journal only grows, so no reading is
// ever the whole of it.
func TestTheJournalComesBackNewestFirstAndAPageAtATime(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	journal := postgres.NewAuditRepository(world.db)

	at := time.Now().UTC().Truncate(time.Microsecond)
	for i := range 5 {
		if err := journal.Record(ctx, domain.Allow(
			world.org, &world.user, domain.ActionRunQueued, domain.TargetRun,
			at.Add(time.Duration(i)*time.Second),
		).InProject(world.scenario.ProjectID).With("n", string(rune('0'+i)))); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	entries, err := journal.InProject(ctx, world.org, world.scenario.ProjectID, 3)
	if err != nil {
		t.Fatalf("InProject: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("a page of three returned %d", len(entries))
	}
	if entries[0].Details["n"] != "4" || entries[2].Details["n"] != "2" {
		t.Fatalf("the page is %v, %v, %v",
			entries[0].Details["n"], entries[1].Details["n"], entries[2].Details["n"])
	}
}

// The one that matters most. Row-level security is what keeps a journal from
// becoming the easiest way to read another organization's business: it holds
// who ran what, against which host, and when they were refused.
func TestOneOrganizationsJournalIsUnreadableByAnother(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	theirs := newQueueWorld(ctx, t)
	theirJournal := postgres.NewAuditRepository(theirs.db)

	at := time.Now().UTC().Truncate(time.Microsecond)
	if err := theirJournal.Record(ctx, domain.Allow(
		theirs.org, &theirs.user, domain.ActionTargetApproved, domain.TargetEnvironment, at,
	).InProject(theirs.scenario.ProjectID).On(theirs.env.ID).
		With("host", "staging.their-company.test")); err != nil {
		t.Fatalf("Record: %v", err)
	}

	ours := newQueueWorld(ctx, t)
	ourJournal := postgres.NewAuditRepository(ours.db)

	// Their project identifier is known here, which is the only interesting
	// case: guessing one must still show nothing.
	entries, err := ourJournal.InProject(ctx, ours.org, theirs.scenario.ProjectID, 0)
	if err != nil {
		t.Fatalf("InProject: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("another organization's journal was readable: %+v", entries)
	}

	// Which is not the entry having vanished: its own organization still has it.
	stillTheirs, err := theirJournal.InProject(ctx, theirs.org, theirs.scenario.ProjectID, 0)
	if err != nil {
		t.Fatalf("InProject: %v", err)
	}
	if len(stillTheirs) != 1 {
		t.Fatalf("their own journal reads as %+v", stillTheirs)
	}
}

// An entry Ballast wrote itself — a worker refusing a run accepted hours ago —
// has no author, and the schema has to accept that.
func TestAnEntryWithNoAuthorIsStored(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	journal := postgres.NewAuditRepository(world.db)
	run := world.queue(ctx, t, world.env)

	at := time.Now().UTC().Truncate(time.Microsecond)
	if err := journal.Record(ctx, domain.Refuse(
		world.org, nil, domain.ActionRunRefused, domain.TargetRun,
		"the environment's window has closed since the run was accepted", at,
	).InProject(world.scenario.ProjectID).On(run.ID)); err != nil {
		t.Fatalf("Record: %v", err)
	}

	entries, err := journal.InProject(ctx, world.org, world.scenario.ProjectID, 0)
	if err != nil {
		t.Fatalf("InProject: %v", err)
	}
	if len(entries) != 1 || entries[0].ActorID != nil {
		t.Fatalf("the entry reads as %+v", entries)
	}
}

// Nothing rewrites or removes an entry, and it is the database that says so
// rather than the code's good manners. Whoever wanted to hide an approval would
// edit it, not delete it — so both are taken away from the application role.
func TestTheApplicationRoleCannotRewriteTheJournal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	journal := postgres.NewAuditRepository(world.db)

	at := time.Now().UTC().Truncate(time.Microsecond)
	if err := journal.Record(ctx, domain.Allow(
		world.org, &world.user, domain.ActionTargetApproved, domain.TargetEnvironment, at,
	).InProject(world.scenario.ProjectID).On(world.env.ID)); err != nil {
		t.Fatalf("Record: %v", err)
	}

	err := world.db.InTenantTx(ctx, world.org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE audit_log SET action = 'something.else' WHERE org_id = $1`, world.org)
		return err
	})
	if err == nil {
		t.Fatal("the application role rewrote an audit entry")
	}

	err = world.db.InTenantTx(ctx, world.org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM audit_log WHERE org_id = $1`, world.org)
		return err
	})
	if err == nil {
		t.Fatal("the application role deleted an audit entry")
	}

	// And the entry is still there, which is the half that would go unnoticed
	// if the statements above failed for some other reason.
	entries, err := journal.InProject(ctx, world.org, world.scenario.ProjectID, 0)
	if err != nil {
		t.Fatalf("InProject: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("the journal holds %d entries", len(entries))
	}
}

// An entry outlives the person who caused it, and stops naming them.
//
// Two things at once, and both were broken before this ticket: the referential
// action blanks only actor_id now — on a composite key it blanked org_id too,
// which is NOT NULL, so removing anybody who had left an entry simply failed —
// and it still runs although the application role has lost UPDATE on this
// table, because the database performs it as the table's owner.
//
// A person removed from an organization must not be kept alive by a journal,
// and the journal must not forget that something happened.
func TestAnEntryOutlivesThePersonWhoCausedIt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	journal := postgres.NewAuditRepository(world.db)

	// Somebody who has done nothing else: the seeded user approved the
	// environment and triggered runs, and those references are another
	// ticket's problem.
	leaving := uuid.New()
	if err := world.db.InTenantTx(ctx, world.org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`INSERT INTO app_user (id, org_id, subject, email) VALUES ($1, $2, $3, $4)`,
			leaving, world.org, "subject-"+leaving.String(), leaving.String()+"@example.test")
		return err
	}); err != nil {
		t.Fatalf("add the person: %v", err)
	}

	at := time.Now().UTC().Truncate(time.Microsecond)
	if err := journal.Record(ctx, domain.Allow(
		world.org, &leaving, domain.ActionTargetApproved, domain.TargetEnvironment, at,
	).InProject(world.scenario.ProjectID).On(world.env.ID).
		With("host", "staging.example.test")); err != nil {
		t.Fatalf("Record: %v", err)
	}

	if err := world.db.InTenantTx(ctx, world.org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM app_user WHERE org_id = $1 AND id = $2`,
			world.org, leaving)
		return err
	}); err != nil {
		t.Fatalf("remove the person: %v", err)
	}

	entries, err := journal.InProject(ctx, world.org, world.scenario.ProjectID, 0)
	if err != nil {
		t.Fatalf("InProject: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("the entry went with the person: %+v", entries)
	}
	if entries[0].ActorID != nil {
		t.Fatalf("the entry still names %v", entries[0].ActorID)
	}
	// What happened is still on the record, which is the point of keeping it.
	if entries[0].Action != domain.ActionTargetApproved ||
		entries[0].Details["host"] != "staging.example.test" {
		t.Fatalf("the entry now reads as %+v", entries[0])
	}
}

// The promise behind writing some entries inside the transaction that performs
// the action: if the entry cannot be written, the action does not happen.
//
// Load is authorised with somebody's name on it, or it is not authorised. The
// entry here is made unwritable the way a broken one would be — it names an
// author who does not exist — and what the test asks is what survives.
func TestAnApprovalWhoseRecordCannotBeWrittenDoesNotHappen(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	environments := postgres.NewEnvironmentWriteRepository(world.db)

	// A target with its proof in, ready to be approved.
	proven := world.provenEnvironment(ctx, t)

	who := domain.Identity{UserID: world.user, OrgID: world.org}
	unwritable := domain.Allow(
		world.org, &[]uuid.UUID{uuid.New()}[0], // nobody
		domain.ActionTargetApproved, domain.TargetEnvironment, time.Now().UTC(),
	).InProject(world.scenario.ProjectID).On(proven)

	if _, err := environments.Approve(
		ctx, world.org, proven, who, time.Now().UTC(), "staging.example.test", unwritable,
	); err == nil {
		t.Fatal("the approval went through although its record could not be written")
	}

	// Nothing of it happened: not the approval, and not the host on the
	// allowlist that the approval writes in the same transaction.
	var status string
	var hosts int
	if err := world.db.InTenantTx(ctx, world.org, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx,
			`SELECT verification_status FROM environment WHERE org_id = $1 AND id = $2`,
			world.org, proven).Scan(&status); err != nil {
			return err
		}
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM project_allowed_host WHERE org_id = $1 AND host = $2`,
			world.org, "staging.example.test").Scan(&hosts)
	}); err != nil {
		t.Fatalf("read what survived: %v", err)
	}

	if status == string(domain.VerificationVerified) {
		t.Error("the target is verified although nobody is recorded as having approved it")
	}
	if hosts != 0 {
		t.Error("the host was allowed although nobody is recorded as having approved it")
	}
}
