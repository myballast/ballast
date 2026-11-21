package dto

import "time"

// CreateAPITokenRequest is the body of POST /projects/{projectId}/api-tokens.
type CreateAPITokenRequest struct {
	Name string `json:"name"`
	// ExpiresAt is optional. Left out, the token lives for the default period.
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// APIToken is a token as the screen lists it. It never carries the secret, which
// is shown once at creation, nor the hash it is kept by.
type APIToken struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Prefix is the start of the secret, enough to tell two tokens apart.
	Prefix string `json:"prefix"`
	// CreatedBy is the person's id, and absent when they left. CreatedByName is
	// their name, read when the list is asked for.
	CreatedBy     string `json:"created_by,omitempty"`
	CreatedByName string `json:"created_by_name,omitempty"`
	// State is active, expired or revoked, read against the clock when the list is
	// asked for and never stored.
	State      string     `json:"state"`
	ExpiresAt  time.Time  `json:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// APITokenListResponse is returned by GET /projects/{projectId}/api-tokens.
type APITokenListResponse struct {
	Tokens []APIToken `json:"tokens"`
	// Limit is how many tokens a project may hold that have not been revoked. Said
	// by the API, so that the screen does not carry its own copy of the ceiling.
	Limit int `json:"limit"`
}

// CreatedAPIToken is returned with 201 by POST /projects/{projectId}/api-tokens.
type CreatedAPIToken struct {
	Token APIToken `json:"token"`
	// Secret is the credential itself, shown this once. Only its hash is stored,
	// so it cannot be shown again.
	Secret string `json:"secret"`
}
