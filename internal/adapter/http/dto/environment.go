package dto

import "time"

// Environment is a target a project may send load to.
//
// There is no field for a secret anywhere in this file beyond a key and its
// dates. A value is written to the API and never read back out of it.
type Environment struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	// VerificationStatus is one of pending, proof_ok, verified,
	// verification_failed. Only a verified target may be sent load.
	VerificationStatus string `json:"verification_status"`
	MaxVUs             int    `json:"max_vus"`
	MaxDurationS       int    `json:"max_duration_s"`
	// AllowedWindow is absent when the target takes load at any hour.
	AllowedWindow *AllowedWindow `json:"allowed_window,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
}

// AllowedWindow is the stretch of the day a target accepts load, in its own
// timezone. A start after an end crosses midnight: 22:00 to 06:00.
type AllowedWindow struct {
	Start string `json:"start"`
	End   string `json:"end"`
	// Timezone is an IANA name, such as Europe/Paris.
	Timezone string `json:"timezone"`
}

// EnvironmentListResponse is returned by the environment list.
type EnvironmentListResponse struct {
	Environments []Environment `json:"environments"`
}

// CreateEnvironmentRequest declares a target. It arrives unverified whatever the
// caller sends: nothing here can make an environment verified.
type CreateEnvironmentRequest struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	// MaxVUs and MaxDurationS are the ceilings every run here is clamped to.
	MaxVUs       int `json:"max_vus"`
	MaxDurationS int `json:"max_duration_s"`
	// AllowedWindow is optional. Left out, the target takes load at any hour.
	AllowedWindow *AllowedWindow `json:"allowed_window,omitempty"`
}

// UpdateEnvironmentRequest changes a target. Anything left out stays as it is,
// and allowed_window sent as null removes the window.
//
// Pointing the environment at another origin resets its verification, because the
// proof was served by the old one.
type UpdateEnvironmentRequest struct {
	Name         *string `json:"name,omitempty"`
	BaseURL      *string `json:"base_url,omitempty"`
	MaxVUs       *int    `json:"max_vus,omitempty"`
	MaxDurationS *int    `json:"max_duration_s,omitempty"`
	// AllowedWindow uses a double pointer because "leave it alone" and "there is
	// no window any more" are different instructions, and JSON spells the first
	// as an absent field and the second as null.
	AllowedWindow **AllowedWindow `json:"allowed_window,omitempty"`
}

// Secret is what Ballast will say about a stored credential: its key, and when it
// was set. The value is not returned by any endpoint.
type Secret struct {
	ID        string    `json:"id"`
	Key       string    `json:"key"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SecretListResponse is returned by the secret list: the keys, never the values.
type SecretListResponse struct {
	Secrets []Secret `json:"secrets"`
}

// SetSecretRequest carries the one value that travels into Ballast and never
// back out. The key is in the path.
type SetSecretRequest struct {
	Value string `json:"value"`
}
