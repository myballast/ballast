package dto

import "time"

// Project is a project as the API describes it.
type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Slug appears in URLs and in object storage keys, so it never changes
	// after creation.
	Slug string `json:"slug"`
	// Infra, Region, Owner and Stack are optional details a person wrote. They
	// are always present, empty when not said: an absent field and an empty one
	// would be two ways of saying the same thing.
	Infra  string   `json:"infra"`
	Region string   `json:"region"`
	Owner  string   `json:"owner"`
	Stack  []string `json:"stack"`
	// Favorite is whether the caller has starred it. Their own star: it is never
	// another person's.
	Favorite  bool      `json:"favorite"`
	CreatedAt time.Time `json:"created_at"`
}

// ProjectListResponse is returned by GET /projects.
type ProjectListResponse struct {
	Projects []Project `json:"projects"`
}

// CreateProjectRequest is the body of POST /projects.
type CreateProjectRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
	// The details are optional. Left out or empty, they are not said.
	Infra  string   `json:"infra,omitempty"`
	Region string   `json:"region,omitempty"`
	Owner  string   `json:"owner,omitempty"`
	Stack  []string `json:"stack,omitempty"`
}

// UpdateProjectRequest is the body of PATCH /projects/{projectId}. Anything left
// out stays as it is; an empty string, or an empty stack, says it is not said any
// more. The slug cannot move.
type UpdateProjectRequest struct {
	Name   *string   `json:"name,omitempty"`
	Infra  *string   `json:"infra,omitempty"`
	Region *string   `json:"region,omitempty"`
	Owner  *string   `json:"owner,omitempty"`
	Stack  *[]string `json:"stack,omitempty"`
}

// Member is somebody in a project, with the role that decides what they may do.
type Member struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Name   string `json:"name,omitempty"`
	// Role is one of viewer, runner, maintainer, admin.
	Role string `json:"role"`
	// HasCustomPermissions reports a mask stored against this membership, which
	// replaces the role's. Nothing in Ballast writes one; it is reported so that
	// a role shown here is never mistaken for the whole story.
	HasCustomPermissions bool      `json:"has_custom_permissions"`
	CreatedAt            time.Time `json:"created_at"`
	// LastActivity is the newest entry of the project's journal this person
	// wrote, and null when they have written none.
	LastActivity *time.Time `json:"last_activity"`
}

// MemberListResponse is returned by GET /projects/{projectId}/members.
type MemberListResponse struct {
	Members []Member `json:"members"`
}

// SetMemberRoleRequest is the body of PUT /projects/{projectId}/members/{userId}.
type SetMemberRoleRequest struct {
	Role string `json:"role"`
}
