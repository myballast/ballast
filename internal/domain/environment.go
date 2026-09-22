package domain

// Environment is a target a project is allowed to send load to.
//
// Only the two ceilings are modelled here, because they are what a load profile
// is clamped against and nothing else in the domain needs the rest yet. The
// full entity — base URL, verification state, secrets, allowed hosts — belongs
// to the environment use cases and arrives with them.
type Environment struct {
	// MaxVUs is the most virtual users this environment tolerates.
	MaxVUs int
	// MaxDurationS is the longest a run may last here, in seconds.
	MaxDurationS int
}
