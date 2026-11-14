package authz

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

type fakeMemberships struct {
	// byProject is what the person holds, project by project.
	byProject map[uuid.UUID]domain.Membership
	err       error
}

func (f fakeMemberships) OfMember(
	_ context.Context, _ domain.Identity, projectID uuid.UUID,
) (domain.Membership, bool, error) {
	if f.err != nil {
		return domain.Membership{}, false, f.err
	}
	membership, ok := f.byProject[projectID]
	return membership, ok, nil
}

func (f fakeMemberships) AllOfMember(
	_ context.Context, _ domain.Identity,
) ([]domain.Membership, error) {
	if f.err != nil {
		return nil, f.err
	}

	all := make([]domain.Membership, 0, len(f.byProject))
	for _, membership := range f.byProject {
		all = append(all, membership)
	}
	return all, nil
}

func newService(memberships Memberships) *Service {
	return NewService(memberships, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func someone() domain.Identity {
	return domain.Identity{UserID: uuid.New(), OrgID: uuid.New()}
}

func TestCanFollowsTheRole(t *testing.T) {
	project := uuid.New()
	who := someone()

	cases := []struct {
		role    domain.Role
		perm    domain.Permission
		allowed bool
	}{
		{domain.RoleViewer, domain.PermViewProject, true},
		{domain.RoleViewer, domain.PermRunTest, false},
		{domain.RoleRunner, domain.PermRunTest, true},
		{domain.RoleRunner, domain.PermManageScenarios, false},
		{domain.RoleMaintainer, domain.PermManageScenarios, true},
		{domain.RoleMaintainer, domain.PermManageMembers, false},
		{domain.RoleAdmin, domain.PermManageMembers, true},
		{domain.RoleAdmin, domain.PermViewProject, true},
	}

	for _, c := range cases {
		t.Run(string(c.role), func(t *testing.T) {
			service := newService(fakeMemberships{byProject: map[uuid.UUID]domain.Membership{
				project: {UserID: who.UserID, ProjectID: project, Role: c.role},
			}})

			allowed, err := service.Can(context.Background(), who, c.perm, project)
			if err != nil {
				t.Fatalf("Can: %v", err)
			}
			if allowed != c.allowed {
				t.Errorf("%s with bit %d: got %t, want %t", c.role, int64(c.perm), allowed, c.allowed)
			}
		})
	}
}

// The right role in the wrong project grants nothing. This is the mistake a
// scope-less check would hide, and why the project treats one as a bug.
func TestCanIsScopedToOneProject(t *testing.T) {
	who := someone()
	theirs, somebodyElses := uuid.New(), uuid.New()

	service := newService(fakeMemberships{byProject: map[uuid.UUID]domain.Membership{
		theirs: {UserID: who.UserID, ProjectID: theirs, Role: domain.RoleAdmin},
	}})

	allowed, err := service.Can(context.Background(), who, domain.PermManageMembers, theirs)
	if err != nil || !allowed {
		t.Fatalf("their own project: allowed=%t err=%v", allowed, err)
	}

	allowed, err = service.Can(context.Background(), who, domain.PermManageMembers, somebodyElses)
	if err != nil {
		t.Fatalf("Can: %v", err)
	}
	if allowed {
		t.Error("an administrator of one project was granted the same in another")
	}
}

// No membership is a refusal, not an error: the person is simply not there.
func TestCanRefusesWithoutAMembership(t *testing.T) {
	service := newService(fakeMemberships{byProject: map[uuid.UUID]domain.Membership{}})

	allowed, err := service.Can(context.Background(), someone(), domain.PermViewProject, uuid.New())
	if err != nil {
		t.Fatalf("Can: %v", err)
	}
	if allowed {
		t.Error("somebody with no membership was allowed")
	}
}

// A check with no project is a programming error. Answering false would read as
// "not allowed" and leave the bug in place.
func TestCanRefusesAScopelessCheck(t *testing.T) {
	service := newService(fakeMemberships{})

	if _, err := service.Can(context.Background(), someone(), domain.PermViewProject, uuid.Nil); !errors.Is(err, ErrNoScope) {
		t.Fatalf("got %v, want ErrNoScope", err)
	}
}

func TestCanRefusesWithoutAnIdentity(t *testing.T) {
	service := newService(fakeMemberships{})
	project := uuid.New()

	cases := map[string]domain.Identity{
		"nothing":         {},
		"no organization": {UserID: uuid.New()},
		"no user":         {OrgID: uuid.New()},
	}

	for name, who := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := service.Can(context.Background(), who, domain.PermViewProject, project); !errors.Is(err, ErrNoActor) {
				t.Fatalf("got %v, want ErrNoActor", err)
			}
		})
	}
}

// A mask stored against the membership replaces the role's.
func TestCanHonoursAStoredMask(t *testing.T) {
	who := someone()
	project := uuid.New()
	onlyViewing := int64(domain.PermViewProject)

	service := newService(fakeMemberships{byProject: map[uuid.UUID]domain.Membership{
		project: {UserID: who.UserID, ProjectID: project, Role: domain.RoleAdmin, Override: &onlyViewing},
	}})

	allowed, err := service.Can(context.Background(), who, domain.PermManageMembers, project)
	if err != nil {
		t.Fatalf("Can: %v", err)
	}
	if allowed {
		t.Error("the admin role leaked through a stored mask that did not grant it")
	}

	if allowed, err := service.Can(context.Background(), who, domain.PermViewProject, project); err != nil || !allowed {
		t.Errorf("the stored mask lost what it granted: allowed=%t err=%v", allowed, err)
	}
}

// A role this build does not know grants nothing. A row holding one means the
// database carries something no version of this code wrote.
func TestCanRefusesAnUnknownRole(t *testing.T) {
	who := someone()
	project := uuid.New()

	service := newService(fakeMemberships{byProject: map[uuid.UUID]domain.Membership{
		project: {UserID: who.UserID, ProjectID: project, Role: domain.Role("superuser")},
	}})

	for _, perm := range []domain.Permission{domain.PermViewProject, domain.PermManageMembers} {
		allowed, err := service.Can(context.Background(), who, perm, project)
		if err != nil {
			t.Fatalf("Can: %v", err)
		}
		if allowed {
			t.Errorf("an unknown role granted bit %d", int64(perm))
		}
	}
}

func TestCanInAnyProject(t *testing.T) {
	who := someone()
	viewerIn, adminIn := uuid.New(), uuid.New()

	onlyViewer := fakeMemberships{byProject: map[uuid.UUID]domain.Membership{
		viewerIn: {UserID: who.UserID, ProjectID: viewerIn, Role: domain.RoleViewer},
	}}
	alsoAdmin := fakeMemberships{byProject: map[uuid.UUID]domain.Membership{
		viewerIn: {UserID: who.UserID, ProjectID: viewerIn, Role: domain.RoleViewer},
		adminIn:  {UserID: who.UserID, ProjectID: adminIn, Role: domain.RoleAdmin},
	}}

	allowed, err := newService(onlyViewer).CanInAnyProject(context.Background(), who, domain.PermManageProject)
	if err != nil {
		t.Fatalf("CanInAnyProject: %v", err)
	}
	if allowed {
		t.Error("a viewer was allowed to create a project")
	}

	allowed, err = newService(alsoAdmin).CanInAnyProject(context.Background(), who, domain.PermManageProject)
	if err != nil {
		t.Fatalf("CanInAnyProject: %v", err)
	}
	if !allowed {
		t.Error("an administrator of one project was refused")
	}
}

// A failure to read is not a refusal. Reporting false would turn a database
// problem into a quiet permission denial.
func TestCanReportsAReadFailure(t *testing.T) {
	boom := errors.New("the database is down")
	service := newService(fakeMemberships{err: boom})

	if _, err := service.Can(context.Background(), someone(), domain.PermViewProject, uuid.New()); !errors.Is(err, boom) {
		t.Fatalf("got %v, want the read error", err)
	}
	if _, err := service.CanInAnyProject(context.Background(), someone(), domain.PermViewProject); !errors.Is(err, boom) {
		t.Fatalf("got %v, want the read error", err)
	}
}

func TestMembershipOfReturnsTheCallersOwnMembership(t *testing.T) {
	project := uuid.New()
	held := domain.Membership{ProjectID: project, Role: domain.RoleRunner}
	service := newService(fakeMemberships{byProject: map[uuid.UUID]domain.Membership{project: held}})

	got, found, err := service.MembershipOf(context.Background(), someone(), project)
	if err != nil || !found || got != held {
		t.Errorf("MembershipOf = %+v, %v, %v; want %+v", got, found, err, held)
	}

	_, found, err = service.MembershipOf(context.Background(), someone(), uuid.New())
	if err != nil || found {
		t.Errorf("another project: found %v, %v; want not found", found, err)
	}
}

// The same guard as Can: a question with no actor or no project is a bug.
func TestMembershipOfRequiresAnActorAndAProject(t *testing.T) {
	service := newService(fakeMemberships{})

	if _, _, err := service.MembershipOf(context.Background(), domain.Identity{}, uuid.New()); err == nil {
		t.Error("a question with no actor was answered")
	}
	if _, _, err := service.MembershipOf(context.Background(), someone(), uuid.Nil); err == nil {
		t.Error("a question with no project was answered")
	}
}
