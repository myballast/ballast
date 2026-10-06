package http

import "github.com/youssefmahersi/ballast/internal/domain"

// Scope says where a route's permission is checked, and by whom.
//
// A permission alone is not a decision: it answers what somebody may do, never
// what to. Every route therefore says how its project is found, and the five
// kinds below are the five situations that actually arise.
type Scope int

const (
	// ScopeZero is no scope at all, which is a declaration nobody finished.
	ScopeZero Scope = iota
	// ScopePublic needs no session: the health check and the sign-in flow.
	ScopePublic
	// ScopeNone is signed in with nothing to refuse. The route must say why.
	ScopeNone
	// ScopeAnyProject covers an action that has no project yet. The service
	// asks the question once per project the caller belongs to, so the check
	// stays scoped even though the request is not.
	ScopeAnyProject
	// ScopePath takes the project from a path value, and the middleware built
	// from this table enforces it. The declaration is not a comment.
	ScopePath
	// ScopeDerived takes the project from a request body or from the row being
	// read, which only the service can resolve.
	ScopeDerived
)

// Route is one entry of the API's surface.
type Route struct {
	Method string
	Path   string
	Scope  Scope
	// Permission is zero for ScopePublic and ScopeNone, and required otherwise.
	Permission domain.Permission
	// ProjectParam names the path value holding the project, for ScopePath.
	ProjectParam string
	// Reason says why a ScopeNone route needs no permission. Written out so
	// that "none" reads as a decision rather than as an omission.
	Reason string
}

// permissionNames is how a permission is spelled in the API documentation.
// They are part of the published contract, so they do not change on a whim.
var permissionNames = map[domain.Permission]string{
	domain.PermViewProject:        "view_project",
	domain.PermRunTest:            "run_test",
	domain.PermCancelRun:          "cancel_run",
	domain.PermManageScenarios:    "manage_scenarios",
	domain.PermManageEnvironments: "manage_environments",
	domain.PermApproveTarget:      "approve_target",
	domain.PermManageMembers:      "manage_members",
	domain.PermManageProject:      "manage_project",
}

// PermissionName is the name this route's permission goes by in the API
// documentation. A route that requires none says so in words — "none" rather
// than nothing at all — so that the documentation cannot be silently missing it.
func (r Route) PermissionName() string {
	if name, ok := permissionNames[r.Permission]; ok {
		return name
	}
	return "none"
}

// Routes is every route the API serves.
//
// This table is the only place a route comes from: NewRouter builds the
// multiplexer by walking it, and a test reads the source of this package to
// refuse any registration made another way. A route cannot therefore be added
// without saying what it requires.
func Routes() []Route {
	return []Route{
		// Public.
		{Method: "GET", Path: "/healthz", Scope: ScopePublic},
		{Method: "GET", Path: "/auth/providers", Scope: ScopePublic},
		{Method: "GET", Path: "/auth/{provider}/login", Scope: ScopePublic},
		{Method: "GET", Path: "/auth/{provider}/callback", Scope: ScopePublic},
		{Method: "POST", Path: "/auth/logout", Scope: ScopePublic},

		// Runs. The project is on the scenario for a run being queued, and on
		// the row itself for one being read; neither is in the path.
		{
			Method: "POST", Path: "/runs",
			Scope: ScopeDerived, Permission: domain.PermRunTest,
		},
		{
			Method: "GET", Path: "/runs/{id}",
			Scope: ScopeDerived, Permission: domain.PermViewProject,
		},

		// Projects.
		{
			Method: "GET", Path: "/projects",
			Scope: ScopeNone,
			Reason: "the query returns the caller's own memberships, so there is " +
				"nothing to refuse: a project they do not belong to is not " +
				"hidden from them, it is not theirs and does not appear",
		},
		{
			Method: "POST", Path: "/projects",
			Scope: ScopeAnyProject, Permission: domain.PermManageProject,
		},
		{
			Method: "GET", Path: "/projects/{projectId}",
			Scope: ScopePath, Permission: domain.PermViewProject, ProjectParam: "projectId",
		},
		{
			Method: "PATCH", Path: "/projects/{projectId}",
			Scope: ScopePath, Permission: domain.PermManageProject, ProjectParam: "projectId",
		},
		{
			Method: "DELETE", Path: "/projects/{projectId}",
			Scope: ScopePath, Permission: domain.PermManageProject, ProjectParam: "projectId",
		},
		{
			Method: "GET", Path: "/projects/{projectId}/members",
			Scope: ScopePath, Permission: domain.PermViewProject, ProjectParam: "projectId",
		},
		{
			Method: "PUT", Path: "/projects/{projectId}/members/{userId}",
			Scope: ScopePath, Permission: domain.PermManageMembers, ProjectParam: "projectId",
		},
		{
			Method: "DELETE", Path: "/projects/{projectId}/members/{userId}",
			Scope: ScopePath, Permission: domain.PermManageMembers, ProjectParam: "projectId",
		},
	}
}
