package project

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

type fakeProjects struct {
	byID    map[uuid.UUID]domain.Project
	created *domain.Project
	deleted []uuid.UUID
	err     error
}

func (f *fakeProjects) Create(
	_ context.Context, newProject domain.Project, _ domain.Identity, _ time.Time,
) (domain.Project, error) {
	if f.err != nil {
		return domain.Project{}, f.err
	}
	stored := newProject
	f.created = &stored
	return stored, nil
}

func (f *fakeProjects) ByID(_ context.Context, _, id uuid.UUID) (domain.Project, error) {
	if found, ok := f.byID[id]; ok {
		return found, nil
	}
	return domain.Project{}, domain.ErrNotFound
}

func (f *fakeProjects) OfMember(_ context.Context, _ domain.Identity) ([]domain.Project, error) {
	all := make([]domain.Project, 0, len(f.byID))
	for _, p := range f.byID {
		all = append(all, p)
	}
	return all, nil
}

func (f *fakeProjects) Rename(_ context.Context, _, id uuid.UUID, name string) (domain.Project, error) {
	found, ok := f.byID[id]
	if !ok {
		return domain.Project{}, domain.ErrNotFound
	}
	found.Name = name
	f.byID[id] = found
	return found, nil
}

func (f *fakeProjects) Delete(_ context.Context, _, id uuid.UUID) error {
	if f.err != nil {
		return f.err
	}
	f.deleted = append(f.deleted, id)
	return nil
}

type fakeMemberships struct {
	members map[uuid.UUID][]Member
	set     []struct {
		userID uuid.UUID
		role   domain.Role
	}
	removed []uuid.UUID
}

func (f *fakeMemberships) InProject(_ context.Context, _, projectID uuid.UUID) ([]Member, error) {
	return f.members[projectID], nil
}

func (f *fakeMemberships) SetRole(
	_ context.Context, _, _, userID uuid.UUID, role domain.Role,
) error {
	f.set = append(f.set, struct {
		userID uuid.UUID
		role   domain.Role
	}{userID, role})
	return nil
}

func (f *fakeMemberships) Remove(_ context.Context, _, _, userID uuid.UUID) error {
	f.removed = append(f.removed, userID)
	return nil
}

// allowAll and refuseAll stand in for the real authorizer. What it decides is
// tested in usecase/authz; what matters here is that the service asks.
type allowAll struct{ asked []domain.Permission }

func (a *allowAll) Can(_ context.Context, _ domain.Identity, perm domain.Permission, _ uuid.UUID) (bool, error) {
	a.asked = append(a.asked, perm)
	return true, nil
}

func (a *allowAll) CanInAnyProject(_ context.Context, _ domain.Identity, perm domain.Permission) (bool, error) {
	a.asked = append(a.asked, perm)
	return true, nil
}

type refuseAll struct{}

func (refuseAll) Can(context.Context, domain.Identity, domain.Permission, uuid.UUID) (bool, error) {
	return false, nil
}

func (refuseAll) CanInAnyProject(context.Context, domain.Identity, domain.Permission) (bool, error) {
	return false, nil
}

type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Unix(0, 0).UTC() }

func newService(projects *fakeProjects, memberships *fakeMemberships, authz Authorizer) *Service {
	return NewService(projects, memberships, authz, fixedClock{},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func someone() domain.Identity {
	return domain.Identity{UserID: uuid.New(), OrgID: uuid.New()}
}

func TestCreateMakesTheCallerAnAdministrator(t *testing.T) {
	projects := &fakeProjects{byID: map[uuid.UUID]domain.Project{}}
	permissions := &allowAll{}
	who := someone()

	created, err := newService(projects, &fakeMemberships{}, permissions).
		Create(context.Background(), who, "Checkout service", "checkout")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	switch {
	case projects.created == nil:
		t.Fatal("nothing was stored")
	case created.OrgID != who.OrgID:
		t.Error("the project was not attached to the caller's organization")
	case created.Slug != "checkout":
		t.Errorf("slug is %q", created.Slug)
	case created.ID == uuid.Nil:
		t.Error("the project has no identifier")
	}

	// The repository is what writes the admin membership, in the same
	// transaction. What is checked here is that the service asked for the right
	// permission before any of it.
	if len(permissions.asked) != 1 || permissions.asked[0] != domain.PermManageProject {
		t.Errorf("permissions asked for: %v", permissions.asked)
	}
}

func TestCreateRefusesSomebodyWhoAdministersNothing(t *testing.T) {
	projects := &fakeProjects{byID: map[uuid.UUID]domain.Project{}}

	_, err := newService(projects, &fakeMemberships{}, refuseAll{}).
		Create(context.Background(), someone(), "Checkout service", "checkout")
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("got %v, want ErrForbidden", err)
	}
	if projects.created != nil {
		t.Error("a project was stored for somebody who was refused")
	}
}

func TestCreateRefusesABadSlug(t *testing.T) {
	projects := &fakeProjects{byID: map[uuid.UUID]domain.Project{}}
	service := newService(projects, &fakeMemberships{}, &allowAll{})

	for _, slug := range []string{"", "Checkout", "check out", "-checkout", "checkout-", "check--out", "chèque"} {
		if _, err := service.Create(context.Background(), someone(), "A project", slug); !errors.Is(err, domain.ErrInvalidProject) {
			t.Errorf("slug %q was accepted: %v", slug, err)
		}
	}
	if projects.created != nil {
		t.Error("a project was stored despite a refused slug")
	}
}

// Every read and write of a project asks for a permission first. A refusal and
// a project that is not there look the same from outside.
func TestEveryOperationIsGuarded(t *testing.T) {
	id := uuid.New()
	who := someone()

	projects := &fakeProjects{byID: map[uuid.UUID]domain.Project{
		id: {ID: id, OrgID: who.OrgID, Name: "Checkout", Slug: "checkout"},
	}}
	service := newService(projects, &fakeMemberships{}, refuseAll{})

	operations := map[string]func() error{
		"read":        func() error { _, err := service.ByID(context.Background(), who, id); return err },
		"rename":      func() error { _, err := service.Rename(context.Background(), who, id, "New"); return err },
		"delete":      func() error { return service.Delete(context.Background(), who, id) },
		"list member": func() error { _, err := service.Members(context.Background(), who, id); return err },
		"set role": func() error {
			return service.SetMemberRole(context.Background(), who, id, uuid.New(), domain.RoleAdmin)
		},
		"remove member": func() error { return service.RemoveMember(context.Background(), who, id, uuid.New()) },
	}

	for name, call := range operations {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, domain.ErrForbidden) {
				t.Fatalf("got %v, want ErrForbidden", err)
			}
		})
	}

	if len(projects.deleted) > 0 {
		t.Error("a project was deleted despite the refusal")
	}
}

func TestSetMemberRoleRefusesAnUnknownRole(t *testing.T) {
	id := uuid.New()
	memberships := &fakeMemberships{}

	err := newService(&fakeProjects{byID: map[uuid.UUID]domain.Project{}}, memberships, &allowAll{}).
		SetMemberRole(context.Background(), someone(), id, uuid.New(), domain.Role("superuser"))
	if !errors.Is(err, domain.ErrInvalidProject) {
		t.Fatalf("got %v, want ErrInvalidProject", err)
	}
	if len(memberships.set) > 0 {
		t.Error("an unknown role was written")
	}
}

func admin(projectID, userID uuid.UUID) Member {
	return Member{Membership: domain.Membership{
		UserID: userID, ProjectID: projectID, Role: domain.RoleAdmin,
	}}
}

func viewer(projectID, userID uuid.UUID) Member {
	return Member{Membership: domain.Membership{
		UserID: userID, ProjectID: projectID, Role: domain.RoleViewer,
	}}
}

// A project with nobody able to administer it cannot be repaired through this
// API, so the last administrator does not get to leave — not by being removed,
// and not by demoting themselves.
func TestTheLastAdministratorCannotBeLost(t *testing.T) {
	id := uuid.New()
	onlyAdmin := uuid.New()
	who := domain.Identity{UserID: onlyAdmin, OrgID: uuid.New()}

	memberships := &fakeMemberships{members: map[uuid.UUID][]Member{
		id: {admin(id, onlyAdmin), viewer(id, uuid.New())},
	}}
	service := newService(&fakeProjects{byID: map[uuid.UUID]domain.Project{}}, memberships, &allowAll{})

	if err := service.RemoveMember(context.Background(), who, id, onlyAdmin); !errors.Is(err, domain.ErrLastAdministrator) {
		t.Errorf("removing the last administrator: got %v", err)
	}
	if err := service.SetMemberRole(context.Background(), who, id, onlyAdmin, domain.RoleViewer); !errors.Is(err, domain.ErrLastAdministrator) {
		t.Errorf("demoting the last administrator: got %v", err)
	}

	if len(memberships.removed) > 0 || len(memberships.set) > 0 {
		t.Error("the last administrator was changed anyway")
	}
}

func TestASecondAdministratorFreesTheFirst(t *testing.T) {
	id := uuid.New()
	first, second := uuid.New(), uuid.New()
	who := domain.Identity{UserID: first, OrgID: uuid.New()}

	memberships := &fakeMemberships{members: map[uuid.UUID][]Member{
		id: {admin(id, first), admin(id, second)},
	}}
	service := newService(&fakeProjects{byID: map[uuid.UUID]domain.Project{}}, memberships, &allowAll{})

	if err := service.SetMemberRole(context.Background(), who, id, first, domain.RoleViewer); err != nil {
		t.Fatalf("SetMemberRole: %v", err)
	}
	if len(memberships.set) != 1 || memberships.set[0].role != domain.RoleViewer {
		t.Errorf("what was written: %+v", memberships.set)
	}
}

// Somebody who is not an administrator can be removed freely, whoever else is
// in the project.
func TestANonAdministratorCanAlwaysBeRemoved(t *testing.T) {
	id := uuid.New()
	onlyAdmin, plain := uuid.New(), uuid.New()
	who := domain.Identity{UserID: onlyAdmin, OrgID: uuid.New()}

	memberships := &fakeMemberships{members: map[uuid.UUID][]Member{
		id: {admin(id, onlyAdmin), viewer(id, plain)},
	}}
	service := newService(&fakeProjects{byID: map[uuid.UUID]domain.Project{}}, memberships, &allowAll{})

	if err := service.RemoveMember(context.Background(), who, id, plain); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}
	if len(memberships.removed) != 1 || memberships.removed[0] != plain {
		t.Errorf("who was removed: %v", memberships.removed)
	}
}

// A mask stored against a membership is what decides, so somebody whose stored
// mask grants PermManageMembers counts as an administrator for this rule even
// though their role does not say so.
func TestAStoredMaskCountsForTheLastAdministratorRule(t *testing.T) {
	id := uuid.New()
	roleAdmin, maskAdmin := uuid.New(), uuid.New()
	who := domain.Identity{UserID: roleAdmin, OrgID: uuid.New()}

	managesMembers := int64(domain.PermManageMembers)
	memberships := &fakeMemberships{members: map[uuid.UUID][]Member{
		id: {
			admin(id, roleAdmin),
			{Membership: domain.Membership{UserID: maskAdmin, ProjectID: id, Role: domain.RoleViewer, Override: &managesMembers}},
		},
	}}
	service := newService(&fakeProjects{byID: map[uuid.UUID]domain.Project{}}, memberships, &allowAll{})

	if err := service.RemoveMember(context.Background(), who, id, roleAdmin); err != nil {
		t.Fatalf("the stored mask was not counted as an administrator: %v", err)
	}
}

func TestRenameKeepsTheSlug(t *testing.T) {
	id := uuid.New()
	who := someone()

	projects := &fakeProjects{byID: map[uuid.UUID]domain.Project{
		id: {ID: id, OrgID: who.OrgID, Name: "Checkout", Slug: "checkout"},
	}}

	renamed, err := newService(projects, &fakeMemberships{}, &allowAll{}).
		Rename(context.Background(), who, id, "Checkout and payments")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}

	if renamed.Name != "Checkout and payments" {
		t.Errorf("name is %q", renamed.Name)
	}
	if renamed.Slug != "checkout" {
		t.Errorf("the slug moved to %q; URLs and storage keys are filed under it", renamed.Slug)
	}
}
