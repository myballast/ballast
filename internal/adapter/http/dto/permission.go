package dto

import "github.com/youssefmahersi/ballast/internal/domain"

// permissions is how each permission is spelled in the API: in the
// documentation of every route, and in GET /projects/{projectId}/permissions.
// They are part of the published contract, so they do not change on a whim.
// In bit order, which is the order they are listed in.
var permissions = []struct {
	perm domain.Permission
	name string
}{
	{domain.PermViewProject, "view_project"},
	{domain.PermRunTest, "run_test"},
	{domain.PermCancelRun, "cancel_run"},
	{domain.PermManageScenarios, "manage_scenarios"},
	{domain.PermManageEnvironments, "manage_environments"},
	{domain.PermApproveTarget, "approve_target"},
	{domain.PermManageMembers, "manage_members"},
	{domain.PermManageProject, "manage_project"},
}

// PermissionName is the API's name for perm, and false for a permission that
// has none.
func PermissionName(perm domain.Permission) (string, bool) {
	for _, p := range permissions {
		if p.perm == perm {
			return p.name, true
		}
	}
	return "", false
}

// PermissionNames lists the permissions set grants, by name, in bit order.
func PermissionNames(set domain.PermissionSet) []string {
	names := make([]string, 0, len(permissions))
	for _, p := range permissions {
		if set.Has(p.perm) {
			names = append(names, p.name)
		}
	}
	return names
}

// AccessResponse is returned by GET /projects/{projectId}/permissions: what
// the caller holds in the project, read from the database when asked.
type AccessResponse struct {
	Role string `json:"role"`
	// Permissions are the caller's effective permissions, by name.
	Permissions []string `json:"permissions"`
	// Custom is true when a mask stored on the membership replaces the role's,
	// so that the role is not mistaken for the whole story.
	Custom bool `json:"custom"`
}
