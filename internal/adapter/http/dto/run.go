// Package dto holds the public request and response shapes of the HTTP API.
//
// They exist so the contract the outside world depends on can change at a
// different pace from the domain behind it. They are the only place in the
// project where JSON field names are part of a promise.
package dto

import "time"

// EnqueueRunRequest is the body of POST /runs.
type EnqueueRunRequest struct {
	ScenarioID    string `json:"scenario_id"`
	EnvironmentID string `json:"environment_id"`
	// Profile is one of smoke, load, stress, spike, soak.
	Profile string `json:"profile"`
	// Params is optional. Left out, the profile's own template is used, and
	// either way the result is clamped to the environment's ceilings.
	Params *ProfileParams `json:"params,omitempty"`
}

// ProfileParams is the shape of the load, in whole seconds.
type ProfileParams struct {
	TargetVUs int `json:"target_vus"`
	RampUpS   int `json:"ramp_up_s"`
	PlateauS  int `json:"plateau_s"`
	RampDownS int `json:"ramp_down_s"`
}

// EnqueueRunResponse is returned by POST /runs.
type EnqueueRunResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// RunResponse is returned by GET /runs/{id}.
type RunResponse struct {
	ID              string        `json:"id"`
	ProjectID       string        `json:"project_id"`
	ScenarioID      string        `json:"scenario_id"`
	EnvironmentID   string        `json:"environment_id"`
	ScenarioVersion int           `json:"scenario_version"`
	Profile         string        `json:"profile"`
	Params          ProfileParams `json:"params"`
	Status          string        `json:"status"`
	ErrorMessage    string        `json:"error_message,omitempty"`
	CreatedAt       time.Time     `json:"created_at"`
	StartedAt       *time.Time    `json:"started_at,omitempty"`
	EndedAt         *time.Time    `json:"ended_at,omitempty"`
	Metrics         []Metric      `json:"metrics"`
	Thresholds      []Threshold   `json:"thresholds"`
}

// Metric is one number the generator measured.
type Metric struct {
	Metric string  `json:"metric"`
	Tag    string  `json:"tag,omitempty"`
	Stat   string  `json:"stat"`
	Value  float64 `json:"value"`
}

// Threshold is the generator's verdict on one rule.
type Threshold struct {
	Metric     string `json:"metric"`
	Expression string `json:"expression"`
	Passed     bool   `json:"passed"`
}

// Error is the body of every failed request.
type Error struct {
	Error string `json:"error"`
}
