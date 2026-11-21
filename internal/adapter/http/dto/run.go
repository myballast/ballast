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
	// CI is what a pipeline says about itself when it launches the run. Optional,
	// and declared by the caller: nobody proves it to Ballast.
	CI *RunCI `json:"ci,omitempty"`
}

// RunCI is the context a pipeline launched a run in: which CI, which repository,
// branch and commit, and the link to the job. A closed set of five fields; only the
// provider is required. Absence is spelled by leaving a key out.
type RunCI struct {
	// Provider is github_actions, gitlab_ci or other.
	Provider string `json:"provider"`
	// Repository is a path of plain names, like owner/name.
	Repository string `json:"repository,omitempty"`
	Branch     string `json:"branch,omitempty"`
	// Commit is a hexadecimal hash of 7 to 64 characters, in lower case.
	Commit string `json:"commit,omitempty"`
	// JobURL is an https address, without credentials, query or fragment.
	JobURL string `json:"job_url,omitempty"`
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

// CancelRunResponse is returned by DELETE /runs/{id}.
//
// The outcome is the point of the body: a queued run is over when the request
// returns, and a run being executed has been asked to stop and will be over
// within a few seconds. Saying only "ok" would make the two look alike.
type CancelRunResponse struct {
	ID string `json:"id"`
	// Status is the run's status now: cancelled for a run that had not
	// started, running for one being stopped.
	Status string `json:"status"`
	// Outcome is cancelled or cancellation_requested.
	Outcome string `json:"outcome"`
}

// StopAllResponse is returned by POST /runs/stop-all.
type StopAllResponse struct {
	// Cancelled is how many queued runs were cancelled outright.
	Cancelled int `json:"cancelled"`
	// AskedToStop is how many running runs were asked to stop. Each is ended by
	// the worker executing it, once it has destroyed its container.
	AskedToStop int `json:"asked_to_stop"`
}

// RunResponse is returned by GET /runs/{id}.
type RunResponse struct {
	ID string `json:"id"`
	// Number is how the run is spoken of within its project: #1482.
	Number          int           `json:"number"`
	ProjectID       string        `json:"project_id"`
	ScenarioID      string        `json:"scenario_id"`
	EnvironmentID   string        `json:"environment_id"`
	ScenarioVersion int           `json:"scenario_version"`
	Profile         string        `json:"profile"`
	Params          ProfileParams `json:"params"`
	Status          string        `json:"status"`
	ErrorMessage    string        `json:"error_message,omitempty"`
	// TriggeredBy is the name of who started the run, absent when nobody did or
	// the person has since been removed.
	TriggeredBy string `json:"triggered_by,omitempty"`
	// CI is what the pipeline that launched the run said about itself, absent for
	// a run nobody said anything about. Declared by the caller and not proved to
	// Ballast, and frozen with the run.
	CI *RunCI `json:"ci,omitempty"`
	// CancelRequestedAt is set as soon as somebody asks the run to stop, and
	// before it has stopped: a client watching a cancellation reads it here.
	CancelRequestedAt *time.Time  `json:"cancel_requested_at,omitempty"`
	CreatedAt         time.Time   `json:"created_at"`
	StartedAt         *time.Time  `json:"started_at,omitempty"`
	EndedAt           *time.Time  `json:"ended_at,omitempty"`
	Metrics           []Metric    `json:"metrics"`
	Thresholds        []Threshold `json:"thresholds"`
}

// RunListResponse is returned by GET /projects/{projectId}/runs.
type RunListResponse struct {
	Runs []RunSummary `json:"runs"`
	// Next is passed back as ?before= to read the following, older page.
	// Absent on the last page. Opaque: its content is not part of the contract.
	Next string `json:"next,omitempty"`
	// Prev is passed back as ?after= to read the page of newer runs. Absent
	// on the first page. Opaque, like Next.
	Prev string `json:"prev,omitempty"`
}

// RunSummary is a run as a list shows it: what was run, against what, and how
// it ended. Its metrics and verdicts are read one run at a time, from
// GET /runs/{id}.
type RunSummary struct {
	ID              string        `json:"id"`
	Number          int           `json:"number"`
	ProjectID       string        `json:"project_id"`
	ScenarioID      string        `json:"scenario_id"`
	EnvironmentID   string        `json:"environment_id"`
	ScenarioVersion int           `json:"scenario_version"`
	Profile         string        `json:"profile"`
	Params          ProfileParams `json:"params"`
	Status          string        `json:"status"`
	CreatedAt       time.Time     `json:"created_at"`
	StartedAt       *time.Time    `json:"started_at,omitempty"`
	EndedAt         *time.Time    `json:"ended_at,omitempty"`
	// TriggeredBy is the name of who started the run, absent when nobody did or
	// the person has since been removed.
	TriggeredBy string `json:"triggered_by,omitempty"`
	// P95Ms and ErrorRate are the run's headline numbers as k6 reported them:
	// the 95th percentile of the request duration in milliseconds, and the share
	// of requests that failed as a ratio between 0 and 1. Absent for a run that
	// has no results yet.
	P95Ms     *float64 `json:"p95_ms,omitempty"`
	ErrorRate *float64 `json:"error_rate,omitempty"`
}

// LogLine is one line of GET /runs/{id}/logs, carried as an event's data.
//
// Seq is also the event's id, which is what a browser sends back as
// Last-Event-ID when it reconnects — so the number in the body and the number
// the resumption is based on cannot drift apart.
type LogLine struct {
	Seq     int64     `json:"seq"`
	Stream  string    `json:"stream"`
	Message string    `json:"message"`
	At      time.Time `json:"at"`
}

// RunEnded is the last event of a log stream: the run is over, and this is how
// it ended.
type RunEnded struct {
	Status string `json:"status"`
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

// ProjectStatsResponse is returned by GET /projects/{projectId}/stats: the five
// figures of a project over its last days, each set against the same number of
// days before. The days are whole UTC days, today included.
type ProjectStatsResponse struct {
	// Days is how many days each window holds, as asked and bounded.
	Days int `json:"days"`
	// From and To bound the current window: From included, To excluded.
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	// PassRate is the share of runs that ended with a verdict and passed, as a
	// ratio. Its change is in percentage points.
	PassRate StatsMeasure `json:"pass_rate"`
	// FailedRuns counts runs that failed, errored or timed out. Its change is
	// the difference in runs.
	FailedRuns StatsMeasure `json:"failed_runs"`
	// TotalRuns counts every run created. Its change is a percentage.
	TotalRuns StatsMeasure `json:"total_runs"`
	// AvgDurationS is how long a run that ended with a verdict lasted, in
	// seconds. Its change is the difference in seconds.
	AvgDurationS StatsMeasure `json:"avg_duration_s"`
	// P95Ms is the mean of the runs' 95th percentile of the request duration,
	// in milliseconds. Its change is the difference in milliseconds.
	P95Ms StatsMeasure `json:"p95_ms"`
}

// StatsMeasure is one figure. A null is "nothing to say" and never zero: a
// window with no verdict has no pass rate, and a change against an empty window
// is not a number. The fields are always present, as null when they are.
type StatsMeasure struct {
	Value    *float64 `json:"value"`
	Previous *float64 `json:"previous"`
	Change   *float64 `json:"change"`
	// Series has a point for each day of the current window, the oldest first.
	Series []*float64 `json:"series"`
}
