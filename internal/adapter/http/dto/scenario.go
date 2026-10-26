package dto

import "time"

// ScenarioSummary is a scenario without its script, for a list.
type ScenarioSummary struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version int    `json:"version"`
	// DefaultProfile is one of smoke, load, stress, spike, soak.
	DefaultProfile string        `json:"default_profile"`
	DefaultParams  ProfileParams `json:"default_params"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

// ScenarioListItem is a scenario as the list shows it.
//
// The count lives here rather than on ScenarioSummary, which the full scenario
// embeds: read one at a time, a scenario's rules come from its own endpoint,
// and a count of zero there would be a claim nobody checked.
type ScenarioListItem struct {
	ScenarioSummary
	// ThresholdCount is how many rules the scenario imposes.
	ThresholdCount int `json:"threshold_count"`
}

// ScenarioListResponse is returned by the scenario list.
type ScenarioListResponse struct {
	Scenarios []ScenarioListItem `json:"scenarios"`
}

// Scenario is one scenario, with its script and what inspecting it noticed.
type Scenario struct {
	ScenarioSummary
	Script string `json:"script"`
	// Mode is "code" or "graph". A graph scenario's script is generated, and
	// sending one back is refused: it is here so an interface can say so before
	// somebody types into a field that will not save.
	Mode string `json:"mode"`
	// Graph is the graph of a scenario whose mode is "graph", and absent for
	// one written as code.
	Graph *ScenarioGraph `json:"graph,omitempty"`
	// Warnings are worth reading before launching a run. Anything that would
	// refuse the script is an error on the request that tried to store it, not a
	// warning here.
	Warnings []ScriptFinding `json:"warnings"`
}

// ScriptFinding is one thing inspecting a script noticed.
type ScriptFinding struct {
	// Code is stable; the message is not. Key on the code.
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
	// Line counts from one, and is absent when the finding is about the script
	// as a whole.
	Line int `json:"line,omitempty"`
}

// CreateScenarioRequest is the body that creates a scenario.
type CreateScenarioRequest struct {
	Name string `json:"name"`
	// Mode is "code" (the default) or "graph". A graph scenario starts from a
	// single request and an end, and its script is generated: sending one is
	// refused.
	Mode   string `json:"mode,omitempty"`
	Script string `json:"script,omitempty"`
	// DefaultProfile defaults to smoke.
	DefaultProfile string `json:"default_profile,omitempty"`
	// DefaultParams defaults to the profile's own template.
	DefaultParams *ProfileParams `json:"default_params,omitempty"`
}

// UpdateScenarioRequest changes a scenario. Anything left out stays as it is.
//
// Changing the script writes a new version; renaming does not, because a name is
// not something a run executed.
type UpdateScenarioRequest struct {
	Name           *string        `json:"name,omitempty"`
	Script         *string        `json:"script,omitempty"`
	DefaultProfile *string        `json:"default_profile,omitempty"`
	DefaultParams  *ProfileParams `json:"default_params,omitempty"`
	// Graph replaces the graph of a scenario whose mode is "graph"; the script
	// is generated from it. Sending it together with a script is refused.
	Graph *ScenarioGraph `json:"graph,omitempty"`
}

// ValidateGraphRequest is the body that asks whether a graph would be accepted.
type ValidateGraphRequest struct {
	Graph ScenarioGraph `json:"graph"`
}

// ScenarioVersion is one version of a script, kept for good.
type ScenarioVersion struct {
	Version        int           `json:"version"`
	Script         string        `json:"script"`
	DefaultProfile string        `json:"default_profile"`
	DefaultParams  ProfileParams `json:"default_params"`
	// CreatedBy is absent once the person who wrote it has been removed.
	CreatedBy string    `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// ScenarioVersionListResponse is returned by the version list, newest first.
type ScenarioVersionListResponse struct {
	Versions []ScenarioVersion `json:"versions"`
}

// ThresholdRule is a pass or fail rule on one k6 metric.
type ThresholdRule struct {
	ID string `json:"id"`
	// Metric accepts k6's tagged syntax: http_req_duration{name:POST /login}
	// narrows a rule to one endpoint.
	Metric     string `json:"metric"`
	Expression string `json:"expression"`
}

// ThresholdListResponse is returned by the threshold list.
type ThresholdListResponse struct {
	Thresholds []ThresholdRule `json:"thresholds"`
}

// CreateThresholdRequest imposes a rule.
type CreateThresholdRequest struct {
	Metric     string `json:"metric"`
	Expression string `json:"expression"`
}

// UpdateThresholdRequest changes a rule in place. Anything left out stays as it
// is. Runs already recorded keep the verdicts they were given.
type UpdateThresholdRequest struct {
	Metric     *string `json:"metric,omitempty"`
	Expression *string `json:"expression,omitempty"`
}

// GraphQueryParam is one name and value of a request's query string.
type GraphQueryParam struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// GraphNode is one step of a graph scenario. One shape for every kind: the
// fields a kind does not use stay absent.
type GraphNode struct {
	ID         string            `json:"id"`
	Kind       string            `json:"kind"`
	Method     string            `json:"method,omitempty"`
	Path       string            `json:"path,omitempty"`
	Body       string            `json:"body,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	Query      []GraphQueryParam `json:"query,omitempty"`
	Variable   string            `json:"variable,omitempty"`
	Source     string            `json:"source,omitempty"`
	From       string            `json:"from,omitempty"`
	Value      string            `json:"value,omitempty"`
	FromEnv    string            `json:"from_env,omitempty"`
	Expression string            `json:"expression,omitempty"`
	Iterations int               `json:"iterations,omitempty"`
	Over       string            `json:"over,omitempty"`
	Item       string            `json:"item,omitempty"`
	Seconds    float64           `json:"seconds,omitempty"`
	SecondsMax float64           `json:"seconds_max,omitempty"`
	Group      string            `json:"group,omitempty"`
	Label      string            `json:"label,omitempty"`
	Critical   bool              `json:"critical,omitempty"`
}

// GraphEdge joins two nodes. When is "true" or "false" on the edges leaving a
// condition.
type GraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	When string `json:"when,omitempty"`
}

// ScenarioGraph is the graph of a scenario written as a graph.
type ScenarioGraph struct {
	Start string      `json:"start"`
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}
