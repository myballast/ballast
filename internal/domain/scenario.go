package domain

import "github.com/google/uuid"

// Scenario is a versioned k6 script belonging to a project.
//
// The script never hardcodes what it targets or how hard: it reads its base URL
// from the environment it is pointed at, and its load from the profile chosen
// at launch. That is what lets the same script run as a one minute smoke test
// and as a three hour soak.
type Scenario struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	Name      string
	Script    string
	// Version is frozen onto every run made from this scenario.
	Version        int
	DefaultProfile ProfileKind
}
