package dto

import (
	"testing"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// admin holds every permission there is. Each one must have a name, or
// GET /projects/{projectId}/permissions would silently leave it out and the
// front end would hide a button the caller is entitled to.
func TestEveryPermissionHasAName(t *testing.T) {
	everything, ok := domain.PermissionsFor(domain.RoleAdmin, nil)
	if !ok {
		t.Fatal("admin has no permission set")
	}

	for bit := 0; bit < 63; bit++ {
		perm := domain.Permission(1) << bit
		if !everything.Has(perm) {
			continue
		}
		if _, ok := PermissionName(perm); !ok {
			t.Errorf("permission bit %d has no API name", bit)
		}
	}
}

func TestPermissionNamesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range permissions {
		if seen[p.name] {
			t.Errorf("%q names two permissions", p.name)
		}
		seen[p.name] = true
	}
}

func TestPermissionNamesListOnlyWhatIsGranted(t *testing.T) {
	set := domain.PermissionSet(domain.PermViewProject) | domain.PermissionSet(domain.PermRunTest)
	got := PermissionNames(set)
	if len(got) != 2 || got[0] != "view_project" || got[1] != "run_test" {
		t.Errorf("PermissionNames = %v, want [view_project run_test]", got)
	}
	if got := PermissionNames(0); len(got) != 0 {
		t.Errorf("an empty set named %v", got)
	}
}
