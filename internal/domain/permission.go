package domain

// Permission is one thing a member may do within a project.
//
// The values are bit positions, and they are frozen. A mask built from them can
// be stored in membership.permissions, so moving a bit is not a rename — it
// silently changes what every mask already stored means. A position is never
// reused either, not even for a permission that is removed: a mask written
// before the removal would come back meaning something else entirely.
type Permission int64

// The permissions, one bit each. Bit positions are part of the stored data.
const (
	// PermViewProject reads a project and everything it measured.
	PermViewProject Permission = 1 << 0
	// PermRunTest launches a run.
	PermRunTest Permission = 1 << 1
	// PermCancelRun stops a run that is under way.
	PermCancelRun Permission = 1 << 2
	// PermManageScenarios edits scenarios and the thresholds they impose.
	PermManageScenarios Permission = 1 << 3
	// PermManageEnvironments declares and edits target environments.
	PermManageEnvironments Permission = 1 << 4
	// PermApproveTarget is the human approval that lets load reach a target.
	PermApproveTarget Permission = 1 << 5
	// PermManageMembers grants and revokes roles.
	PermManageMembers Permission = 1 << 6
	// PermManageProject renames and deletes a project.
	PermManageProject Permission = 1 << 7

	// Bit 8 onwards is unused. Scheduling will take bit 8 when it arrives.
)

// PermissionSet is everything a membership grants.
//
// A separate type from Permission on purpose: the compiler then refuses a whole
// set where a single permission is expected, which is the mistake that would
// otherwise read as an authorization passing when it should not.
type PermissionSet int64

// Has reports whether the set grants perm.
func (s PermissionSet) Has(perm Permission) bool {
	return s&PermissionSet(perm) != 0
}

// The masks, built by composition so that the cumulative rule lives in the code
// rather than only in a test: a permission added to runner reaches maintainer
// and admin without anyone remembering to add it twice.
const (
	viewerMask PermissionSet = PermissionSet(PermViewProject)

	runnerMask = viewerMask | PermissionSet(
		PermRunTest|
			PermCancelRun)

	maintainerMask = runnerMask | PermissionSet(
		PermManageScenarios|
			PermManageEnvironments)

	adminMask = maintainerMask | PermissionSet(
		PermApproveTarget|
			PermManageMembers|
			PermManageProject)
)

var roleMasks = map[Role]PermissionSet{
	RoleViewer:     viewerMask,
	RoleRunner:     runnerMask,
	RoleMaintainer: maintainerMask,
	RoleAdmin:      adminMask,
}

// PermissionsFor is what a membership grants, and whether its role is one
// Ballast knows.
//
// override is membership.permissions. When a value is stored there it replaces
// the role's mask entirely rather than adding to it, because an exception
// written by hand is meant to be exactly what it says.
//
// Nothing in Ballast writes that column: no endpoint and no screen exposes it,
// so in practice the role decides. Reading it anyway is what keeps an exception
// somebody did set from being silently ignored.
func PermissionsFor(role Role, override *int64) (PermissionSet, bool) {
	if override != nil {
		return PermissionSet(*override), true
	}

	mask, ok := roleMasks[role]
	return mask, ok
}
