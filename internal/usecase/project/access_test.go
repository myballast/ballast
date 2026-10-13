package project

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// memberAs is an authorizer that holds exactly one membership, or none.
type memberAs struct {
	membership domain.Membership
	found      bool
	err        error
}

func (m memberAs) Can(context.Context, domain.Identity, domain.Permission, uuid.UUID) (bool, error) {
	return false, errors.New("Access must not ask Can: it reads the membership itself")
}

func (m memberAs) CanInAnyProject(context.Context, domain.Identity, domain.Permission) (bool, error) {
	return false, errors.New("Access must not ask CanInAnyProject")
}

func (m memberAs) MembershipOf(context.Context, domain.Identity, uuid.UUID) (domain.Membership, bool, error) {
	return m.membership, m.found, m.err
}

func TestAccessIsWhatTheRoleGrants(t *testing.T) {
	who := someone()
	project := uuid.New()
	service := newService(&fakeProjects{}, &fakeMemberships{}, memberAs{
		membership: domain.Membership{Role: domain.RoleRunner}, found: true,
	})

	got, err := service.Access(context.Background(), who, project)
	if err != nil {
		t.Fatalf("Access: %v", err)
	}

	want, _ := domain.PermissionsFor(domain.RoleRunner, nil)
	if got.Role != domain.RoleRunner || got.Permissions != want || got.Custom {
		t.Errorf("Access = %+v, want runner with %v and no custom mask", got, want)
	}
}

// A mask stored on the membership replaces the role's. The caller is told
// the permissions they actually hold, and that they come from a custom mask,
// so that a role shown next to them is not mistaken for the whole story.
func TestAccessFollowsACustomMask(t *testing.T) {
	mask := int64(domain.PermViewProject)
	service := newService(&fakeProjects{}, &fakeMemberships{}, memberAs{
		membership: domain.Membership{Role: domain.RoleAdmin, Override: &mask}, found: true,
	})

	got, err := service.Access(context.Background(), someone(), uuid.New())
	if err != nil {
		t.Fatalf("Access: %v", err)
	}
	if got.Permissions != domain.PermissionSet(mask) || !got.Custom {
		t.Errorf("Access = %+v, want only view_project, marked custom", got)
	}
}

// Not a member: the same answer as a project that does not exist.
func TestAccessOfANonMemberIsNotFound(t *testing.T) {
	service := newService(&fakeProjects{}, &fakeMemberships{}, memberAs{})

	if _, err := service.Access(context.Background(), someone(), uuid.New()); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Access returned %v, want ErrNotFound", err)
	}
}

// A role this build does not know grants nothing, here as everywhere.
func TestAccessOfAnUnknownRoleGrantsNothing(t *testing.T) {
	service := newService(&fakeProjects{}, &fakeMemberships{}, memberAs{
		membership: domain.Membership{Role: "owner"}, found: true,
	})

	got, err := service.Access(context.Background(), someone(), uuid.New())
	if err != nil {
		t.Fatalf("Access: %v", err)
	}
	if got.Permissions != 0 {
		t.Errorf("an unknown role was granted %v", got.Permissions)
	}
}
