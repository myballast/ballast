package dto

import "time"

// Runner is one machine that executes runs, as it last described itself, and
// what that means now.
type Runner struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	// Contract is what the runner speaks; MinimumContract, on the list, is what
	// it is held to.
	Contract int `json:"contract"`
	// Status is read against the clock when the list is asked for, never stored:
	// online, draining or offline.
	Status string `json:"status"`
	// Outdated says the runner is below the minimum and takes no run. It stays
	// online all the same, which is why it is not a status of its own.
	Outdated bool `json:"outdated"`
	// StartedAt is when the process started; LastSeenAt is its last beat.
	StartedAt  time.Time `json:"started_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
}

// RunnerListResponse is the runners of the organization.
type RunnerListResponse struct {
	Runners         []Runner `json:"runners"`
	MinimumContract int      `json:"minimum_contract"`
}
