package dto

import "time"

// AuditEntry is one line of a project's journal.
//
// Identifiers are strings and may be empty, which is how absence is spelled
// here: an entry Ballast wrote itself has no author, and an action on the whole
// organization — an emergency stop — belongs to no project.
type AuditEntry struct {
	Action  string `json:"action"`
	Outcome string `json:"outcome"`
	// ActorID is empty when Ballast acted on nobody's behalf, or when a token did.
	ActorID string `json:"actor_id,omitempty"`
	// ActorTokenID is the API token that acted, when a pipeline did. Never set
	// together with ActorID: an entry with neither is Ballast acting alone.
	ActorTokenID string `json:"actor_token_id,omitempty"`
	// ActorWebhookID is the project's webhook that acted, when a signed request did.
	// Never set together with the other two.
	ActorWebhookID string `json:"actor_webhook_id,omitempty"`
	// ProjectID is empty for an action on the organization.
	ProjectID  string `json:"project_id,omitempty"`
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id,omitempty"`
	// Details holds a small set of keys chosen per action. It never carries a
	// secret, a token or a header.
	Details map[string]string `json:"details"`
	At      time.Time         `json:"at"`
}

// AuditListResponse is returned by the journal of a project.
type AuditListResponse struct {
	Entries []AuditEntry `json:"entries"`
}
