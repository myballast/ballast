//go:build integration

package postgres_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// An API token is the credential a pipeline holds instead of a session. It is
// kept in a table like any other — row-level security keeps it inside its
// organization — and it has the one exception signing in and following an
// invitation have: a request that carries a token knows neither the
// organization nor the project it belongs to. That is precisely what the token
// says, and row-level security refuses a read that does not name one.
//
// api_token_lookup is the opening, and these tests pin down how narrow it is, in
// the way the auth_lookup and invitation_lookup tests pin down theirs. Each
// would pass against a function that leaked more, were it not checking for the
// leak. Its owner is a role of its own: the other two are each bounded to their
// own tables by a test, and widening one would be loosening it.
//
// Written before the migration, which is why they do not pass yet.

type tokenRow struct {
	name      string
	prefix    string
	hash      string
	expiresAt time.Time
}

func newToken(name string) tokenRow {
	return tokenRow{
		name:      name,
		prefix:    "blt_abcd1234",
		hash:      hashOf("blt_" + name + "-" + time.Now().Format(time.RFC3339Nano)),
		expiresAt: time.Now().Add(90 * 24 * time.Hour),
	}
}

// issueToken writes a token through the application role and its policies.
func issueToken(ctx context.Context, t *testing.T, conn *pgx.Conn, org, project string, in tokenRow) (string, error) {
	t.Helper()

	var id string
	err := asOrg(ctx, t, conn, org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO api_token (org_id, project_id, name, prefix, token_hash, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			org, project, in.name, in.prefix, in.hash, in.expiresAt).Scan(&id)
	})
	return id, err
}

func mustIssueToken(ctx context.Context, t *testing.T, conn *pgx.Conn, org, project string, in tokenRow) string {
	t.Helper()

	id, err := issueToken(ctx, t, conn, org, project, in)
	if err != nil {
		t.Fatalf("write an API token: %v", err)
	}
	return id
}

type tokenFound struct {
	tokenID   string
	orgID     string
	projectID string
	expiresAt time.Time
	revokedAt *time.Time
}

// lookupToken calls api_token_lookup the way a request does: on a connection
// that has announced no organization.
func lookupToken(ctx context.Context, t *testing.T, conn *pgx.Conn, hash string) []tokenFound {
	t.Helper()

	rows, err := conn.Query(ctx, `
		SELECT token_id, org_id, project_id, expires_at, revoked_at
		FROM api_token_lookup($1)`, hash)
	if err != nil {
		t.Fatalf("api_token_lookup: %v", err)
	}
	defer rows.Close()

	var found []tokenFound
	for rows.Next() {
		var f tokenFound
		if err := rows.Scan(&f.tokenID, &f.orgID, &f.projectID, &f.expiresAt, &f.revokedAt); err != nil {
			t.Fatalf("scan: %v", err)
		}
		found = append(found, f)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return found
}

// --- Row-level security ----------------------------------------------------

// The tokens of one organization are invisible to another. If row-level
// security were off on the table, the second check would be the one to fail:
// organization B would read what A wrote.
func TestATokenOfAnotherOrganizationIsInvisible(t *testing.T) {
	ctx, conn := connect(t)

	orgA := seedOrg(ctx, t, conn, "alpha")
	orgB := seedOrg(ctx, t, conn, "beta")
	mustIssueToken(ctx, t, conn, orgA, projectOf(ctx, t, conn, orgA), newToken("ci"))

	if err := asOrg(ctx, t, conn, orgA, func(tx pgx.Tx) error {
		if n := countRows(ctx, t, tx, "api_token"); n != 1 {
			t.Errorf("within its own org, api_token holds %d rows, want 1", n)
		}
		return nil
	}); err != nil {
		t.Fatalf("read as A: %v", err)
	}

	if err := asOrg(ctx, t, conn, orgB, func(tx pgx.Tx) error {
		if n := countRows(ctx, t, tx, "api_token"); n != 0 {
			t.Errorf("organization B reads %d of organization A's tokens", n)
		}
		return nil
	}); err != nil {
		t.Fatalf("read as B: %v", err)
	}
}

func TestNoTokenIsReadableWithoutAnOrganization(t *testing.T) {
	ctx, conn := connect(t)

	org := seedOrg(ctx, t, conn, "alpha")
	mustIssueToken(ctx, t, conn, org, projectOf(ctx, t, conn, org), newToken("ci"))

	var n int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM api_token").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("with no organization announced, api_token exposes %d rows", n)
	}
}

func TestATokenCannotBeWrittenIntoAnotherOrganization(t *testing.T) {
	ctx, conn := connect(t)

	orgA := seedOrg(ctx, t, conn, "alpha")
	orgB := seedOrg(ctx, t, conn, "beta")
	projectA := projectOf(ctx, t, conn, orgA)

	// Organization B writes a row that says it belongs to A.
	err := asOrg(ctx, t, conn, orgB, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO api_token (org_id, project_id, name, prefix, token_hash, expires_at)
			VALUES ($1, $2, 'forged', 'blt_forged000', $3, now() + interval '1 day')`,
			orgA, projectA, hashOf("forged"))
		return err
	})
	if err == nil {
		t.Fatal("organization B wrote a token into organization A")
	}
	if got := sqlState(err); got != "42501" {
		t.Errorf("the write was refused with %q (%v), want 42501, the policy's", got, err)
	}
}

func TestATokenOfAnotherOrganizationCannotBeChangedOrRemoved(t *testing.T) {
	ctx, conn := connect(t)

	orgA := seedOrg(ctx, t, conn, "alpha")
	orgB := seedOrg(ctx, t, conn, "beta")
	id := mustIssueToken(ctx, t, conn, orgA, projectOf(ctx, t, conn, orgA), newToken("ci"))

	if err := asOrg(ctx, t, conn, orgB, func(tx pgx.Tx) error {
		changed, err := tx.Exec(ctx, "UPDATE api_token SET revoked_at = now(), name = 'mine' WHERE id = $1", id)
		if err != nil {
			return err
		}
		if changed.RowsAffected() != 0 {
			t.Errorf("organization B changed %d of A's tokens", changed.RowsAffected())
		}
		removed, err := tx.Exec(ctx, "DELETE FROM api_token WHERE id = $1", id)
		if err != nil {
			return err
		}
		if removed.RowsAffected() != 0 {
			t.Errorf("organization B removed %d of A's tokens", removed.RowsAffected())
		}
		return nil
	}); err != nil {
		t.Fatalf("write as B: %v", err)
	}

	if err := asOrg(ctx, t, conn, orgA, func(tx pgx.Tx) error {
		var name string
		var revoked *time.Time
		if err := tx.QueryRow(ctx, "SELECT name, revoked_at FROM api_token WHERE id = $1", id).Scan(&name, &revoked); err != nil {
			return err
		}
		if name != "ci" || revoked != nil {
			t.Errorf("the token became %q, revoked at %v", name, revoked)
		}
		return nil
	}); err != nil {
		t.Fatalf("read as A: %v", err)
	}
}

// --- What the table itself refuses -------------------------------------------

// A token names a project of its own organization. The composite key is what
// says so: row-level security checks the row's org_id, and a token whose
// org_id is A and whose project belongs to B would pass it.
func TestATokenCannotNameAProjectOfAnotherOrganization(t *testing.T) {
	ctx, conn := connect(t)

	orgA := seedOrg(ctx, t, conn, "alpha")
	orgB := seedOrg(ctx, t, conn, "beta")
	projectB := projectOf(ctx, t, conn, orgB)

	_, err := issueToken(ctx, t, conn, orgA, projectB, newToken("crossed"))
	if sqlState(err) != "23503" {
		t.Errorf("a token naming another organization's project was accepted or failed oddly: %v", err)
	}
}

// The lookup finds a token by its hash alone, so two tokens with one hash, in
// two organizations, would make it answer for the wrong one.
func TestAnAPITokenHashIsUniqueAcrossOrganizations(t *testing.T) {
	ctx, conn := connect(t)

	orgA := seedOrg(ctx, t, conn, "alpha")
	orgB := seedOrg(ctx, t, conn, "beta")

	shared := newToken("ci")
	mustIssueToken(ctx, t, conn, orgA, projectOf(ctx, t, conn, orgA), shared)

	_, err := issueToken(ctx, t, conn, orgB, projectOf(ctx, t, conn, orgB), shared)
	if sqlState(err) != "23505" {
		t.Errorf("one hash was accepted in two organizations, or failed oddly: %v", err)
	}
}

// A token that never expires is a credential nobody remembers having issued. The
// column is NOT NULL: the default lifetime is applied by the code, and the
// database refuses the row that was written without one.
func TestATokenAlwaysExpires(t *testing.T) {
	ctx, conn := connect(t)

	org := seedOrg(ctx, t, conn, "alpha")
	project := projectOf(ctx, t, conn, org)

	err := asOrg(ctx, t, conn, org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO api_token (org_id, project_id, name, prefix, token_hash)
			VALUES ($1, $2, 'forever', 'blt_forever00', $3)`, org, project, hashOf("forever"))
		return err
	})
	if sqlState(err) != "23502" {
		t.Errorf("a token with no expiry was accepted or failed oddly: %v", err)
	}
}

func TestATokenHasANameOfReasonableLength(t *testing.T) {
	ctx, conn := connect(t)

	org := seedOrg(ctx, t, conn, "alpha")
	project := projectOf(ctx, t, conn, org)

	for _, name := range []string{"", "   ", strings.Repeat("a", 65)} {
		in := newToken("x")
		in.name = name
		if _, err := issueToken(ctx, t, conn, org, project, in); sqlState(err) != "23514" {
			t.Errorf("the name %q was accepted or failed oddly: %v", name, err)
		}
	}
	if _, err := issueToken(ctx, t, conn, org, project, newToken("github main")); err != nil {
		t.Errorf("an ordinary name was refused: %v", err)
	}
}

func TestDeletingAProjectTakesItsTokens(t *testing.T) {
	ctx, conn := connect(t)

	org := seedOrg(ctx, t, conn, "alpha")
	project := projectOf(ctx, t, conn, org)
	in := newToken("ci")
	mustIssueToken(ctx, t, conn, org, project, in)

	if err := asOrg(ctx, t, conn, org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "DELETE FROM project WHERE id = $1", project)
		return err
	}); err != nil {
		t.Fatalf("delete the project: %v", err)
	}

	if err := asOrg(ctx, t, conn, org, func(tx pgx.Tx) error {
		if n := countRows(ctx, t, tx, "api_token"); n != 0 {
			t.Errorf("%d tokens outlived their project", n)
		}
		return nil
	}); err != nil {
		t.Fatalf("count: %v", err)
	}

	// And a token of a project that is gone resolves to nothing.
	if got := lookupToken(ctx, t, conn, in.hash); len(got) != 0 {
		t.Errorf("the token of a deleted project still resolves: %+v", got)
	}
}

// A token outlives the person who created it — a pipeline must not stop because
// somebody left — and the foreign key clears created_by alone: clearing the
// composite key would try to empty org_id, which is NOT NULL.
func TestATokenSurvivesItsAuthor(t *testing.T) {
	ctx, conn := connect(t)

	org := seedOrg(ctx, t, conn, "alpha")
	project := projectOf(ctx, t, conn, org)
	author := seedUser(ctx, t, conn, org, "test|author", "author@example.test")
	in := newToken("ci")

	if err := asOrg(ctx, t, conn, org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO api_token (org_id, project_id, name, prefix, token_hash, expires_at, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			org, project, in.name, in.prefix, in.hash, in.expiresAt, author)
		return err
	}); err != nil {
		t.Fatalf("issue: %v", err)
	}

	if err := asOrg(ctx, t, conn, org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "DELETE FROM app_user WHERE id = $1", author)
		return err
	}); err != nil {
		t.Fatalf("delete the author: %v", err)
	}

	if err := asOrg(ctx, t, conn, org, func(tx pgx.Tx) error {
		var createdBy *string
		if err := tx.QueryRow(ctx, "SELECT created_by FROM api_token WHERE token_hash = $1", in.hash).Scan(&createdBy); err != nil {
			return err
		}
		if createdBy != nil {
			t.Errorf("created_by is %s after its author was deleted, want NULL", *createdBy)
		}
		return nil
	}); err != nil {
		t.Fatalf("the token did not survive its author: %v", err)
	}
}

// --- api_token_lookup ---------------------------------------------------------

// The reason the function exists: with no organization announced, a hash is
// resolved to the token it stands for, the organization and the project it
// belongs to.
func TestTokenLookupResolvesAHashWithoutAnOrganization(t *testing.T) {
	ctx, conn := connect(t)

	orgA := seedOrg(ctx, t, conn, "alpha")
	orgB := seedOrg(ctx, t, conn, "beta")
	projectA := projectOf(ctx, t, conn, orgA)
	in := newToken("ci")
	id := mustIssueToken(ctx, t, conn, orgA, projectA, in)
	mustIssueToken(ctx, t, conn, orgB, projectOf(ctx, t, conn, orgB), newToken("other"))

	got := lookupToken(ctx, t, conn, in.hash)

	if len(got) != 1 {
		t.Fatalf("api_token_lookup returned %d rows, want exactly 1: %+v", len(got), got)
	}
	if got[0].tokenID != id || got[0].orgID != orgA || got[0].projectID != projectA {
		t.Errorf("resolved to token %s of org %s project %s, want %s of %s %s",
			got[0].tokenID, got[0].orgID, got[0].projectID, id, orgA, projectA)
	}
	if got[0].revokedAt != nil {
		t.Errorf("a fresh token says it was revoked at %v", got[0].revokedAt)
	}
	if !got[0].expiresAt.Equal(in.expiresAt.Truncate(time.Microsecond)) {
		t.Errorf("expires_at is %v, want %v", got[0].expiresAt, in.expiresAt)
	}
}

// The function answers about a token whatever became of it, and says what became
// of it; deciding that a revoked or expired token is dead belongs to the caller,
// which has the clock, and a caller that cannot see the state cannot decide.
func TestTokenLookupSaysWhatBecameOfAToken(t *testing.T) {
	ctx, conn := connect(t)

	org := seedOrg(ctx, t, conn, "alpha")
	project := projectOf(ctx, t, conn, org)
	revoked := newToken("revoked")
	expired := newToken("expired")
	expired.expiresAt = time.Now().Add(-time.Hour)
	idRevoked := mustIssueToken(ctx, t, conn, org, project, revoked)
	mustIssueToken(ctx, t, conn, org, project, expired)

	if err := asOrg(ctx, t, conn, org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE api_token SET revoked_at = now() WHERE id = $1", idRevoked)
		return err
	}); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	if got := lookupToken(ctx, t, conn, revoked.hash); len(got) != 1 || got[0].revokedAt == nil {
		t.Errorf("a revoked token does not say it was revoked: %+v", got)
	}
	if got := lookupToken(ctx, t, conn, expired.hash); len(got) != 1 || !got[0].expiresAt.Before(time.Now()) {
		t.Errorf("an expired token does not say when it expired: %+v", got)
	}
}

func TestTokenLookupReturnsNothingForAnUnknownHash(t *testing.T) {
	ctx, conn := connect(t)

	org := seedOrg(ctx, t, conn, "alpha")
	mustIssueToken(ctx, t, conn, org, projectOf(ctx, t, conn, org), newToken("ci"))

	if got := lookupToken(ctx, t, conn, hashOf("never-issued")); len(got) != 0 {
		t.Errorf("an unknown hash resolved to %+v", got)
	}
}

// The hash is compared whole, exactly. A prefix, a pattern or a change of case
// must not match, or a crafted value could resolve to somebody else's token.
func TestTokenLookupMatchesTheWholeHashOnly(t *testing.T) {
	ctx, conn := connect(t)

	org := seedOrg(ctx, t, conn, "alpha")
	in := newToken("ci")
	mustIssueToken(ctx, t, conn, org, projectOf(ctx, t, conn, org), in)

	for _, hash := range []string{
		in.hash[:len(in.hash)-1],
		in.hash[:8],
		in.hash + " ",
		" " + in.hash,
		"%",
		in.hash[:8] + "%",
		"",
		upper(in.hash),
	} {
		if got := lookupToken(ctx, t, conn, hash); len(got) != 0 {
			t.Errorf("hash %q resolved to %+v", hash, got)
		}
	}
}

// What a request needs to know who is asking and what to check, and nothing a
// caller could use to learn more about a tenant: not the hash it already holds,
// not the name, not who created it, not the other tokens.
func TestTokenLookupReturnsOnlyWhatAuthenticationNeeds(t *testing.T) {
	ctx, conn := connect(t)

	rows, err := conn.Query(ctx, "SELECT * FROM api_token_lookup($1)", hashOf("nobody"))
	if err != nil {
		t.Fatalf("api_token_lookup: %v", err)
	}
	fields := rows.FieldDescriptions()
	rows.Close()

	var names []string
	for _, f := range fields {
		names = append(names, f.Name)
	}
	want := []string{"token_id", "org_id", "project_id", "expires_at", "revoked_at"}
	if len(names) != len(want) {
		t.Fatalf("api_token_lookup returns columns %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("column %d is %q, want %q (all: %v)", i, names[i], want[i], names)
		}
	}
}

// Calling the function must not leave the caller with any wider view than it
// had: row-level security still hides every row afterwards, in the same
// transaction.
func TestTokenLookupDoesNotOpenRowLevelSecurity(t *testing.T) {
	ctx, conn := connect(t)

	org := seedOrg(ctx, t, conn, "alpha")
	in := newToken("ci")
	mustIssueToken(ctx, t, conn, org, projectOf(ctx, t, conn, org), in)

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var n int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM api_token_lookup($1)", in.hash).Scan(&n); err != nil {
		t.Fatalf("api_token_lookup: %v", err)
	}
	if n != 1 {
		t.Fatalf("api_token_lookup found %d rows, want 1; the check below would prove nothing", n)
	}

	for _, table := range []string{"api_token", "app_user", "organization", "project", "membership"} {
		if n := countRows(ctx, t, tx, table); n != 0 {
			t.Errorf("after api_token_lookup, %s exposed %d rows without an organization", table, n)
		}
	}
}

// The function runs with its owner's rights, so the owner is what bounds it.
func TestTokenLookupOwnerCanReadNothingElse(t *testing.T) {
	ctx, conn := connect(t)

	var owner string
	var canLogin, bypassRLS bool
	err := conn.QueryRow(ctx, `
		SELECT r.rolname, r.rolcanlogin, r.rolbypassrls
		FROM pg_proc p
		JOIN pg_roles r ON r.oid = p.proowner
		WHERE p.proname = 'api_token_lookup'`).Scan(&owner, &canLogin, &bypassRLS)
	if err != nil {
		t.Fatalf("read the owner of api_token_lookup: %v", err)
	}

	if owner != "ballast_token" {
		t.Errorf("api_token_lookup is owned by %q, want the dedicated role ballast_token", owner)
	}
	if canLogin {
		t.Errorf("%s can log in; it must exist only to own api_token_lookup", owner)
	}
	if !bypassRLS {
		t.Errorf("%s lacks BYPASSRLS; api_token_lookup would see no row", owner)
	}

	violations(ctx, t, conn, "the api_token_lookup owner must read no table but api_token", `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public'
		  AND c.relkind = 'r'
		  AND c.relname <> 'api_token'
		  AND has_table_privilege('ballast_token', c.oid, 'SELECT')`)

	// Not the hash column's neighbours: name, prefix, who created it and when it
	// was last used are what a tenant knows about its own tokens, and nothing
	// the lookup returns.
	violations(ctx, t, conn, "the api_token_lookup owner must read only the columns the lookup uses", `
		SELECT a.attname
		FROM pg_attribute a
		WHERE a.attrelid = 'public.api_token'::regclass
		  AND a.attnum > 0
		  AND NOT a.attisdropped
		  AND a.attname NOT IN ('id', 'org_id', 'project_id', 'token_hash', 'expires_at', 'revoked_at')
		  AND has_column_privilege('ballast_token', a.attrelid, a.attnum, 'SELECT')`)

	violations(ctx, t, conn, "the api_token_lookup owner must not write anywhere", `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public'
		  AND c.relkind = 'r'
		  AND (has_table_privilege('ballast_token', c.oid, 'INSERT')
		    OR has_table_privilege('ballast_token', c.oid, 'UPDATE')
		    OR has_table_privilege('ballast_token', c.oid, 'DELETE'))`)
}

// A SECURITY DEFINER function that resolves names through the caller's
// search_path can be made to run the caller's own objects with the owner's
// rights. The path is pinned, and only the application role may call it.
func TestTokenLookupIsPinnedAndNotPublic(t *testing.T) {
	ctx, conn := connect(t)

	var securityDefiner, pinned, publicCanExecute, appCanExecute bool
	err := conn.QueryRow(ctx, `
		SELECT p.prosecdef,
		       coalesce(array_to_string(p.proconfig, ',') LIKE '%search_path=%', false),
		       EXISTS (SELECT 1 FROM aclexplode(coalesce(p.proacl, acldefault('f', p.proowner))) a
		               WHERE a.grantee = 0 AND a.privilege_type = 'EXECUTE'),
		       has_function_privilege('ballast_app', p.oid, 'EXECUTE')
		FROM pg_proc p
		WHERE p.proname = 'api_token_lookup'`).Scan(&securityDefiner, &pinned, &publicCanExecute, &appCanExecute)
	if err != nil {
		t.Fatalf("read api_token_lookup: %v", err)
	}

	if !securityDefiner {
		t.Error("api_token_lookup is not SECURITY DEFINER")
	}
	if !pinned {
		t.Error("api_token_lookup does not pin its search_path")
	}
	if publicCanExecute {
		t.Error("PUBLIC may execute api_token_lookup; only ballast_app should")
	}
	if !appCanExecute {
		t.Error("ballast_app may not execute api_token_lookup")
	}
}
