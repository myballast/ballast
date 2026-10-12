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
		{
			Method: "DELETE", Path: "/runs/{id}",
			Scope: ScopeDerived, Permission: domain.PermCancelRun,
		},
		// The live log stream. Reading a run's output is reading the run, so it
		// takes the same permission, resolved on every poll rather than once
		// when the stream opens.
		{
			Method: "GET", Path: "/runs/{id}/logs",
			Scope: ScopeDerived, Permission: domain.PermViewProject,
		},
		// The emergency stop. Declared as ScopeAnyProject because its project
		// is not in the path and not in a body — there is none: the service
		// asks the question once per project of the organization and refuses
		// unless the caller administers every one of them. A literal path
		// beats the wildcard above for the same method, and these two differ
		// in method anyway.
		{
			Method: "POST", Path: "/runs/stop-all",
			Scope: ScopeAnyProject, Permission: domain.PermCancelRun,
		},

		// The signed-in user.
		{
			Method: "GET", Path: "/me",
			Scope: ScopeNone,
			Reason: "it reads the caller's own user and the organization their " +
				"session names, nothing that belongs to a project",
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

		// Scenarios. Nested under the project on purpose: the project is in the
		// path, so the middleware built from this table refuses before any
		// handler code matters.
		{
			Method: "GET", Path: "/projects/{projectId}/scenarios",
			Scope: ScopePath, Permission: domain.PermViewProject, ProjectParam: "projectId",
		},
		{
			Method: "POST", Path: "/projects/{projectId}/scenarios",
			Scope: ScopePath, Permission: domain.PermManageScenarios, ProjectParam: "projectId",
		},
		{
			Method: "GET", Path: "/projects/{projectId}/scenarios/{scenarioId}",
			Scope: ScopePath, Permission: domain.PermViewProject, ProjectParam: "projectId",
		},
		{
			Method: "PATCH", Path: "/projects/{projectId}/scenarios/{scenarioId}",
			Scope: ScopePath, Permission: domain.PermManageScenarios, ProjectParam: "projectId",
		},
		{
			Method: "DELETE", Path: "/projects/{projectId}/scenarios/{scenarioId}",
			Scope: ScopePath, Permission: domain.PermManageScenarios, ProjectParam: "projectId",
		},
		{
			Method: "GET", Path: "/projects/{projectId}/scenarios/{scenarioId}/versions",
			Scope: ScopePath, Permission: domain.PermViewProject, ProjectParam: "projectId",
		},
		{
			Method: "GET", Path: "/projects/{projectId}/scenarios/{scenarioId}/thresholds",
			Scope: ScopePath, Permission: domain.PermViewProject, ProjectParam: "projectId",
		},
		{
			Method: "POST", Path: "/projects/{projectId}/scenarios/{scenarioId}/thresholds",
			Scope: ScopePath, Permission: domain.PermManageScenarios, ProjectParam: "projectId",
		},
		{
			Method: "DELETE",
			Path:   "/projects/{projectId}/scenarios/{scenarioId}/thresholds/{thresholdId}",
			Scope:  ScopePath, Permission: domain.PermManageScenarios, ProjectParam: "projectId",
		},

		// Environments, nested under the project for the same reason as the
		// scenarios. The secrets take the permission to manage environments even
		// to be listed: the name of a key says how the target expects to be
		// authenticated, which is not a thing to show every reader of a project.
		{
			Method: "GET", Path: "/projects/{projectId}/environments",
			Scope: ScopePath, Permission: domain.PermViewProject, ProjectParam: "projectId",
		},
		{
			Method: "POST", Path: "/projects/{projectId}/environments",
			Scope: ScopePath, Permission: domain.PermManageEnvironments, ProjectParam: "projectId",
		},
		{
			Method: "GET", Path: "/projects/{projectId}/environments/{environmentId}",
			Scope: ScopePath, Permission: domain.PermViewProject, ProjectParam: "projectId",
		},
		{
			Method: "PATCH", Path: "/projects/{projectId}/environments/{environmentId}",
			Scope: ScopePath, Permission: domain.PermManageEnvironments, ProjectParam: "projectId",
		},
		{
			Method: "DELETE", Path: "/projects/{projectId}/environments/{environmentId}",
			Scope: ScopePath, Permission: domain.PermManageEnvironments, ProjectParam: "projectId",
		},
		{
			Method: "GET", Path: "/projects/{projectId}/environments/{environmentId}/secrets",
			Scope: ScopePath, Permission: domain.PermManageEnvironments, ProjectParam: "projectId",
		},
		{
			Method: "PUT", Path: "/projects/{projectId}/environments/{environmentId}/secrets/{key}",
			Scope: ScopePath, Permission: domain.PermManageEnvironments, ProjectParam: "projectId",
		},
		{
			Method: "DELETE", Path: "/projects/{projectId}/environments/{environmentId}/secrets/{key}",
			Scope: ScopePath, Permission: domain.PermManageEnvironments, ProjectParam: "projectId",
		},

		// Target verification. Proving and approving are deliberately two
		// permissions: a maintainer declares a target and runs the check, and
		// only an admin says load may be sent at it. Were it one right, whoever
		// creates an environment approves their own.
		{
			Method: "GET", Path: "/projects/{projectId}/environments/{environmentId}/verification",
			Scope: ScopePath, Permission: domain.PermManageEnvironments, ProjectParam: "projectId",
		},
		{
			Method: "POST", Path: "/projects/{projectId}/environments/{environmentId}/verify",
			Scope: ScopePath, Permission: domain.PermManageEnvironments, ProjectParam: "projectId",
		},
		{
			Method: "POST", Path: "/projects/{projectId}/environments/{environmentId}/approve",
			Scope: ScopePath, Permission: domain.PermApproveTarget, ProjectParam: "projectId",
		},
	}
}
