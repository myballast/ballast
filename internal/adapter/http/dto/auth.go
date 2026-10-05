package dto

// ProvidersResponse is returned by GET /auth/providers: the identity providers
// a person can sign in with, by the name used in /auth/{provider}/login.
type ProvidersResponse struct {
	Providers []string `json:"providers"`
}
