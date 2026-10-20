//go:build integration

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	httpadapter "github.com/youssefmahersi/ballast/internal/adapter/http"
	"github.com/youssefmahersi/ballast/internal/adapter/http/session"
	"github.com/youssefmahersi/ballast/internal/adapter/postgres"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/infra"
	"github.com/youssefmahersi/ballast/internal/infra/crypto"
	"github.com/youssefmahersi/ballast/internal/usecase/audit"
	"github.com/youssefmahersi/ballast/internal/usecase/auth"
	"github.com/youssefmahersi/ballast/internal/usecase/authz"
	"github.com/youssefmahersi/ballast/internal/usecase/invitation"
	"github.com/youssefmahersi/ballast/internal/usecase/project"
)

// The members page, through the API it calls. Two organizations: Acme, which has
// the project, an administrator, a maintainer and a viewer; and Ada's own, where
// she is an administrator of nothing that Acme owns. Ada is invited to Acme's
// project from her address, follows the link from her own organization, and
// ends up acting for Acme.

type membersWorld struct {
	api     *httptest.Server
	db      *postgres.DB
	acme    uuid.UUID
	home    uuid.UUID
	project uuid.UUID

	admin, maintainer, viewer, ada, mallory domain.Identity
	sessions                                *session.Manager
}

const (
	adaEmail     = "ada@example.test"
	malloryEmail = "mallory@example.test"
)

func newMembersWorld(ctx context.Context, t *testing.T) membersWorld {
	t.Helper()

	db, err := postgres.Open(ctx, dsn())
	if err != nil {
		t.Fatalf("open the database: %v", err)
	}
	t.Cleanup(db.Close)

	w := membersWorld{db: db, acme: uuid.New(), home: uuid.New(), project: uuid.New()}
	tag := uuid.NewString()[:8]

	user := func(org uuid.UUID) domain.Identity { return domain.Identity{UserID: uuid.New(), OrgID: org} }
	w.admin, w.maintainer, w.viewer = user(w.acme), user(w.acme), user(w.acme)
	w.ada, w.mallory = user(w.home), user(uuid.New())
	malloryOrg := w.mallory.OrgID

	seed := func(org uuid.UUID, name string, people map[domain.Identity][3]string) {
		err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `INSERT INTO organization (id, name) VALUES ($1, $2)`, org, name); err != nil {
				return err
			}
			for who, v := range people {
				if _, err := tx.Exec(ctx,
					`INSERT INTO app_user (id, org_id, subject, email, name) VALUES ($1, $2, $3, $4, $5)`,
					who.UserID, org, v[0]+tag, v[1], v[2]); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	seed(w.acme, "Acme", map[domain.Identity][3]string{
		w.admin:      {"admin", "admin-" + tag + "@example.test", "Alex Admin"},
		w.maintainer: {"maintainer", "maintainer-" + tag + "@example.test", "Max Maintainer"},
		w.viewer:     {"viewer", "viewer-" + tag + "@example.test", "Val Viewer"},
	})
	// Ada's address is the same on every run of this test, and unique per
	// organization: each run has its own home organization.
	seed(w.home, "Ada's organization", map[domain.Identity][3]string{w.ada: {"ada", adaEmail, "Ada"}})
	seed(malloryOrg, "Mallory's organization", map[domain.Identity][3]string{w.mallory: {"mallory", malloryEmail, "Mallory"}})

	err = db.InTenantTx(ctx, w.acme, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO project (id, org_id, name, slug) VALUES ($1, $2, 'storefront', 'storefront')`,
			w.project, w.acme); err != nil {
			return err
		}
		for who, role := range map[domain.Identity]domain.Role{
			w.admin: domain.RoleAdmin, w.maintainer: domain.RoleMaintainer, w.viewer: domain.RoleViewer,
		} {
			if _, err := tx.Exec(ctx,
				`INSERT INTO membership (org_id, user_id, project_id, role) VALUES ($1, $2, $3, $4)`,
				w.acme, who.UserID, w.project, string(role)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed the project: %v", err)
	}

	t.Cleanup(func() {
		cleanup := context.WithoutCancel(ctx)
		for _, org := range []uuid.UUID{w.acme, w.home, malloryOrg} {
			_ = db.InTenantTx(cleanup, org, func(tx pgx.Tx) error {
				for _, table := range []string{"invitation", "project_favorite", "membership", "project", "app_user", "organization"} {
					if _, err := tx.Exec(cleanup,
						"DELETE FROM "+pgx.Identifier{table}.Sanitize()+" WHERE org_id = $1", org); err != nil {
						return err
					}
				}
				return nil
			})
		}
	})

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	memberships := postgres.NewMembershipRepository(db)
	permissions := authz.NewService(memberships, log)
	accounts := postgres.NewAccountRepository(db)
	journal := postgres.NewAuditRepository(db)

	w.sessions, err = session.NewManager(
		[]byte(strings.Repeat("m", session.MinSecretLength)), time.Hour, infra.SystemClock{})
	if err != nil {
		t.Fatalf("session manager: %v", err)
	}

	projects := project.NewService(
		postgres.NewProjectRepository(db), memberships, permissions, journal, infra.SystemClock{}, log)
	invitations := invitation.NewService(
		postgres.NewInvitationRepository(db), accounts, memberships, permissions, journal,
		crypto.NewRandomTokens(), infra.SystemClock{}, log)

	web, _ := url.Parse("http://web.test")
	w.api = httptest.NewServer(httpadapter.NewRouter(
		nil, projects, nil, nil, invitations,
		audit.NewService(journal, permissions, log),
		auth.NewService(accounts, nil, infra.SystemClock{}, log),
		permissions, w.sessions, db.Ping, web, log,
	))
	t.Cleanup(w.api.Close)

	return w
}

func (w membersWorld) cookie(t *testing.T, who domain.Identity) *http.Cookie {
	t.Helper()

	issued := httptest.NewRecorder()
	if err := w.sessions.Issue(issued, who); err != nil {
		t.Fatalf("issue a session: %v", err)
	}
	return issued.Result().Cookies()[0]
}

// call sends a request as who (nobody when who is nil) and decodes a JSON
// answer into into, when there is one.
func (w membersWorld) call(
	t *testing.T, who *http.Cookie, method, path, body string, into any,
) (int, []*http.Cookie) {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, w.api.URL+path, reader)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, path, err)
	}
	if who != nil {
		req.AddCookie(who)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := w.api.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = res.Body.Close() }()

	if into != nil && res.StatusCode < 300 && res.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(res.Body).Decode(into); err != nil {
			t.Fatalf("decode %s %s: %v", method, path, err)
		}
	}
	return res.StatusCode, res.Cookies()
}

type invited struct {
	Invitation struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		State string `json:"state"`
	} `json:"invitation"`
	Token string `json:"token"`
}

func (w membersWorld) invite(t *testing.T, as *http.Cookie, email, role string) (int, invited) {
	t.Helper()

	var out invited
	status, _ := w.call(t, as, "POST", fmt.Sprintf("/projects/%s/invitations", w.project),
		fmt.Sprintf(`{"email":%q,"role":%q}`, email, role), &out)
	return status, out
}

func (w membersWorld) journalOf(t *testing.T, action domain.AuditAction) []domain.AuditEntry {
	t.Helper()

	entries, err := postgres.NewAuditRepository(w.db).InProject(context.Background(), w.acme, w.project, 200)
	if err != nil {
		t.Fatalf("read the journal: %v", err)
	}
	var found []domain.AuditEntry
	for _, e := range entries {
		if e.Action == action {
			found = append(found, e)
		}
	}
	return found
}

// Only an administrator brings people in, lists the invitations or withdraws
// one. A maintainer manages scenarios and environments, not members.
func TestOnlyAnAdministratorManagesInvitations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	w := newMembersWorld(ctx, t)

	maintainer, viewer, admin := w.cookie(t, w.maintainer), w.cookie(t, w.viewer), w.cookie(t, w.admin)
	path := fmt.Sprintf("/projects/%s/invitations", w.project)

	for name, as := range map[string]*http.Cookie{"maintainer": maintainer, "viewer": viewer} {
		if status, _ := w.call(t, as, "GET", path, "", nil); status != http.StatusForbidden {
			t.Errorf("a %s listing invitations got %d, want 403", name, status)
		}
		if status, _ := w.invite(t, as, adaEmail, "viewer"); status != http.StatusForbidden {
			t.Errorf("a %s inviting got %d, want 403", name, status)
		}
	}
	if status, _ := w.call(t, nil, "GET", path, "", nil); status != http.StatusUnauthorized {
		t.Errorf("an anonymous listing got %d, want 401", status)
	}

	// A maintainer cannot change a role either, nor remove a member.
	member := fmt.Sprintf("/projects/%s/members/%s", w.project, w.viewer.UserID)
	if status, _ := w.call(t, maintainer, "PUT", member, `{"role":"admin"}`, nil); status != http.StatusForbidden {
		t.Errorf("a maintainer changing a role got %d, want 403", status)
	}
	if status, _ := w.call(t, maintainer, "DELETE", member, "", nil); status != http.StatusForbidden {
		t.Errorf("a maintainer removing a member got %d, want 403", status)
	}

	if status, _ := w.invite(t, admin, adaEmail, "viewer"); status != http.StatusCreated {
		t.Errorf("an administrator inviting got %d, want 201", status)
	}
}

// The whole journey: invite, read the link without signing in, be refused as
// somebody else, accept as the person it was written for, then act for the
// organization that invited — and not be able to use the link again.
func TestAnInvitationBringsSomeoneFromAnotherOrganizationIntoTheProject(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	w := newMembersWorld(ctx, t)

	admin, ada, mallory := w.cookie(t, w.admin), w.cookie(t, w.ada), w.cookie(t, w.mallory)

	status, created := w.invite(t, admin, "  ADA@example.test ", "maintainer")
	if status != http.StatusCreated || created.Token == "" || created.Invitation.Email != adaEmail {
		t.Fatalf("invite: %d %+v", status, created)
	}

	// The list shows it, and never the token.
	var listing struct {
		Invitations []map[string]any `json:"invitations"`
	}
	if status, _ := w.call(t, admin, "GET", fmt.Sprintf("/projects/%s/invitations", w.project), "", &listing); status != http.StatusOK {
		t.Fatalf("list: %d", status)
	}
	if len(listing.Invitations) != 1 || listing.Invitations[0]["state"] != "pending" {
		t.Fatalf("listing: %v", listing.Invitations)
	}
	for key := range listing.Invitations[0] {
		if strings.Contains(key, "token") {
			t.Errorf("the listing carries %q", key)
		}
	}

	// Anybody holding the link can read what it offers, signed in or not.
	var preview struct {
		Organization string `json:"organization_name"`
		Project      string `json:"project_name"`
		Role         string `json:"role"`
		State        string `json:"state"`
	}
	if status, _ := w.call(t, nil, "GET", "/invitations/"+created.Token, "", &preview); status != http.StatusOK {
		t.Fatalf("preview: %d", status)
	}
	if preview.Organization != "Acme" || preview.Project != "storefront" || preview.Role != "maintainer" || preview.State != "pending" {
		t.Errorf("preview: %+v", preview)
	}
	if status, _ := w.call(t, nil, "GET", "/invitations/"+strings.Repeat("x", 43), "", nil); status != http.StatusNotFound {
		t.Errorf("an unknown token got %d, want 404", status)
	}

	// Following it needs a session, and the right address.
	if status, _ := w.call(t, nil, "POST", "/invitations/"+created.Token+"/accept", "", nil); status != http.StatusUnauthorized {
		t.Errorf("accepting anonymously got %d, want 401", status)
	}
	if status, _ := w.call(t, mallory, "POST", "/invitations/"+created.Token+"/accept", "", nil); status != http.StatusForbidden {
		t.Errorf("accepting with another address got %d, want 403", status)
	}
	refusals := w.journalOf(t, domain.ActionMemberJoined)
	if len(refusals) != 1 || refusals[0].Outcome != domain.Refused || refusals[0].Details["reason"] != "wrong_invitee" {
		t.Fatalf("the refusal was not journaled: %+v", refusals)
	}

	// Ada has no access to Acme before she accepts.
	if status, _ := w.call(t, ada, "GET", fmt.Sprintf("/projects/%s", w.project), "", nil); status != http.StatusForbidden {
		t.Errorf("before accepting, Ada reading the project got %d, want 403", status)
	}

	var accepted struct {
		OrganizationID string `json:"organization_id"`
		ProjectID      string `json:"project_id"`
	}
	status, cookies := w.call(t, ada, "POST", "/invitations/"+created.Token+"/accept", "", &accepted)
	if status != http.StatusOK || accepted.OrganizationID != w.acme.String() || accepted.ProjectID != w.project.String() {
		t.Fatalf("accept: %d %+v", status, accepted)
	}
	var acme *http.Cookie
	for _, c := range cookies {
		if c.Name == session.CookieName {
			acme = c
		}
	}
	if acme == nil {
		t.Fatal("accepting set no session")
	}

	// With that session she acts for Acme, as a maintainer.
	var members struct {
		Members []struct {
			UserID       string     `json:"user_id"`
			Email        string     `json:"email"`
			Role         string     `json:"role"`
			LastActivity *time.Time `json:"last_activity"`
		} `json:"members"`
	}
	if status, _ := w.call(t, acme, "GET", fmt.Sprintf("/projects/%s/members", w.project), "", &members); status != http.StatusOK {
		t.Fatalf("members as Ada: %d", status)
	}
	var found bool
	for _, m := range members.Members {
		if m.Email == adaEmail {
			found = true
			if m.Role != "maintainer" {
				t.Errorf("Ada joined as %q, want maintainer", m.Role)
			}
		}
	}
	if !found || len(members.Members) != 4 {
		t.Errorf("members after joining: %+v", members.Members)
	}

	// The invitation is spent, and so is the link.
	if status, _ := w.call(t, ada, "POST", "/invitations/"+created.Token+"/accept", "", nil); status != http.StatusGone {
		t.Errorf("a second use got %d, want 410", status)
	}
	var again struct {
		State string `json:"state"`
	}
	w.call(t, nil, "GET", "/invitations/"+created.Token, "", &again)
	if again.State != "accepted" {
		t.Errorf("the link says %q after use", again.State)
	}

	joined := w.journalOf(t, domain.ActionMemberJoined)
	var allowed int
	for _, e := range joined {
		if e.Outcome == domain.Allowed {
			allowed++
			if e.ActorID == nil || e.Details["role"] != "maintainer" {
				t.Errorf("the entry of joining: %+v", e)
			}
		}
	}
	if allowed != 1 {
		t.Errorf("%d entries of joining, want 1", allowed)
	}

	// She belongs to two organizations now, and can go back to her own.
	var me struct {
		Organization  struct{ ID string }         `json:"organization"`
		Organizations []struct{ ID, Name string } `json:"organizations"`
	}
	if status, _ := w.call(t, acme, "GET", "/me", "", &me); status != http.StatusOK {
		t.Fatalf("me: %d", status)
	}
	if me.Organization.ID != w.acme.String() || len(me.Organizations) != 2 {
		t.Errorf("me: %+v", me)
	}
	status, back := w.call(t, acme, "POST", "/me/organization", fmt.Sprintf(`{"organization_id":%q}`, w.home), nil)
	if status != http.StatusNoContent || len(back) == 0 {
		t.Fatalf("switch back: %d", status)
	}
	if status, _ := w.call(t, acme, "POST", "/me/organization", fmt.Sprintf(`{"organization_id":%q}`, uuid.New()), nil); status != http.StatusForbidden {
		t.Errorf("switching to a stranger's organization got %d, want 403", status)
	}
}

func TestARevokedInvitationCannotBeFollowed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	w := newMembersWorld(ctx, t)

	admin, ada := w.cookie(t, w.admin), w.cookie(t, w.ada)
	_, created := w.invite(t, admin, adaEmail, "viewer")

	// A second invitation to the same address, while the first is open.
	if status, _ := w.invite(t, admin, adaEmail, "viewer"); status != http.StatusConflict {
		t.Errorf("inviting twice got %d, want 409", status)
	}

	revoke := fmt.Sprintf("/projects/%s/invitations/%s", w.project, created.Invitation.ID)
	if status, _ := w.call(t, w.cookie(t, w.maintainer), "DELETE", revoke, "", nil); status != http.StatusForbidden {
		t.Errorf("a maintainer revoking got %d, want 403", status)
	}
	if status, _ := w.call(t, admin, "DELETE", revoke, "", nil); status != http.StatusNoContent {
		t.Fatalf("revoking got %d, want 204", status)
	}
	if status, _ := w.call(t, admin, "DELETE", revoke, "", nil); status != http.StatusNotFound {
		t.Errorf("revoking twice got %d, want 404", status)
	}

	if status, _ := w.call(t, ada, "POST", "/invitations/"+created.Token+"/accept", "", nil); status != http.StatusGone {
		t.Errorf("following a revoked link got %d, want 410", status)
	}
	if got := w.journalOf(t, domain.ActionInvitationRevoked); len(got) != 1 || got[0].Outcome != domain.Allowed {
		t.Errorf("the revocation was not journaled: %+v", got)
	}

	// And the person can be invited again.
	if status, _ := w.invite(t, admin, adaEmail, "viewer"); status != http.StatusCreated {
		t.Errorf("inviting again after a revocation got %d, want 201", status)
	}
}

// Somebody who already has an account in the organization needs no link: they
// are added, and the answer says so.
func TestSomeoneAlreadyInTheOrganizationIsAddedWithoutALink(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	w := newMembersWorld(ctx, t)

	// An Acme user who belongs to no project yet.
	outsider, outsiderEmail := uuid.New(), "outsider-"+uuid.NewString()[:8]+"@example.test"
	if err := w.db.InTenantTx(ctx, w.acme, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO app_user (id, org_id, subject, email) VALUES ($1, $2, $3, $4)`,
			outsider, w.acme, "outsider-"+outsider.String(), outsiderEmail)
		return err
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	admin := w.cookie(t, w.admin)
	var added struct {
		UserID string `json:"user_id"`
	}
	status, _ := w.call(t, admin, "POST", fmt.Sprintf("/projects/%s/invitations", w.project),
		fmt.Sprintf(`{"email":%q,"role":"runner"}`, strings.ToUpper(outsiderEmail)), &added)
	if status != http.StatusOK || added.UserID != outsider.String() {
		t.Fatalf("got %d %+v, want 200 with the existing user", status, added)
	}

	// And a second time, they are already in.
	if status, _ := w.invite(t, admin, outsiderEmail, "runner"); status != http.StatusConflict {
		t.Errorf("adding a member twice got %d, want 409", status)
	}
}

func TestTheMembersListSaysWhenEachPersonLastActed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	w := newMembersWorld(ctx, t)

	admin := w.cookie(t, w.admin)
	if status, _ := w.invite(t, admin, adaEmail, "viewer"); status != http.StatusCreated {
		t.Fatalf("invite: %d", status)
	}

	var members struct {
		Members []struct {
			UserID       string     `json:"user_id"`
			LastActivity *time.Time `json:"last_activity"`
		} `json:"members"`
	}
	w.call(t, admin, "GET", fmt.Sprintf("/projects/%s/members", w.project), "", &members)

	for _, m := range members.Members {
		switch m.UserID {
		case w.admin.UserID.String():
			if m.LastActivity == nil {
				t.Error("the administrator invited somebody and has no last activity")
			}
		default:
			if m.LastActivity != nil {
				t.Errorf("%s never did anything and has a last activity: %v", m.UserID, m.LastActivity)
			}
		}
	}
}

func TestTheMatrixOfRolesIsServed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	w := newMembersWorld(ctx, t)

	var matrix struct {
		Capabilities []struct{ ID, Label string } `json:"capabilities"`
		Roles        []struct {
			Role         string   `json:"role"`
			Capabilities []string `json:"capabilities"`
		} `json:"roles"`
	}
	if status, _ := w.call(t, w.cookie(t, w.viewer), "GET", "/roles", "", &matrix); status != http.StatusOK {
		t.Fatalf("roles: %d", status)
	}
	if len(matrix.Capabilities) != 6 || len(matrix.Roles) != 4 {
		t.Fatalf("matrix: %+v", matrix)
	}
	if matrix.Roles[0].Role != "viewer" || len(matrix.Roles[0].Capabilities) != 1 || matrix.Roles[3].Role != "admin" || len(matrix.Roles[3].Capabilities) != 6 {
		t.Errorf("roles: %+v", matrix.Roles)
	}
	if status, _ := w.call(t, nil, "GET", "/roles", "", nil); status != http.StatusUnauthorized {
		t.Errorf("anonymous got %d, want 401", status)
	}
}
