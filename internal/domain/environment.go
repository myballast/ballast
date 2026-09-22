package domain

import "github.com/google/uuid"

// Environment is a target a project is allowed to send load to.
//
// Only the two ceilings are modelled here, because they are what a load profile
// is clamped against and nothing else in the domain needs the rest yet. The
// full entity — base URL, verification state, secrets, allowed hosts — belongs
// to the environment use cases and arrives with them.
type Environment struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	Name      string
	// BaseURL is what the scenario reads instead of hardcoding a host.
	BaseURL string
	// VerificationStatus says whether the organization proved it owns this
	// target and an administrator approved it. Anything but Verified means no
	// load may be sent here.
	VerificationStatus VerificationStatus
	// MaxVUs is the most virtual users this environment tolerates.
	MaxVUs int
	// MaxDurationS is the longest a run may last here, in seconds.
	MaxDurationS int
}

// VerificationStatus tracks a target through proof and approval. The two are
// deliberately separate steps: automatic approval falls the day someone finds a
// way to serve a file on a domain that is not theirs.
type VerificationStatus string

// The values stored in environment.verification_status and constrained by
// migration 001.
const (
	VerificationPending  VerificationStatus = "pending"
	VerificationProofOK  VerificationStatus = "proof_ok"
	VerificationVerified VerificationStatus = "verified"
	VerificationFailed   VerificationStatus = "verification_failed"
)

// IsVerified reports whether load may be sent to this target at all.
func (e Environment) IsVerified() bool {
	return e.VerificationStatus == VerificationVerified
}
