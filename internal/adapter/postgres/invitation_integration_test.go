//go:build integration

package postgres_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// An invitation is a promise made to somebody who is not a member yet, and it is
// kept in a table like any other: row-level security keeps it inside its
// organization, and nobody outside reads it.
//
// There is one exception, as there is for signing in. The person following an
// invitation link is signed in to an organization of their own, or to none; the
// organization that invited them is precisely what they do not know yet, and
// row-level security refuses a read that does not name one. invitation_lookup is
// the opening, and these tests pin down how narrow it is, in the way the
// auth_lookup tests pin down the other one. Each would pass against a function
// that leaked more, were it not checking for the leak.
//
// Written before the migration, which is why they do not pass yet.

// hashOf is how a token is kept: the hex of its SHA-256. The token itself is
// never stored, so a copy of the table is not a set of working links.
func hashOf(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func projectOf(ctx context.Context, t *testing.T, conn *pgx.Conn, org string) string {
	t.Helper()

	var id string
	if err := asOrg(ctx, t, conn, org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT id FROM project WHERE org_id = $1", org).Scan(&id)
	}); err != nil {
		t.Fatalf("read the project of %s: %v", org, err)
	}
	return id
}

type invitationRow struct {
	email     string
	role      string
	hash      string
	expiresAt time.Time
}

func newInvitation(email, role string) invitationRow {
	return invitationRow{
		email:     email,
		role:      role,
		hash:      hashOf("token-" + uuid.NewString()),
		expiresAt: time.Now().Add(7 * 24 * time.Hour),
	}
}

// invite writes an invitation through the application role and its policies.
func invite(ctx context.Context, t *testing.T, conn *pgx.Conn, org, project string, in invitationRow) (string, error) {
	t.Helper()

	var id string
	err := asOrg(ctx, t, conn, org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO invitation (org_id, project_id, email, role, token_hash, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			org, project, in.email, in.role, in.hash, in.expiresAt).Scan(&id)
	})
	return id, err
}

func mustInvite(ctx context.Context, t *testing.T, conn *pgx.Conn, org, project string, in invitationRow) string {
	t.Helper()

	id, err := invite(ctx, t, conn, org, project, in)
	if err != nil {
		t.Fatalf("write an invitation: %v", err)
	}
	return id
}

func sqlState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

type lookedUp struct {
	invitationID string
	orgID        string
	projectID    string
	email        string
	role         string
	expiresAt    time.Time
	acceptedAt   *time.Time
	revokedAt    *time.Time
	projectName  string
	orgName      string
}

// lookupInvitation calls invitation_lookup the way an acceptance does: on a
// connection that has announced no organization.
func lookupInvitation(ctx context.Context, t *testing.T, conn *pgx.Conn, hash string) []lookedUp {
	t.Helper()

	rows, err := conn.Query(ctx, `
		SELECT invitation_id, org_id, project_id, email, role, expires_at,
		       accepted_at, revoked_at, project_name, org_name
		FROM invitation_lookup($1)`, hash)
	if err != nil {
		t.Fatalf("invitation_lookup: %v", err)
	}
	defer rows.Close()

	var found []lookedUp
	for rows.Next() {
		var l lookedUp
		if err := rows.Scan(&l.invitationID, &l.orgID, &l.projectID, &l.email, &l.role, &l.expiresAt,
			&l.acceptedAt, &l.revokedAt, &l.projectName, &l.orgName); err != nil {
			t.Fatalf("scan: %v", err)
		}
		found = append(found, l)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return found
}

// --- Row-level security ----------------------------------------------------

// The invitations of one organization are invisible to another, and cannot be
// written into it. If row-level security were off on the table, the first check
// would be the one to fail: organization B would read what A wrote.
func TestAnInvitationOfAnotherOrganizationIsInvisible(t *testing.T) {
	ctx, conn := connect(t)

	orgA := seedOrg(ctx, t, conn, "alpha")
	orgB := seedOrg(ctx, t, conn, "beta")
	projectA := projectOf(ctx, t, conn, orgA)
	mustInvite(ctx, t, conn, orgA, projectA, newInvitation("ada@example.test", "runner"))

	// The row is there for its own organization; otherwise the rest proves
	// nothing.
	if err := asOrg(ctx, t, conn, orgA, func(tx pgx.Tx) error {
		if n := countRows(ctx, t, tx, "invitation"); n != 1 {
			t.Errorf("within its own org, invitation holds %d rows, want 1", n)
		}
		return nil
	}); err != nil {
		t.Fatalf("read as A: %v", err)
	}

	if err := asOrg(ctx, t, conn, orgB, func(tx pgx.Tx) error {
		if n := countRows(ctx, t, tx, "invitation"); n != 0 {
			t.Errorf("organization B reads %d of organization A's invitations", n)
		}
		return nil
	}); err != nil {
		t.Fatalf("read as B: %v", err)
	}
}

func TestNoInvitationIsReadableWithoutAnOrganization(t *testing.T) {
	ctx, conn := connect(t)

	org := seedOrg(ctx, t, conn, "alpha")
	mustInvite(ctx, t, conn, org, projectOf(ctx, t, conn, org), newInvitation("ada@example.test", "runner"))

	var n int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM invitation").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("with no organization announced, invitation exposes %d rows", n)
	}
}

func TestAnInvitationCannotBeWrittenIntoAnotherOrganization(t *testing.T) {
	ctx, conn := connect(t)

	orgA := seedOrg(ctx, t, conn, "alpha")
	orgB := seedOrg(ctx, t, conn, "beta")
	projectA := projectOf(ctx, t, conn, orgA)

	// Organization B writes a row that says it belongs to A.
	err := asOrg(ctx, t, conn, orgB, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO invitation (org_id, project_id, email, role, token_hash, expires_at)
			VALUES ($1, $2, 'ada@example.test', 'admin', $3, now() + interval '1 day')`,
			orgA, projectA, hashOf("forged"))
		return err
	})
	if err == nil {
		t.Fatal("organization B wrote an invitation into organization A")
	}
	if got := sqlState(err); got != "42501" {
		t.Errorf("the write was refused with %q (%v), want 42501, the policy's", got, err)
	}
}

func TestAnInvitationOfAnotherOrganizationCannotBeChangedOrRemoved(t *testing.T) {
	ctx, conn := connect(t)

	orgA := seedOrg(ctx, t, conn, "alpha")
	orgB := seedOrg(ctx, t, conn, "beta")
	id := mustInvite(ctx, t, conn, orgA, projectOf(ctx, t, conn, orgA), newInvitation("ada@example.test", "runner"))

	if err := asOrg(ctx, t, conn, orgB, func(tx pgx.Tx) error {
		changed, err := tx.Exec(ctx, "UPDATE invitation SET role = 'admin' WHERE id = $1", id)
		if err != nil {
			return err
		}
		if changed.RowsAffected() != 0 {
			t.Errorf("organization B changed %d of A's invitations", changed.RowsAffected())
		}
		removed, err := tx.Exec(ctx, "DELETE FROM invitation WHERE id = $1", id)
		if err != nil {
			return err
		}
		if removed.RowsAffected() != 0 {
			t.Errorf("organization B removed %d of A's invitations", removed.RowsAffected())
		}
		return nil
	}); err != nil {
		t.Fatalf("write as B: %v", err)
	}

	if err := asOrg(ctx, t, conn, orgA, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT role FROM invitation WHERE id = $1", id).Scan(&role); err != nil {
			return err
		}
		if role != "runner" {
			t.Errorf("the invitation's role became %q", role)
		}
		return nil
	}); err != nil {
		t.Fatalf("read as A: %v", err)
	}
}

// --- What the table itself refuses -------------------------------------------

func TestOnlyOnePendingInvitationPerEmailInAProject(t *testing.T) {
	ctx, conn := connect(t)

	org := seedOrg(ctx, t, conn, "alpha")
	project := projectOf(ctx, t, conn, org)
	first := mustInvite(ctx, t, conn, org, project, newInvitation("ada@example.test", "runner"))

	// Two pending invitations for one person would be two links that both work,
	// and a revocation that leaves one of them open.
	_, err := invite(ctx, t, conn, org, project, newInvitation("ada@example.test", "admin"))
	if sqlState(err) != "23505" {
		t.Fatalf("a second pending invitation was accepted or failed oddly: %v", err)
	}

	// Once the first is revoked, the person can be invited again.
	if err := asOrg(ctx, t, conn, org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE invitation SET revoked_at = now() WHERE id = $1", first)
		return err
	}); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := invite(ctx, t, conn, org, project, newInvitation("ada@example.test", "maintainer")); err != nil {
		t.Errorf("a person whose invitation was revoked cannot be invited again: %v", err)
	}

	// Another project, another person: nothing in the way.
	if _, err := invite(ctx, t, conn, org, project, newInvitation("grace@example.test", "viewer")); err != nil {
		t.Errorf("a different email was refused: %v", err)
	}
}

func TestAnEmailIsKeptInLowerCase(t *testing.T) {
	ctx, conn := connect(t)

	org := seedOrg(ctx, t, conn, "alpha")

	_, err := invite(ctx, t, conn, org, projectOf(ctx, t, conn, org), newInvitation("Ada@Example.test", "runner"))
	if sqlState(err) != "23514" {
		t.Errorf("a mixed-case email was accepted or failed oddly (%v): the same person could be invited twice", err)
	}
}

func TestAnInvitationNamesOneOfTheFourRoles(t *testing.T) {
	ctx, conn := connect(t)

	org := seedOrg(ctx, t, conn, "alpha")
	project := projectOf(ctx, t, conn, org)

	for _, role := range []string{"viewer", "runner", "maintainer", "admin"} {
		if _, err := invite(ctx, t, conn, org, project, newInvitation(role+"@example.test", role)); err != nil {
			t.Errorf("the role %q was refused: %v", role, err)
		}
	}
	for _, role := range []string{"owner", "root", "", "Admin"} {
		_, err := invite(ctx, t, conn, org, project, newInvitation("other-"+role+"@example.test", role))
		if sqlState(err) != "23514" {
			t.Errorf("the role %q was accepted or failed oddly: %v", role, err)
		}
	}
}

// The lookup finds an invitation by its hash alone, so two invitations with one
// hash, in two organizations, would make it answer for the wrong one.
func TestATokenHashIsUniqueAcrossOrganizations(t *testing.T) {
	ctx, conn := connect(t)

	orgA := seedOrg(ctx, t, conn, "alpha")
	orgB := seedOrg(ctx, t, conn, "beta")

	shared := newInvitation("ada@example.test", "runner")
	mustInvite(ctx, t, conn, orgA, projectOf(ctx, t, conn, orgA), shared)

	_, err := invite(ctx, t, conn, orgB, projectOf(ctx, t, conn, orgB), shared)
	if sqlState(err) != "23505" {
		t.Errorf("one hash was accepted in two organizations, or failed oddly: %v", err)
	}
}

func TestDeletingAProjectTakesItsInvitations(t *testing.T) {
	ctx, conn := connect(t)

	org := seedOrg(ctx, t, conn, "alpha")
	project := projectOf(ctx, t, conn, org)
	mustInvite(ctx, t, conn, org, project, newInvitation("ada@example.test", "runner"))

	if err := asOrg(ctx, t, conn, org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "DELETE FROM project WHERE id = $1", project)
		return err
	}); err != nil {
		t.Fatalf("delete the project: %v", err)
	}

	if err := asOrg(ctx, t, conn, org, func(tx pgx.Tx) error {
		if n := countRows(ctx, t, tx, "invitation"); n != 0 {
			t.Errorf("%d invitations outlived their project", n)
		}
		return nil
	}); err != nil {
		t.Fatalf("count: %v", err)
	}
}

// --- invitation_lookup ---------------------------------------------------------

// The reason the function exists: with no organization announced, a hash is
// resolved to the invitation it stands for, and to what the person is being
// invited to, in words.
func TestInvitationLookupResolvesAHashWithoutAnOrganization(t *testing.T) {
	ctx, conn := connect(t)

	orgA := seedOrg(ctx, t, conn, "alpha")
	orgB := seedOrg(ctx, t, conn, "beta")
	projectA := projectOf(ctx, t, conn, orgA)
	in := newInvitation("ada@example.test", "maintainer")
	id := mustInvite(ctx, t, conn, orgA, projectA, in)
	mustInvite(ctx, t, conn, orgB, projectOf(ctx, t, conn, orgB), newInvitation("grace@example.test", "viewer"))

	got := lookupInvitation(ctx, t, conn, in.hash)

	if len(got) != 1 {
		t.Fatalf("invitation_lookup returned %d rows, want exactly 1: %+v", len(got), got)
	}
	l := got[0]
	if l.invitationID != id || l.orgID != orgA || l.projectID != projectA {
		t.Errorf("resolved to invitation %s of org %s project %s, want %s of %s %s",
			l.invitationID, l.orgID, l.projectID, id, orgA, projectA)
	}
	if l.email != "ada@example.test" || l.role != "maintainer" {
		t.Errorf("resolved to %q as %q", l.email, l.role)
	}
	if l.orgName != "alpha" || l.projectName != "alpha project" {
		t.Errorf("named %q and %q, want the organization and project it belongs to", l.orgName, l.projectName)
	}
	if l.acceptedAt != nil || l.revokedAt != nil {
		t.Errorf("a fresh invitation says it was accepted (%v) or revoked (%v)", l.acceptedAt, l.revokedAt)
	}
}

// The function answers about an invitation whatever became of it, and says what
// became of it; deciding that a used or revoked link is dead is the caller's,
// and a caller that cannot see the state cannot decide.
func TestInvitationLookupSaysWhatBecameOfAnInvitation(t *testing.T) {
	ctx, conn := connect(t)

	org := seedOrg(ctx, t, conn, "alpha")
	project := projectOf(ctx, t, conn, org)
	used := newInvitation("used@example.test", "runner")
	revoked := newInvitation("revoked@example.test", "runner")
	idUsed := mustInvite(ctx, t, conn, org, project, used)
	idRevoked := mustInvite(ctx, t, conn, org, project, revoked)

	if err := asOrg(ctx, t, conn, org, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "UPDATE invitation SET accepted_at = now() WHERE id = $1", idUsed); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "UPDATE invitation SET revoked_at = now() WHERE id = $1", idRevoked)
		return err
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	if got := lookupInvitation(ctx, t, conn, used.hash); len(got) != 1 || got[0].acceptedAt == nil {
		t.Errorf("a used invitation does not say it was used: %+v", got)
	}
	if got := lookupInvitation(ctx, t, conn, revoked.hash); len(got) != 1 || got[0].revokedAt == nil {
		t.Errorf("a revoked invitation does not say it was revoked: %+v", got)
	}
}

func TestInvitationLookupReturnsNothingForAnUnknownHash(t *testing.T) {
	ctx, conn := connect(t)

	org := seedOrg(ctx, t, conn, "alpha")
	mustInvite(ctx, t, conn, org, projectOf(ctx, t, conn, org), newInvitation("ada@example.test", "runner"))

	if got := lookupInvitation(ctx, t, conn, hashOf("never-issued")); len(got) != 0 {
		t.Errorf("an unknown hash resolved to %+v", got)
	}
}

// The hash is compared whole, exactly. A prefix, a pattern or a change of case
// must not match, or a crafted value could resolve to somebody else's
// invitation.
func TestInvitationLookupMatchesTheWholeHashOnly(t *testing.T) {
	ctx, conn := connect(t)

	org := seedOrg(ctx, t, conn, "alpha")
	in := newInvitation("ada@example.test", "runner")
	mustInvite(ctx, t, conn, org, projectOf(ctx, t, conn, org), in)

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
		if got := lookupInvitation(ctx, t, conn, hash); len(got) != 0 {
			t.Errorf("hash %q resolved to %+v", hash, got)
		}
	}
}

func upper(s string) string {
	out := []byte(s)
	for i, c := range out {
		if c >= 'a' && c <= 'f' {
			out[i] = c - 'a' + 'A'
		}
	}
	return string(out)
}

// What an acceptance page needs to say who is being invited to what, and
// nothing a caller could use to learn more about a tenant: not the hash it
// already holds, not who invited, not the other invitations.
func TestInvitationLookupReturnsOnlyWhatAnAcceptanceNeeds(t *testing.T) {
	ctx, conn := connect(t)

	rows, err := conn.Query(ctx, "SELECT * FROM invitation_lookup($1)", hashOf("nobody"))
	if err != nil {
		t.Fatalf("invitation_lookup: %v", err)
	}
	fields := rows.FieldDescriptions()
	rows.Close()

	var names []string
	for _, f := range fields {
		names = append(names, f.Name)
	}
	want := []string{
		"invitation_id", "org_id", "project_id", "email", "role", "expires_at",
		"accepted_at", "revoked_at", "project_name", "org_name",
	}
	if len(names) != len(want) {
		t.Fatalf("invitation_lookup returns columns %v, want %v", names, want)
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
func TestInvitationLookupDoesNotOpenRowLevelSecurity(t *testing.T) {
	ctx, conn := connect(t)

	org := seedOrg(ctx, t, conn, "alpha")
	in := newInvitation("ada@example.test", "runner")
	mustInvite(ctx, t, conn, org, projectOf(ctx, t, conn, org), in)

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var n int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM invitation_lookup($1)", in.hash).Scan(&n); err != nil {
		t.Fatalf("invitation_lookup: %v", err)
	}
	if n != 1 {
		t.Fatalf("invitation_lookup found %d rows, want 1; the check below would prove nothing", n)
	}

	for _, table := range []string{"invitation", "app_user", "organization", "project"} {
		if n := countRows(ctx, t, tx, table); n != 0 {
			t.Errorf("after invitation_lookup, %s exposed %d rows without an organization", table, n)
		}
	}
}

// The function runs with its owner's rights, so the owner is what bounds it. A
// role of its own, not the one that owns auth_lookup: that one is pinned to a
// single table by a test of its own, and widening it would be loosening it.
func TestInvitationLookupOwnerCanReadNothingElse(t *testing.T) {
	ctx, conn := connect(t)

	var owner string
	var canLogin, bypassRLS bool
	err := conn.QueryRow(ctx, `
		SELECT r.rolname, r.rolcanlogin, r.rolbypassrls
		FROM pg_proc p
		JOIN pg_roles r ON r.oid = p.proowner
		WHERE p.proname = 'invitation_lookup'`).Scan(&owner, &canLogin, &bypassRLS)
	if err != nil {
		t.Fatalf("read the owner of invitation_lookup: %v", err)
	}

	if owner != "ballast_invite" {
		t.Errorf("invitation_lookup is owned by %q, want the dedicated role ballast_invite", owner)
	}
	if canLogin {
		t.Errorf("%s can log in; it must exist only to own invitation_lookup", owner)
	}
	if !bypassRLS {
		t.Errorf("%s lacks BYPASSRLS; invitation_lookup would see no row", owner)
	}

	violations(ctx, t, conn, "the invitation_lookup owner must read no table but invitation, project and organization", `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public'
		  AND c.relkind = 'r'
		  AND c.relname NOT IN ('invitation', 'project', 'organization')
		  AND has_table_privilege('ballast_invite', c.oid, 'SELECT')`)

	violations(ctx, t, conn, "the invitation_lookup owner must read only the columns the lookup returns, of invitation", `
		SELECT a.attname
		FROM pg_attribute a
		WHERE a.attrelid = 'public.invitation'::regclass
		  AND a.attnum > 0
		  AND NOT a.attisdropped
		  AND a.attname NOT IN ('id', 'org_id', 'project_id', 'email', 'role', 'token_hash',
		                        'expires_at', 'accepted_at', 'revoked_at')
		  AND has_column_privilege('ballast_invite', a.attrelid, a.attnum, 'SELECT')`)

	violations(ctx, t, conn, "the invitation_lookup owner must read only id, org_id and name of project", `
		SELECT a.attname
		FROM pg_attribute a
		WHERE a.attrelid = 'public.project'::regclass
		  AND a.attnum > 0
		  AND NOT a.attisdropped
		  AND a.attname NOT IN ('id', 'org_id', 'name')
		  AND has_column_privilege('ballast_invite', a.attrelid, a.attnum, 'SELECT')`)

	violations(ctx, t, conn, "the invitation_lookup owner must read only id and name of organization", `
		SELECT a.attname
		FROM pg_attribute a
		WHERE a.attrelid = 'public.organization'::regclass
		  AND a.attnum > 0
		  AND NOT a.attisdropped
		  AND a.attname NOT IN ('id', 'name')
		  AND has_column_privilege('ballast_invite', a.attrelid, a.attnum, 'SELECT')`)

	violations(ctx, t, conn, "the invitation_lookup owner must not write anywhere", `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public'
		  AND c.relkind = 'r'
		  AND (has_table_privilege('ballast_invite', c.oid, 'INSERT')
		    OR has_table_privilege('ballast_invite', c.oid, 'UPDATE')
		    OR has_table_privilege('ballast_invite', c.oid, 'DELETE'))`)
}

// A SECURITY DEFINER function that resolves names through the caller's
// search_path can be made to run the caller's own objects with the owner's
// rights. The path is pinned, and only the application role may call it.
func TestInvitationLookupIsPinnedAndNotPublic(t *testing.T) {
	ctx, conn := connect(t)

	var securityDefiner, pinned, publicCanExecute, appCanExecute bool
	err := conn.QueryRow(ctx, `
		SELECT p.prosecdef,
		       coalesce(array_to_string(p.proconfig, ',') LIKE '%search_path=%', false),
		       EXISTS (SELECT 1 FROM aclexplode(coalesce(p.proacl, acldefault('f', p.proowner))) a
		               WHERE a.grantee = 0 AND a.privilege_type = 'EXECUTE'),
		       has_function_privilege('ballast_app', p.oid, 'EXECUTE')
		FROM pg_proc p
		WHERE p.proname = 'invitation_lookup'`).Scan(&securityDefiner, &pinned, &publicCanExecute, &appCanExecute)
	if err != nil {
		t.Fatalf("read invitation_lookup: %v", err)
	}

	if !securityDefiner {
		t.Error("invitation_lookup is not SECURITY DEFINER")
	}
	if !pinned {
		t.Error("invitation_lookup does not pin its search_path")
	}
	if publicCanExecute {
		t.Error("PUBLIC may execute invitation_lookup; only ballast_app should")
	}
	if !appCanExecute {
		t.Error("ballast_app may not execute invitation_lookup")
	}
}
