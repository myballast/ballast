package domain

import "testing"

// The guard. Each permission is compared to the number it must be, written out
// rather than computed, because the whole point is to catch the day somebody
// reorders the block.
//
// A mask is stored in membership.permissions. Moving a bit does not rename a
// permission, it changes what every mask already in the database means: a
// membership that granted "launch a run" would come back granting "approve a
// target". That is why these numbers are not allowed to move, and why a
// position is never reused even once its permission is gone.
func TestPermissionBitsAreFrozen(t *testing.T) {
	frozen := map[string]struct {
		perm Permission
		want int64
	}{
		"PermViewProject":        {PermViewProject, 1},
		"PermRunTest":            {PermRunTest, 2},
		"PermCancelRun":          {PermCancelRun, 4},
		"PermManageScenarios":    {PermManageScenarios, 8},
		"PermManageEnvironments": {PermManageEnvironments, 16},
		"PermApproveTarget":      {PermApproveTarget, 32},
		"PermManageMembers":      {PermManageMembers, 64},
		"PermManageProject":      {PermManageProject, 128},
	}

	for name, c := range frozen {
		if int64(c.perm) != c.want {
			t.Errorf("%s is %d, and must stay %d: masks already stored would change meaning",
				name, int64(c.perm), c.want)
		}
	}

	if len(frozen) != len(allPermissions()) {
		t.Errorf("%d permissions are frozen here, the package defines %d",
			len(frozen), len(allPermissions()))
	}
}

// Every permission the package defines, for the sweeps below. Written by hand:
// a permission missing from this list is a permission no test covers, which
// TestPermissionBitsAreFrozen turns into a failure.
func allPermissions() []Permission {
	return []Permission{
		PermViewProject,
		PermRunTest,
		PermCancelRun,
		PermManageScenarios,
		PermManageEnvironments,
		PermApproveTarget,
		PermManageMembers,
		PermManageProject,
	}
}

func TestEachPermissionIsASingleBit(t *testing.T) {
	for _, perm := range allPermissions() {
		if perm == 0 {
			t.Error("a permission is zero, so it is granted by nothing and refused by nothing")
			continue
		}
		// A power of two has exactly one bit set.
		if perm&(perm-1) != 0 {
			t.Errorf("permission %d spans more than one bit", int64(perm))
		}
	}
}

func TestNoTwoPermissionsShareABit(t *testing.T) {
	seen := map[Permission]int{}
	for _, perm := range allPermissions() {
		seen[perm]++
	}

	for perm, count := range seen {
		if count > 1 {
			t.Errorf("bit %d is used by %d permissions", int64(perm), count)
		}
	}
}

// The inheritance the use case diagram draws: runner from viewer, maintainer
// from runner, admin from maintainer.
func TestRolesAreCumulative(t *testing.T) {
	ordered := Roles()

	for i := 1; i < len(ordered); i++ {
		lower, higher := ordered[i-1], ordered[i]

		lowerMask, ok := PermissionsFor(lower, nil)
		if !ok {
			t.Fatalf("%s has no mask", lower)
		}
		higherMask, ok := PermissionsFor(higher, nil)
		if !ok {
			t.Fatalf("%s has no mask", higher)
		}

		for _, perm := range allPermissions() {
			if lowerMask.Has(perm) && !higherMask.Has(perm) {
				t.Errorf("%s grants bit %d and %s does not, so the roles are not cumulative",
					lower, int64(perm), higher)
			}
		}

		if higherMask == lowerMask {
			t.Errorf("%s and %s grant exactly the same thing", lower, higher)
		}
	}
}

// Each role is pinned to what it may do, so that widening one is a deliberate
// edit to this table rather than a side effect somewhere else.
func TestWhatEachRoleGrants(t *testing.T) {
	expected := map[Role][]Permission{
		RoleViewer: {PermViewProject},
		RoleRunner: {PermViewProject, PermRunTest, PermCancelRun},
		RoleMaintainer: {
			PermViewProject, PermRunTest, PermCancelRun,
			PermManageScenarios, PermManageEnvironments,
		},
		RoleAdmin: allPermissions(),
	}

	for role, granted := range expected {
		mask, ok := PermissionsFor(role, nil)
		if !ok {
			t.Fatalf("%s has no mask", role)
		}

		allowed := map[Permission]bool{}
		for _, perm := range granted {
			allowed[perm] = true
			if !mask.Has(perm) {
				t.Errorf("%s should grant bit %d and does not", role, int64(perm))
			}
		}
		for _, perm := range allPermissions() {
			if !allowed[perm] && mask.Has(perm) {
				t.Errorf("%s grants bit %d and should not", role, int64(perm))
			}
		}
	}
}

// A role nobody defined grants nothing, and says so. Reading a corrupt value as
// an empty mask that looks legitimate is how a membership silently becomes
// powerless instead of noticed.
func TestAnUnknownRoleIsRefused(t *testing.T) {
	mask, ok := PermissionsFor(Role("superuser"), nil)
	if ok {
		t.Error("an unknown role was accepted")
	}
	if mask != 0 {
		t.Errorf("an unknown role granted %d", int64(mask))
	}

	if Role("superuser").IsValid() {
		t.Error("an unknown role reports itself valid")
	}
	for _, role := range Roles() {
		if !role.IsValid() {
			t.Errorf("%s reports itself invalid", role)
		}
	}
}

// When a mask is stored against a membership it replaces the role's, rather
// than adding to it: an exception written by hand is meant to be exactly what
// it says.
func TestAStoredMaskReplacesTheRole(t *testing.T) {
	override := int64(PermViewProject)

	mask, ok := PermissionsFor(RoleAdmin, &override)
	if !ok {
		t.Fatal("a stored mask was refused")
	}

	if !mask.Has(PermViewProject) {
		t.Error("the stored mask lost what it granted")
	}
	if mask.Has(PermManageMembers) {
		t.Error("the admin role leaked through a stored mask that did not grant it")
	}

	// An empty mask is a real answer: it means this person may do nothing here.
	empty := int64(0)
	if mask, ok := PermissionsFor(RoleAdmin, &empty); !ok || mask != 0 {
		t.Errorf("an empty stored mask gave ok=%t mask=%d", ok, int64(mask))
	}
}
