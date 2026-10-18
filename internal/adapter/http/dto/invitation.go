package dto

import "time"

// Capability is one row of the permissions matrix.
type Capability struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// RoleCapabilities is one column of the matrix: a role, and the ids of the
// capabilities it holds.
type RoleCapabilities struct {
	Role         string   `json:"role"`
	Capabilities []string `json:"capabilities"`
}

// RolesResponse is returned by GET /roles: what each role may do, in the order a
// reader compares them — least able first. Derived from the table the
// permission checks themselves use, so that what is shown cannot differ from
// what is enforced.
type RolesResponse struct {
	Capabilities []Capability       `json:"capabilities"`
	Roles        []RoleCapabilities `json:"roles"`
}

// InviteRequest is the body of POST /projects/{projectId}/invitations.
type InviteRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

// Invitation is an invitation that has not been used. It never carries the
// token: that is shown once, when the invitation is created.
type Invitation struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	InvitedBy string `json:"invited_by,omitempty"`
	// State is pending or expired. Used and revoked invitations are not listed.
	State     string    `json:"state"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// InvitationListResponse is returned by GET /projects/{projectId}/invitations.
type InvitationListResponse struct {
	Invitations []Invitation `json:"invitations"`
}

// InvitationCreatedResponse is returned with 201 by POST
// /projects/{projectId}/invitations when an invitation was written.
type InvitationCreatedResponse struct {
	Invitation Invitation `json:"invitation"`
	// Token is the secret in the invitation link, shown this once. Only its hash
	// is stored.
	Token string `json:"token"`
}

// MemberAddedResponse is returned with 200 by POST
// /projects/{projectId}/invitations when the address already belonged to a
// person of the organization: they were added directly and there is no link.
type MemberAddedResponse struct {
	UserID string `json:"user_id"`
}

// InvitationPreview is returned by GET /invitations/{token}: what a link offers,
// read before anybody agrees to it.
type InvitationPreview struct {
	OrganizationName string    `json:"organization_name"`
	ProjectName      string    `json:"project_name"`
	Email            string    `json:"email"`
	Role             string    `json:"role"`
	State            string    `json:"state"`
	ExpiresAt        time.Time `json:"expires_at"`
}

// InvitationAcceptedResponse is returned by POST /invitations/{token}/accept.
// The response also sets a session for the organization that invited, so that
// the next request is already inside it.
type InvitationAcceptedResponse struct {
	OrganizationID string `json:"organization_id"`
	ProjectID      string `json:"project_id"`
}

// SwitchOrganizationRequest is the body of POST /me/organization.
type SwitchOrganizationRequest struct {
	OrganizationID string `json:"organization_id"`
}
