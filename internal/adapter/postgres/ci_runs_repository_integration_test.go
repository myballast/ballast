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
	"github.com/youssefmahersi/ballast/internal/usecase/run"
)

// A run launched by a pipeline is written with its token and no person, read back
// the same way, and listed with the token's name; the journal entry about it
// names the token and not a person. What these tests hold in place is that the
// repository keeps the two kinds apart all the way to the row, so that nothing
// above it can turn a pipeline into nobody.
//
// Written before the code, which is why they do not pass yet.

func tokenActor(id uuid.UUID) domain.Actor { return domain.Actor{TokenID: &id} }

func TestARunLaunchedByATokenIsStoredWithTheTokenAndNoPerson(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	token := world.token(ctx, t, "github-main")

	created, err := world.runs.Create(ctx, domain.NewRun(
		world.org, world.scenario, world.env, domain.ProfileSmoke,
		domain.ProfileParams{TargetVUs: 1, PlateauS: 5}, nil,
	).By(tokenActor(token)))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	for name, got := range map[string]domain.Run{"what Create returns": created, "what ByID reads": mustByID(ctx, t, world, created.ID)} {
		if got.TriggeredByToken == nil || *got.TriggeredByToken != token {
			t.Errorf("%s: triggered by token %v, want %s", name, got.TriggeredByToken, token)
		}
		if got.TriggeredBy != nil {
			t.Errorf("%s: a run launched by a token names the person %v", name, got.TriggeredBy)
		}
	}
}

func mustByID(ctx context.Context, t *testing.T, world queueWorld, id uuid.UUID) domain.Run {
	t.Helper()

	found, err := world.runs.ByID(ctx, world.org, id)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	return found
}

// A person's run is what it always was.
func TestARunLaunchedByAPersonStillNamesThePersonAndNoToken(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)

	created := world.queue(ctx, t, world.env)

	got := mustByID(ctx, t, world, created.ID)
	if got.TriggeredBy == nil || *got.TriggeredBy != world.user || got.TriggeredByToken != nil {
		t.Errorf("triggered by %v / token %v, want the person alone", got.TriggeredBy, got.TriggeredByToken)
	}
}

// The list says who started each run, and a pipeline's run says so as a token's:
// a person with the same name must not be mistaken for it. A run whose token is
// gone — the project's deletion takes both, but the foreign key is for whatever
// else — has nobody to name, as one whose person left has not.
func TestTheListNamesWhoLaunchedEachRun(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	if err := world.db.InTenantTx(ctx, world.org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE app_user SET name = 'github-main' WHERE org_id = $1 AND id = $2`, world.org, world.user)
		return err
	}); err != nil {
		t.Fatalf("name the person like the token: %v", err)
	}
	token := world.token(ctx, t, "github-main")

	byPerson := world.queue(ctx, t, world.env)
	byToken, err := world.runs.Create(ctx, domain.NewRun(
		world.org, world.scenario, world.env, domain.ProfileSmoke,
		domain.ProfileParams{TargetVUs: 1, PlateauS: 5}, nil,
	).By(tokenActor(token)))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	byNobody, err := world.runs.Create(ctx, domain.NewRun(
		world.org, world.scenario, world.env, domain.ProfileSmoke,
		domain.ProfileParams{TargetVUs: 1, PlateauS: 5}, nil,
	))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	listed, err := world.runs.InProject(ctx, world.org, world.scenario.ProjectID, run.Filter{}, run.Window{}, 10)
	if err != nil {
		t.Fatalf("InProject: %v", err)
	}

	names := map[uuid.UUID]string{}
	for _, l := range listed {
		names[l.ID] = l.TriggeredByName
	}
	if names[byPerson.ID] != "github-main" {
		t.Errorf("the person's run says %q, want their name", names[byPerson.ID])
	}
	if names[byToken.ID] != "API token github-main" {
		t.Errorf("the pipeline's run says %q, want the token's name, said as a token's", names[byToken.ID])
	}
	if names[byNobody.ID] != "" {
		t.Errorf("a run nobody launched says %q", names[byNobody.ID])
	}
}

// --- The journal ---------------------------------------------------------------------

func entryAtNow(world queueWorld) domain.AuditEntry {
	return domain.Allow(world.org, nil, domain.ActionRunQueued, domain.TargetRun, time.Now().UTC().Truncate(time.Microsecond)).
		InProject(world.scenario.ProjectID).
		With("profile", "smoke")
}

func TestAnEntryDoneByATokenIsWrittenAndReadBackWithTheTokenAndNoPerson(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	journal := postgres.NewAuditRepository(world.db)
	token := world.token(ctx, t, "github-main")

	if err := journal.Record(ctx, entryAtNow(world).By(tokenActor(token))); err != nil {
		t.Fatalf("Record: %v", err)
	}

	entries, err := journal.InProject(ctx, world.org, world.scenario.ProjectID, 10)
	if err != nil || len(entries) != 1 {
		t.Fatalf("InProject: %d entries, %v", len(entries), err)
	}
	if entries[0].ActorTokenID == nil || *entries[0].ActorTokenID != token {
		t.Errorf("the entry names the token %v, want %s", entries[0].ActorTokenID, token)
	}
	if entries[0].ActorID != nil {
		t.Errorf("the entry names the person %v for what a token did", entries[0].ActorID)
	}
}

func TestAnEntryDoneByAPersonOrByNobodyIsReadBackAsSuch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	journal := postgres.NewAuditRepository(world.db)
	person := domain.Actor{UserID: &world.user}

	if err := journal.Record(ctx, entryAtNow(world).By(person).With("who", "person")); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := journal.Record(ctx, entryAtNow(world).With("who", "nobody")); err != nil {
		t.Fatalf("Record: %v", err)
	}

	entries, err := journal.InProject(ctx, world.org, world.scenario.ProjectID, 10)
	if err != nil || len(entries) != 2 {
		t.Fatalf("InProject: %d entries, %v", len(entries), err)
	}
	for _, e := range entries {
		switch e.Details["who"] {
		case "person":
			if e.ActorID == nil || *e.ActorID != world.user || e.ActorTokenID != nil {
				t.Errorf("a person's entry: person %v, token %v", e.ActorID, e.ActorTokenID)
			}
		case "nobody":
			// The one entry with no author at all is Ballast acting alone.
			if e.ActorID != nil || e.ActorTokenID != nil {
				t.Errorf("Ballast's own entry names %v and %v", e.ActorID, e.ActorTokenID)
			}
		}
	}
}

// An entry that names a person and a token is refused by the schema, and nothing
// is written: the journal is not where a guess about who acted is recorded.
func TestAnEntryNamingBothAPersonAndATokenIsNotWritten(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	journal := postgres.NewAuditRepository(world.db)
	token := world.token(ctx, t, "github-main")

	both := entryAtNow(world)
	both.ActorID, both.ActorTokenID = &world.user, &token

	if err := journal.Record(ctx, both); err == nil {
		t.Fatal("an entry naming both was written")
	}
	if entries, _ := journal.InProject(ctx, world.org, world.scenario.ProjectID, 10); len(entries) != 0 {
		t.Errorf("%d entries were left behind", len(entries))
	}
}
