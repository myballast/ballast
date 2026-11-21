//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/apitoken"
)

// Managing tokens is writing a token and its journal entry together, reading a
// project's tokens, and withdrawing one. What these tests hold in place:
//
//   - a token and its entry are one fact: if the entry cannot be written, the
//     token does not exist, and the same for a revocation;
//   - the ceiling is enforced in the transaction that writes, so that two
//     creations at once cannot both pass it;
//   - nothing crosses an organization, in either direction.
//
// Written before the code, which is why they do not compile yet.

var _ apitoken.Registry = (*postgres.APITokenRepository)(nil)

// who is the fixture's person and project in an organization: the rows the
// fixture wrote, and what a token needs to be created in them.
type who struct {
	user    uuid.UUID
	project uuid.UUID
}

func whoIn(ctx context.Context, t *testing.T, f fixture, org uuid.UUID) who {
	t.Helper()

	var w who
	err := f.db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, "SELECT id FROM app_user WHERE org_id = $1", org).Scan(&w.user); err != nil {
			return err
		}
		return tx.QueryRow(ctx, "SELECT id FROM project WHERE org_id = $1", org).Scan(&w.project)
	})
	if err != nil {
		t.Fatalf("read the fixture's person and project in %s: %v", org, err)
	}
	return w
}

func aNewToken(org uuid.UUID, w who, name string) (domain.APIToken, string) {
	now := time.Now().UTC().Truncate(time.Second)
	id := uuid.New()
	return domain.APIToken{
		ID:        id,
		OrgID:     org,
		ProjectID: w.project,
		Name:      name,
		Prefix:    "blt_" + id.String()[:8],
		CreatedBy: &w.user,
		ExpiresAt: now.Add(90 * 24 * time.Hour),
		CreatedAt: now,
	}, "hash-of-" + id.String()
}

func create(ctx context.Context, t *testing.T, repo *postgres.APITokenRepository, org uuid.UUID, w who, name string) (domain.APIToken, error) {
	t.Helper()

	token, hash := aNewToken(org, w, name)
	err := repo.Create(ctx, token, hash, domain.APITokenCreatedEntry(&w.user, token, token.CreatedAt))
	return token, err
}

func journal(ctx context.Context, t *testing.T, f fixture, org uuid.UUID, action domain.AuditAction) []string {
	t.Helper()

	var outcomes []string
	err := f.db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, "SELECT outcome FROM audit_log WHERE org_id = $1 AND action = $2", org, string(action))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var outcome string
			if err := rows.Scan(&outcome); err != nil {
				return err
			}
			outcomes = append(outcomes, outcome)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("read the journal: %v", err)
	}
	return outcomes
}

func liveTokens(ctx context.Context, t *testing.T, f fixture, org uuid.UUID) int {
	t.Helper()

	var n int
	err := f.db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM api_token WHERE org_id = $1 AND revoked_at IS NULL", org).Scan(&n)
	})
	if err != nil {
		t.Fatalf("count the live tokens: %v", err)
	}
	return n
}

// --- Create -------------------------------------------------------------------

func TestCreatingATokenWritesItAndItsEntryTogether(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	repo := postgres.NewAPITokenRepository(f.db)
	w := whoIn(ctx, t, f, f.orgA)

	token, err := create(ctx, t, repo, f.orgA, w, "github main")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, found, err := repo.ByID(ctx, f.orgA, token.ID)
	if err != nil || !found {
		t.Fatalf("ByID: found=%v err=%v", found, err)
	}
	if got.Name != "github main" || got.Prefix != token.Prefix || got.ProjectID != w.project {
		t.Errorf("stored %+v, want what was written", got)
	}
	if got.CreatedBy == nil || *got.CreatedBy != w.user {
		t.Errorf("created by %v, want %s", got.CreatedBy, w.user)
	}
	if !got.ExpiresAt.Equal(token.ExpiresAt) {
		t.Errorf("expires at %v, want %v", got.ExpiresAt, token.ExpiresAt)
	}

	if outcomes := journal(ctx, t, f, f.orgA, domain.ActionAPITokenCreated); len(outcomes) != 1 || outcomes[0] != "allowed" {
		t.Errorf("the journal holds %v for the creation, want one allowed entry", outcomes)
	}
}

// If the entry cannot be written, the token does not exist. An approval with no
// trace of who gave it is load authorized in nobody's name; a credential with no
// trace of who made it is the same thing for access.
func TestATokenWhoseEntryCannotBeWrittenIsNotCreated(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	repo := postgres.NewAPITokenRepository(f.db)
	w := whoIn(ctx, t, f, f.orgA)

	token, hash := aNewToken(f.orgA, w, "doomed")
	entry := domain.APITokenCreatedEntry(&w.user, token, token.CreatedAt)
	// The schema accepts two outcomes. This is neither, so the insert fails
	// after the token's own insert has already gone through.
	entry.Outcome = "neither"

	if err := repo.Create(ctx, token, hash, entry); err == nil {
		t.Fatal("Create succeeded although its entry was refused")
	}

	if _, found, _ := repo.ByID(ctx, f.orgA, token.ID); found {
		t.Error("the token exists although its entry could not be written")
	}
}

// A project holds at most twenty tokens that have not been revoked. The fixture
// put one there already.
func TestAProjectHoldsAtMostTwentyLiveTokens(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	repo := postgres.NewAPITokenRepository(f.db)
	w := whoIn(ctx, t, f, f.orgA)

	for i := liveTokens(ctx, t, f, f.orgA); i < domain.MaxAPITokensPerProject; i++ {
		if _, err := create(ctx, t, repo, f.orgA, w, "filler"); err != nil {
			t.Fatalf("token %d: %v", i+1, err)
		}
	}
	if n := liveTokens(ctx, t, f, f.orgA); n != domain.MaxAPITokensPerProject {
		t.Fatalf("%d live tokens, want %d; the rest would prove nothing", n, domain.MaxAPITokensPerProject)
	}

	token, err := create(ctx, t, repo, f.orgA, w, "one too many")
	if !errors.Is(err, domain.ErrTooManyAPITokens) {
		t.Fatalf("the 21st token: error %v, want ErrTooManyAPITokens", err)
	}
	if _, found, _ := repo.ByID(ctx, f.orgA, token.ID); found {
		t.Error("the refused token was written")
	}
	// Refusing leaves nothing in the journal either: nothing happened.
	if outcomes := journal(ctx, t, f, f.orgA, domain.ActionAPITokenCreated); len(outcomes) != domain.MaxAPITokensPerProject-1 {
		t.Errorf("%d creation entries, want one per token that exists (the fixture's has none)", len(outcomes))
	}

	// Revoking one frees a place: the ceiling counts what has not been revoked.
	list, err := repo.List(ctx, f.orgA, w.project)
	if err != nil || len(list) == 0 {
		t.Fatalf("List: %d tokens, %v", len(list), err)
	}
	if err := repo.Revoke(ctx, list[0].APIToken, time.Now().UTC(), domain.APITokenRevokedEntry(&w.user, list[0].APIToken, time.Now().UTC())); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := create(ctx, t, repo, f.orgA, w, "after a revocation"); err != nil {
		t.Errorf("a revoked token still counted against the ceiling: %v", err)
	}
}

// Two creations at once for the last places must not both pass. The ceiling is
// checked in the transaction that writes, behind a lock on the project: a count
// taken outside it would let every one of them see room.
func TestTwoCreationsAtOnceCannotBothTakeTheLastPlace(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	repo := postgres.NewAPITokenRepository(f.db)
	w := whoIn(ctx, t, f, f.orgA)

	// Leave exactly two places.
	for i := liveTokens(ctx, t, f, f.orgA); i < domain.MaxAPITokensPerProject-2; i++ {
		if _, err := create(ctx, t, repo, f.orgA, w, "filler"); err != nil {
			t.Fatalf("filler %d: %v", i, err)
		}
	}

	const racers = 8
	start := make(chan struct{})
	results := make(chan error, racers)
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := create(ctx, t, repo, f.orgA, w, "racer")
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	var created, refused int
	for err := range results {
		switch {
		case err == nil:
			created++
		case errors.Is(err, domain.ErrTooManyAPITokens):
			refused++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if created != 2 || refused != racers-2 {
		t.Errorf("%d created and %d refused, want 2 and %d", created, refused, racers-2)
	}
	if n := liveTokens(ctx, t, f, f.orgA); n != domain.MaxAPITokensPerProject {
		t.Errorf("%d live tokens, want exactly %d", n, domain.MaxAPITokensPerProject)
	}
}

// A project of another organization is not there to be written into: not by the
// policy and not by the composite key. It is a not found, not a leak.
func TestACreationUnderAnotherOrganizationsProjectIsNotFound(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	repo := postgres.NewAPITokenRepository(f.db)
	a, b := whoIn(ctx, t, f, f.orgA), whoIn(ctx, t, f, f.orgB)

	crossed := who{user: a.user, project: b.project}
	token, err := create(ctx, t, repo, f.orgA, crossed, "crossed")

	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("error %v, want ErrNotFound", err)
	}
	if _, found, _ := repo.ByID(ctx, f.orgA, token.ID); found {
		t.Error("a token was written into another organization's project")
	}
}

// --- List ---------------------------------------------------------------------

func TestTheListHoldsTheProjectsTokensWithWhoMadeThem(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	repo := postgres.NewAPITokenRepository(f.db)
	w := whoIn(ctx, t, f, f.orgA)

	if err := f.db.InTenantTx(ctx, f.orgA, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE app_user SET name = 'Ada Lovelace' WHERE id = $1", w.user)
		return err
	}); err != nil {
		t.Fatalf("name the author: %v", err)
	}

	created, err := create(ctx, t, repo, f.orgA, w, "github main")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Revoke(ctx, created, time.Now().UTC(), domain.APITokenRevokedEntry(&w.user, created, time.Now().UTC())); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	list, err := repo.List(ctx, f.orgA, w.project)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	// The fixture's token and the one just made; the revoked one is still listed.
	if len(list) != 2 {
		t.Fatalf("%d tokens listed, want 2", len(list))
	}
	var seen bool
	for _, l := range list {
		if l.CreatedByName != "Ada Lovelace" {
			t.Errorf("token %q made by %q, want the author's name", l.Name, l.CreatedByName)
		}
		if l.ID == created.ID {
			seen = true
			if l.RevokedAt == nil {
				t.Error("the revoked token does not say so")
			}
		}
	}
	if !seen {
		t.Error("the revoked token is not listed")
	}
	// Newest first.
	if !list[0].CreatedAt.After(list[1].CreatedAt) && !list[0].CreatedAt.Equal(list[1].CreatedAt) {
		t.Errorf("not newest first: %v then %v", list[0].CreatedAt, list[1].CreatedAt)
	}
}

// A token whose author is not known — the person left, and the foreign key
// cleared created_by — has no name to show, and does not stop being listed.
func TestATokenWithNoKnownAuthorIsStillListed(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	repo := postgres.NewAPITokenRepository(f.db)
	w := whoIn(ctx, t, f, f.orgA)

	token, hash := aNewToken(f.orgA, w, "orphan")
	token.CreatedBy = nil
	if err := repo.Create(ctx, token, hash, domain.APITokenCreatedEntry(&w.user, token, token.CreatedAt)); err != nil {
		t.Fatalf("Create: %v", err)
	}

	list, err := repo.List(ctx, f.orgA, w.project)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, l := range list {
		if l.ID == token.ID {
			if l.CreatedBy != nil || l.CreatedByName != "" {
				t.Errorf("made by %v (%q), want nobody", l.CreatedBy, l.CreatedByName)
			}
			return
		}
	}
	t.Error("a token with no known author is not listed")
}

func TestTheListNeverCrossesAnOrganization(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	repo := postgres.NewAPITokenRepository(f.db)
	a, b := whoIn(ctx, t, f, f.orgA), whoIn(ctx, t, f, f.orgB)

	// Each organization sees its own, and one.
	for org, w := range map[uuid.UUID]who{f.orgA: a, f.orgB: b} {
		list, err := repo.List(ctx, org, w.project)
		if err != nil || len(list) != 1 {
			t.Fatalf("own list: %d tokens, %v, want 1", len(list), err)
		}
	}

	// Asking under one organization for the other's project returns nothing, and
	// not an error: nobody learns that the project exists.
	if list, err := repo.List(ctx, f.orgA, b.project); err != nil || len(list) != 0 {
		t.Errorf("A reading B's project: %d tokens, %v, want none", len(list), err)
	}
	if list, err := repo.List(ctx, f.orgB, a.project); err != nil || len(list) != 0 {
		t.Errorf("B reading A's project: %d tokens, %v, want none", len(list), err)
	}
}

func TestAnEmptyProjectListsAsEmptyAndNotNil(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	repo := postgres.NewAPITokenRepository(f.db)

	list, err := repo.List(ctx, f.orgA, uuid.New())
	if err != nil || list == nil || len(list) != 0 {
		t.Errorf("list %v, err %v, want an empty slice", list, err)
	}
}

// --- Revoke -------------------------------------------------------------------

func TestRevokingATokenWritesTheDateAndTheEntryTogether(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	repo := postgres.NewAPITokenRepository(f.db)
	w := whoIn(ctx, t, f, f.orgA)

	token, err := create(ctx, t, repo, f.orgA, w, "to revoke")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	at := time.Now().UTC().Truncate(time.Second)
	if err := repo.Revoke(ctx, token, at, domain.APITokenRevokedEntry(&w.user, token, at)); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	got, found, err := repo.ByID(ctx, f.orgA, token.ID)
	if err != nil || !found {
		t.Fatalf("ByID: found=%v err=%v", found, err)
	}
	if got.RevokedAt == nil || !got.RevokedAt.Equal(at) {
		t.Errorf("revoked at %v, want %v", got.RevokedAt, at)
	}
	if outcomes := journal(ctx, t, f, f.orgA, domain.ActionAPITokenRevoked); len(outcomes) != 1 || outcomes[0] != "allowed" {
		t.Errorf("the journal holds %v for the revocation, want one allowed entry", outcomes)
	}

	// And the lookup a request goes through says so too, which is what makes the
	// token stop working on the next one.
	found2, ok, err := repo.ByHash(ctx, "hash-of-"+token.ID.String())
	if err != nil || !ok || found2.RevokedAt == nil {
		t.Errorf("the lookup says %+v (found=%v, err=%v), want a revoked token", found2, ok, err)
	}
}

func TestATokenWhoseRevocationEntryCannotBeWrittenStaysLive(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	repo := postgres.NewAPITokenRepository(f.db)
	w := whoIn(ctx, t, f, f.orgA)

	token, err := create(ctx, t, repo, f.orgA, w, "stays")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	entry := domain.APITokenRevokedEntry(&w.user, token, time.Now().UTC())
	entry.Outcome = "neither"
	if err := repo.Revoke(ctx, token, time.Now().UTC(), entry); err == nil {
		t.Fatal("Revoke succeeded although its entry was refused")
	}

	got, _, _ := repo.ByID(ctx, f.orgA, token.ID)
	if got.RevokedAt != nil {
		t.Error("the token was revoked although the entry that says so could not be written")
	}
}

// Revoking twice is a not found the second time, and writes no second entry: the
// journal says a token was revoked once because it was.
func TestRevokingTwiceIsNotFoundAndWritesNoSecondEntry(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	repo := postgres.NewAPITokenRepository(f.db)
	w := whoIn(ctx, t, f, f.orgA)

	token, err := create(ctx, t, repo, f.orgA, w, "once")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	at := time.Now().UTC()
	if err := repo.Revoke(ctx, token, at, domain.APITokenRevokedEntry(&w.user, token, at)); err != nil {
		t.Fatalf("first Revoke: %v", err)
	}

	err = repo.Revoke(ctx, token, at, domain.APITokenRevokedEntry(&w.user, token, at))
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("second Revoke: error %v, want ErrNotFound", err)
	}
	if outcomes := journal(ctx, t, f, f.orgA, domain.ActionAPITokenRevoked); len(outcomes) != 1 {
		t.Errorf("%d revocation entries, want 1", len(outcomes))
	}
}

// A revocation reaches a token of its own organization and its own project, and
// nothing else, whatever the token passed in claims to be.
func TestARevocationNeverReachesAnotherOrganizationOrProject(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	repo := postgres.NewAPITokenRepository(f.db)
	a, b := whoIn(ctx, t, f, f.orgA), whoIn(ctx, t, f, f.orgB)

	victim, err := create(ctx, t, repo, f.orgB, b, "victim")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// A's identity, B's token id: the org in the token is A's, so the row is not
	// there.
	forged := victim
	forged.OrgID = f.orgA
	forged.ProjectID = a.project
	at := time.Now().UTC()
	if err := repo.Revoke(ctx, forged, at, domain.APITokenRevokedEntry(&a.user, forged, at)); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("error %v, want ErrNotFound", err)
	}

	// The right organization with the wrong project is as absent.
	wrongProject := victim
	wrongProject.ProjectID = uuid.New()
	if err := repo.Revoke(ctx, wrongProject, at, domain.APITokenRevokedEntry(&b.user, wrongProject, at)); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a wrong project: error %v, want ErrNotFound", err)
	}

	got, _, _ := repo.ByID(ctx, f.orgB, victim.ID)
	if got.RevokedAt != nil {
		t.Error("a token was revoked from outside its organization or project")
	}
}
