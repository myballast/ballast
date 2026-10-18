package domain

// Capability is one row of the permissions matrix a person reads before choosing
// a role: a thing they can do, in words, and the permission that grants it.
//
// The matrix is derived from roleMasks, never written out a second time, so that
// the table on screen cannot say something the API does not do. Two permissions
// are left out of it on purpose: cancelling a run goes with running one, and
// managing the project is the administrator's whole role rather than a row.
type Capability struct {
	ID         string
	Label      string
	Permission Permission
}

// Capabilities lists the rows of the matrix, in the order they are shown.
func Capabilities() []Capability {
	return []Capability{
		{"view_runs", "View runs and reports", PermViewProject},
		{"run_test", "Trigger a run", PermRunTest},
		{"edit_scenarios", "Edit scenarios", PermManageScenarios},
		{"manage_environments", "Manage environments", PermManageEnvironments},
		{"verify_domain", "Verify a domain", PermApproveTarget},
		{"manage_members", "Manage members and roles", PermManageMembers},
	}
}

// Grants reports whether role holds the capability.
func (c Capability) Grants(role Role) bool {
	mask, ok := PermissionsFor(role, nil)
	return ok && mask.Has(c.Permission)
}
