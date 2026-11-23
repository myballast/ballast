package dto

import "time"

// Webhook is a project's webhook as the screen reads it: where a tool sends its
// requests, and when the secret was made and last replaced. It has no secret in it, and
// nothing that could be one.
type Webhook struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	// Path is the address a tool sends its signed requests to, relative to the API:
	// /webhooks/{organization}/{project}. It is not a secret, and proves nothing.
	Path      string     `json:"path"`
	CreatedAt time.Time  `json:"created_at"`
	RotatedAt *time.Time `json:"rotated_at,omitempty"`
}

// CreatedWebhook is returned when a secret is made or replaced.
type CreatedWebhook struct {
	Webhook Webhook `json:"webhook"`
	// Secret signs the requests. It is shown this once: only its sealed form is
	// stored, and nothing can read it back.
	Secret string `json:"secret"`
}
