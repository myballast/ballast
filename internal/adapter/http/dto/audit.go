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
	// ActorID is empty when Ballast acted on nobody's behalf.
	ActorID string `json:"actor_id,omitempty"`
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
