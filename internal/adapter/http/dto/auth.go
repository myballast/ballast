package dto

// ProvidersResponse is returned by GET /auth/providers: the identity providers
// a person can sign in with, by the name used in /auth/{provider}/login.
type ProvidersResponse struct {
	Providers []string `json:"providers"`
}

// MeResponse is returned by GET /me: who is signed in, and the organization
// the session acts for.
type MeResponse struct {
	User         MeUser         `json:"user"`
	Organization MeOrganization `json:"organization"`
	// Organizations is every organization the person belongs to, the current one
	// included, oldest first.
	Organizations []MeOrganization `json:"organizations"`
}

// MeUser is the signed-in user.
type MeUser struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// MeOrganization is the organization the session acts for.
type MeOrganization struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
