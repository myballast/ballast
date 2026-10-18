package invitation

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

var now = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

type fixedClock struct{}

func (fixedClock) Now() time.Time { return now }

type fixedTokens struct{}

func (fixedTokens) New() (string, error) { return "the-secret-token", nil }

type fakeRepository struct {
	created      *domain.Invitation
	createdHash  string
	createdEntry domain.AuditEntry
	pending      []domain.Invitation
	revoked      []uuid.UUID
	revokedEntry domain.AuditEntry
	described    *Described
	userByEmail  map[string]uuid.UUID
	accepted     *Acceptance
	acceptErr    error
}

func (f *fakeRepository) Create(_ context.Context, in domain.Invitation, hash string, entry domain.AuditEntry) error {
	f.created, f.createdHash, f.createdEntry = &in, hash, entry
	return nil
}

func (f *fakeRepository) Pending(context.Context, uuid.UUID, uuid.UUID) ([]domain.Invitation, error) {
	return f.pending, nil
}

func (f *fakeRepository) Revoke(_ context.Context, _, _, id uuid.UUID, _ time.Time, entry domain.AuditEntry) error {
	f.revoked, f.revokedEntry = append(f.revoked, id), entry
	return nil
}

func (f *fakeRepository) ByTokenHash(_ context.Context, hash string) (Described, bool, error) {
	if f.described == nil || hash != HashToken("the-secret-token") {
		return Described{}, false, nil
	}
	return *f.described, true, nil
}

func (f *fakeRepository) UserByEmail(_ context.Context, _ uuid.UUID, email string) (uuid.UUID, bool, error) {
	id, ok := f.userByEmail[email]
	return id, ok, nil
}

func (f *fakeRepository) Accept(_ context.Context, a Acceptance) (domain.Identity, error) {
	if f.acceptErr != nil {
		return domain.Identity{}, f.acceptErr
	}
	f.accepted = &a
	return domain.Identity{UserID: a.NewUserID, OrgID: a.Invitation.OrgID}, nil
}

type fakePeople struct{ profile Profile }

func (f fakePeople) Profile(context.Context, domain.Identity) (Profile, error) { return f.profile, nil }

type fakeMemberships struct {
	set   []domain.Role
	entry domain.AuditEntry
}

func (f *fakeMemberships) SetRole(_ context.Context, _, _, _ uuid.UUID, role domain.Role, entry domain.AuditEntry) error {
	f.set, f.entry = append(f.set, role), entry
	return nil
}

// authorizer holds the caller's role, and who else is already a member.
type authorizer struct {
	role    domain.Role
	allowed bool
	members map[uuid.UUID]bool
	// outsiders belong to the organization and to no project.
	outsiders map[uuid.UUID]bool
}

func (a authorizer) Can(context.Context, domain.Identity, domain.Permission, uuid.UUID) (bool, error) {
	return a.allowed, nil
}

func (a authorizer) MembershipOf(_ context.Context, who domain.Identity, _ uuid.UUID) (domain.Membership, bool, error) {
	if a.members[who.UserID] {
		return domain.Membership{UserID: who.UserID, Role: domain.RoleViewer}, true, nil
	}
	if a.role == "" || a.outsiders[who.UserID] {
		return domain.Membership{}, false, nil
	}
	return domain.Membership{UserID: who.UserID, Role: a.role}, true, nil
}

type journal struct{ entries []domain.AuditEntry }

func (j *journal) Record(_ context.Context, e domain.AuditEntry) error {
	j.entries = append(j.entries, e)
	return nil
}

type world struct {
	service     *Service
	repo        *fakeRepository
	memberships *fakeMemberships
	journal     *journal
}

func newWorld(authz authorizer, people Profile) world {
	w := world{repo: &fakeRepository{userByEmail: map[string]uuid.UUID{}}, memberships: &fakeMemberships{}, journal: &journal{}}
	w.service = NewService(w.repo, fakePeople{people}, w.memberships, authz, w.journal, fixedTokens{}, fixedClock{},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	return w
}

func caller() domain.Identity { return domain.Identity{UserID: uuid.New(), OrgID: uuid.New()} }

func TestAnInvitationStoresTheHashAndNeverTheToken(t *testing.T) {
	w := newWorld(authorizer{role: domain.RoleAdmin, allowed: true}, Profile{})
	who, project := caller(), uuid.New()

	created, err := w.service.Create(context.Background(), who, project, " Ada@Example.test ", domain.RoleRunner)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if created.Token != "the-secret-token" {
		t.Errorf("the token shown is %q", created.Token)
	}
	if w.repo.createdHash != HashToken("the-secret-token") || strings.Contains(w.repo.createdHash, "secret") {
		t.Errorf("stored %q: not the hash of the token", w.repo.createdHash)
	}
	if len(w.repo.createdHash) != 64 {
		t.Errorf("a SHA-256 in hex is 64 characters, got %d", len(w.repo.createdHash))
	}

	in := w.repo.created
	switch {
	case in.Email != "ada@example.test":
		t.Errorf("kept as %q", in.Email)
	case !in.ExpiresAt.Equal(now.Add(7 * 24 * time.Hour)):
		t.Errorf("expires %v, want seven days from now", in.ExpiresAt)
	case in.InvitedBy == nil || *in.InvitedBy != who.UserID:
		t.Errorf("invited by %v", in.InvitedBy)
	}

	// The journal entry travels into the transaction, names the invitation, and
	// carries the role and nothing of the link or the address.
	e := w.repo.createdEntry
	switch {
	case e.Action != domain.ActionMemberInvited || e.Outcome != domain.Allowed:
		t.Errorf("entry: %s / %s", e.Action, e.Outcome)
	case e.TargetID == nil || *e.TargetID != in.ID || e.TargetType != domain.TargetInvitation:
		t.Errorf("target: %s %v", e.TargetType, e.TargetID)
	case len(e.Details) != 1 || e.Details["role"] != "runner":
		t.Errorf("details: %v", e.Details)
	}
	for _, v := range e.Details {
		if strings.Contains(v, "secret") || strings.Contains(v, "@") {
			t.Errorf("the entry carries %q", v)
		}
	}
}

func TestSomebodyWithoutThePermissionCannotInvite(t *testing.T) {
	w := newWorld(authorizer{role: domain.RoleViewer, allowed: false}, Profile{})

	_, err := w.service.Create(context.Background(), caller(), uuid.New(), "ada@example.test", domain.RoleViewer)
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("got %v", err)
	}
	if w.repo.created != nil {
		t.Error("an invitation was written")
	}
}

// Granting is bounded by the caller's own role even when the permission to
// manage members was somehow theirs, and the attempt is journaled: it is
// somebody prevented from doing something.
func TestNobodyInvitesAboveTheirOwnRole(t *testing.T) {
	w := newWorld(authorizer{role: domain.RoleMaintainer, allowed: true}, Profile{})
	who, project := caller(), uuid.New()

	_, err := w.service.Create(context.Background(), who, project, "ada@example.test", domain.RoleAdmin)
	if !errors.Is(err, domain.ErrRoleAboveOwn) {
		t.Fatalf("got %v", err)
	}
	if w.repo.created != nil {
		t.Error("an invitation was written")
	}

	if len(w.journal.entries) != 1 {
		t.Fatalf("%d entries", len(w.journal.entries))
	}
	e := w.journal.entries[0]
	if e.Outcome != domain.Refused || e.Details["reason"] != "role_above_own" || e.ActorID == nil || *e.ActorID != who.UserID {
		t.Errorf("entry: %+v", e)
	}
}

func TestAMistakeInTheFormIsNotJournaled(t *testing.T) {
	w := newWorld(authorizer{role: domain.RoleAdmin, allowed: true}, Profile{})

	if _, err := w.service.Create(context.Background(), caller(), uuid.New(), "not an address", domain.RoleViewer); !errors.Is(err, domain.ErrInvalidInvitation) {
		t.Fatalf("got %v", err)
	}
	if len(w.journal.entries) != 0 {
		t.Errorf("a typing mistake was journaled: %v", w.journal.entries)
	}
}

func TestSomebodyAlreadyInTheOrganizationIsAddedAtOnce(t *testing.T) {
	existing := uuid.New()
	w := newWorld(authorizer{role: domain.RoleAdmin, allowed: true, outsiders: map[uuid.UUID]bool{existing: true}}, Profile{})
	w.repo.userByEmail["ada@example.test"] = existing

	created, err := w.service.Create(context.Background(), caller(), uuid.New(), "Ada@example.test", domain.RoleMaintainer)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if created.Added == nil || *created.Added != existing || created.Token != "" || created.Invitation != nil {
		t.Errorf("result: %+v", created)
	}
	if w.repo.created != nil {
		t.Error("an invitation was written for somebody who needed none")
	}
	if len(w.memberships.set) != 1 || w.memberships.set[0] != domain.RoleMaintainer {
		t.Errorf("roles set: %v", w.memberships.set)
	}
	if w.memberships.entry.Action != domain.ActionRoleChanged {
		t.Errorf("entry: %s", w.memberships.entry.Action)
	}
}

func TestSomebodyAlreadyInTheProjectIsNotInvitedAgain(t *testing.T) {
	existing := uuid.New()
	w := newWorld(authorizer{role: domain.RoleAdmin, allowed: true, members: map[uuid.UUID]bool{existing: true}}, Profile{})
	w.repo.userByEmail["ada@example.test"] = existing

	_, err := w.service.Create(context.Background(), caller(), uuid.New(), "ada@example.test", domain.RoleViewer)
	if !errors.Is(err, domain.ErrAlreadyMember) {
		t.Fatalf("got %v", err)
	}
	if len(w.memberships.set) != 0 {
		t.Error("a role was written")
	}
}

func TestOpenInvitationsSayWhetherTheyHaveExpired(t *testing.T) {
	w := newWorld(authorizer{role: domain.RoleAdmin, allowed: true}, Profile{})
	w.repo.pending = []domain.Invitation{
		{ID: uuid.New(), ExpiresAt: now.Add(time.Hour)},
		{ID: uuid.New(), ExpiresAt: now.Add(-time.Hour)},
	}

	open, err := w.service.Pending(context.Background(), caller(), uuid.New())
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(open) != 2 || open[0].State != domain.InvitationPending || open[1].State != domain.InvitationExpired {
		t.Errorf("states: %+v", open)
	}
}

func TestRevokingCarriesItsEntry(t *testing.T) {
	w := newWorld(authorizer{role: domain.RoleAdmin, allowed: true}, Profile{})
	who, project, id := caller(), uuid.New(), uuid.New()

	if err := w.service.Revoke(context.Background(), who, project, id); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	e := w.repo.revokedEntry
	if e.Action != domain.ActionInvitationRevoked || e.TargetID == nil || *e.TargetID != id || e.ActorID == nil || *e.ActorID != who.UserID {
		t.Errorf("entry: %+v", e)
	}
}

func TestOnlyAnAdministratorSeesOrRevokesInvitations(t *testing.T) {
	w := newWorld(authorizer{role: domain.RoleRunner, allowed: false}, Profile{})

	if _, err := w.service.Pending(context.Background(), caller(), uuid.New()); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("listing: %v", err)
	}
	if err := w.service.Revoke(context.Background(), caller(), uuid.New(), uuid.New()); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("revoking: %v", err)
	}
	if len(w.repo.revoked) != 0 {
		t.Error("an invitation was revoked")
	}
}

func described(mutate func(*Described)) *Described {
	d := &Described{
		Invitation: domain.Invitation{
			ID: uuid.New(), OrgID: uuid.New(), ProjectID: uuid.New(),
			Email: "ada@example.test", Role: domain.RoleMaintainer, ExpiresAt: now.Add(time.Hour),
		},
		OrgName: "Acme", ProjectName: "storefront",
	}
	if mutate != nil {
		mutate(d)
	}
	return d
}

func TestAnUnknownTokenIsNotFound(t *testing.T) {
	w := newWorld(authorizer{}, Profile{})

	if _, _, err := w.service.Describe(context.Background(), "guess"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Describe: %v", err)
	}
	if _, err := w.service.Accept(context.Background(), caller(), "guess"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Accept: %v", err)
	}
}

func TestALinkIsDescribedWhateverBecameOfIt(t *testing.T) {
	revoked := now.Add(-time.Minute)
	w := newWorld(authorizer{}, Profile{})
	w.repo.described = described(func(d *Described) { d.RevokedAt = &revoked })

	got, state, err := w.service.Describe(context.Background(), "the-secret-token")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if state != domain.InvitationRevoked || got.OrgName != "Acme" || got.ProjectName != "storefront" {
		t.Errorf("%+v %s", got, state)
	}
}

func TestAcceptingJoinsAsTheInvitedRoleAndJournalsIt(t *testing.T) {
	w := newWorld(authorizer{}, Profile{Subject: "github|1", Email: "Ada@Example.test", Name: "Ada"})
	w.repo.described = described(nil)

	joined, err := w.service.Accept(context.Background(), caller(), "the-secret-token")
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}

	a := w.repo.accepted
	if a == nil {
		t.Fatal("nothing was accepted")
	}
	if joined.OrgID != w.repo.described.OrgID || joined.ProjectID != w.repo.described.ProjectID {
		t.Errorf("joined %+v", joined)
	}
	if a.Invitee.Subject != "github|1" || a.NewUserID == uuid.Nil || !a.At.Equal(now) {
		t.Errorf("acceptance: %+v", a)
	}
	e := a.Entry
	if e.Action != domain.ActionMemberJoined || e.Outcome != domain.Allowed || e.Details["role"] != "maintainer" {
		t.Errorf("entry: %+v", e)
	}
	if e.OrgID != w.repo.described.OrgID {
		t.Error("the entry belongs to the wrong organization: it must be the inviting one")
	}
}

// The link is a credential for one address. Forwarded, it does not make its
// holder a member, and the attempt is what the journal is for.
func TestAForwardedLinkDoesNotMakeAMember(t *testing.T) {
	w := newWorld(authorizer{}, Profile{Subject: "github|2", Email: "mallory@example.test"})
	w.repo.described = described(nil)

	_, err := w.service.Accept(context.Background(), caller(), "the-secret-token")
	if !errors.Is(err, domain.ErrWrongInvitee) {
		t.Fatalf("got %v", err)
	}
	if w.repo.accepted != nil {
		t.Error("somebody joined with another person's link")
	}

	if len(w.journal.entries) != 1 {
		t.Fatalf("%d entries", len(w.journal.entries))
	}
	e := w.journal.entries[0]
	if e.Outcome != domain.Refused || e.Action != domain.ActionMemberJoined || e.Details["reason"] != "wrong_invitee" {
		t.Errorf("entry: %+v", e)
	}
	// Nobody acted for Ballast to name, and the refused address is not written.
	if e.ActorID != nil {
		t.Errorf("author: %v", e.ActorID)
	}
	for _, v := range e.Details {
		if strings.Contains(v, "@") {
			t.Errorf("the entry carries an address: %q", v)
		}
	}
}

func TestADeadLinkCannotBeFollowed(t *testing.T) {
	past, earlier := now.Add(-time.Hour), now.Add(-time.Minute)
	cases := map[string]func(*Described){
		"expired":  func(d *Described) { d.ExpiresAt = past },
		"revoked":  func(d *Described) { d.RevokedAt = &earlier },
		"accepted": func(d *Described) { d.AcceptedAt = &earlier },
	}
	for name, mutate := range cases {
		w := newWorld(authorizer{}, Profile{Subject: "github|1", Email: "ada@example.test"})
		w.repo.described = described(mutate)

		_, err := w.service.Accept(context.Background(), caller(), "the-secret-token")
		if !errors.Is(err, domain.ErrInvitationUnusable) {
			t.Errorf("%s: got %v", name, err)
		}
		if w.repo.accepted != nil {
			t.Errorf("%s: somebody joined", name)
		}
	}
}
