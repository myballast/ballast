package dto

import "time"

// Project is a project as the API describes it.
type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Slug appears in URLs and in object storage keys, so it never changes
	// after creation.
	Slug      string    `json:"slug"`
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
}

// UpdateProjectRequest is the body of PATCH /projects/{projectId}. Only the
// display name can move.
type UpdateProjectRequest struct {
	Name string `json:"name"`
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
