//go:build integration

package postgres_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres"
	"github.com/youssefmahersi/ballast/internal/usecase/apitoken"
	"github.com/youssefmahersi/ballast/internal/usecase/authz"
)

// The repository is the two halves of one table. ByHash is the cross-tenant
// half, through api_token_lookup, and answers before anybody knows an
// organization. ByID and Touch are the ordinary half, inside InTenantTx, and
// answer only for the organization they are asked under.
//
// Written before the code, which is why they do not compile yet.

var (
	_ apitoken.Repository = (*postgres.APITokenRepository)(nil)
	_ authz.Tokens        = (*postgres.APITokenRepository)(nil)
)

func TestTheRepositoryFindsATokenByHashWithoutAnOrganization(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	repo := postgres.NewAPITokenRepository(f.db)

	found, ok, err := repo.ByHash(ctx, "alpha-api-token-hash")
	if err != nil || !ok {
		t.Fatalf("ByHash: found=%v err=%v", ok, err)
	}
	if found.OrgID != f.orgA {
		t.Errorf("resolved to organization %s, want %s", found.OrgID, f.orgA)
	}
	if found.TokenID == uuid.Nil || found.ProjectID == uuid.Nil {
		t.Errorf("the lookup returned no token or project: %+v", found)
	}
	if found.RevokedAt != nil || !found.ExpiresAt.After(time.Now()) {
		t.Errorf("a fresh token reads as revoked (%v) or expired (%v)", found.RevokedAt, found.ExpiresAt)
	}

	if _, ok, err := repo.ByHash(ctx, "never-issued"); err != nil || ok {
		t.Errorf("an unknown hash: found=%v err=%v, want not found and no error", ok, err)
	}
}

func TestTheRepositoryReadsATokenOnlyUnderItsOwnOrganization(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	repo := postgres.NewAPITokenRepository(f.db)

	found, ok, err := repo.ByHash(ctx, "alpha-api-token-hash")
	if err != nil || !ok {
		t.Fatalf("ByHash: found=%v err=%v", ok, err)
	}

	token, ok, err := repo.ByID(ctx, f.orgA, found.TokenID)
	if err != nil || !ok {
		t.Fatalf("ByID under its own organization: found=%v err=%v", ok, err)
	}
	if token.ID != found.TokenID || token.OrgID != f.orgA || token.ProjectID != found.ProjectID {
		t.Errorf("read %+v, want token %s of org %s project %s", token, found.TokenID, f.orgA, found.ProjectID)
	}
	if token.Name != "ci" || token.Prefix != "blt_seed0001" {
		t.Errorf("read name %q prefix %q, want what was written", token.Name, token.Prefix)
	}

	// Under another organization the row is not there. This is row-level
	// security, not a filter somebody wrote: with RLS off on the table it fails.
	if _, ok, err := repo.ByID(ctx, f.orgB, found.TokenID); err != nil || ok {
		t.Errorf("ByID under another organization: found=%v err=%v, want not found", ok, err)
	}
}

// Recording a use is information for whoever reads the list, and a write on every
// request would be the heaviest thing a busy pipeline did. At most one a minute.
func TestRecordingAUseIsThrottledToOnceAMinute(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	repo := postgres.NewAPITokenRepository(f.db)

	found, _, err := repo.ByHash(ctx, "alpha-api-token-hash")
	if err != nil {
		t.Fatalf("ByHash: %v", err)
	}
	read := func() *time.Time {
		token, ok, err := repo.ByID(ctx, f.orgA, found.TokenID)
		if err != nil || !ok {
			t.Fatalf("ByID: found=%v err=%v", ok, err)
		}
		return token.LastUsedAt
	}

	if read() != nil {
		t.Fatal("a token that was never used says it was")
	}

	start := time.Now().UTC().Truncate(time.Second)
	if err := repo.Touch(ctx, f.orgA, found.TokenID, start); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	if got := read(); got == nil || !got.Equal(start) {
		t.Fatalf("first use recorded as %v, want %v", got, start)
	}

	// Thirty seconds later: nothing moves.
	if err := repo.Touch(ctx, f.orgA, found.TokenID, start.Add(30*time.Second)); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	if got := read(); got == nil || !got.Equal(start) {
		t.Errorf("a use within a minute moved last_used_at to %v", got)
	}

	// A little over a minute later: it does.
	later := start.Add(61 * time.Second)
	if err := repo.Touch(ctx, f.orgA, found.TokenID, later); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	if got := read(); got == nil || !got.Equal(later) {
		t.Errorf("a use after a minute recorded %v, want %v", got, later)
	}
}

// Touching a token under another organization changes nothing, and is not an
// error: nobody must learn from a failure that the id exists somewhere else.
func TestRecordingAUseUnderAnotherOrganizationChangesNothing(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	repo := postgres.NewAPITokenRepository(f.db)

	found, _, err := repo.ByHash(ctx, "alpha-api-token-hash")
	if err != nil {
		t.Fatalf("ByHash: %v", err)
	}

	if err := repo.Touch(ctx, f.orgB, found.TokenID, time.Now().UTC()); err != nil {
		t.Errorf("Touch under another organization failed: %v", err)
	}

	token, ok, err := repo.ByID(ctx, f.orgA, found.TokenID)
	if err != nil || !ok {
		t.Fatalf("ByID: found=%v err=%v", ok, err)
	}
	if token.LastUsedAt != nil {
		t.Errorf("another organization recorded a use of this token: %v", token.LastUsedAt)
	}
}
