package domain

import "errors"

// ErrInvalidGraph reports a scenario graph Ballast refuses to store.
var ErrInvalidGraph = errors.New("invalid scenario graph")

// ScenarioMode says how a scenario version was written.
//
// A scenario in ModeGraph has a script Ballast generated, and that script is
// read only: editing it would make the graph and the code disagree, with
// nothing to say which one a run used.
type ScenarioMode string

// The two ways a scenario can be written.
const (
	ModeCode  ScenarioMode = "code"
	ModeGraph ScenarioMode = "graph"
)

// NodeKind is what a node does. The set is closed: anything else is refused
// when the graph is stored, so the generator never meets a kind it cannot emit.
type NodeKind string

// Every kind of node a graph may hold. Adding one here means teaching the
// generator to write it, which is why the set is closed.
const (
	NodeHTTPRequest     NodeKind = "http_request"
	NodeExtractVariable NodeKind = "extract_variable"
	NodeSetVariable     NodeKind = "set_variable"
	NodeCondition       NodeKind = "condition"
	NodeLoop            NodeKind = "loop"
	NodeThinkTime       NodeKind = "think_time"
	NodeAssertion       NodeKind = "assertion"
	NodeEnd             NodeKind = "end"
)

// MaxLoopIterations is the most times one loop may repeat.
//
// A constant rather than a setting, like the ceilings on virtual users and
// duration: a bound somebody can raise from the outside is a bound an attacker
// raises. A loop of a thousand is already far more than a scenario needs.
const MaxLoopIterations = 1000

// Limits on what a node may carry, so that one scenario cannot produce a script
// too large to store or to read.
const (
	maxNodesPerGraph  = 200
	maxNodeIDLength   = 64
	maxNodeTextLength = 2000
	// maxNodeLabelLength bounds the group and the label of a node, which are
	// shown on the canvas and written as one comment line.
	maxNodeLabelLength = 200
	// maxQueryParams is how many query parameters one request may carry.
	maxQueryParams = 50
)

// Node is one step of a scenario.
//
// One struct for every kind rather than one type each: the fields a kind does
// not use stay empty, and Validate says which ones a kind requires. It keeps
// the JSONB stored on a version readable by eye.
type Node struct {
	ID   string   `json:"id"`
	Kind NodeKind `json:"kind"`

	// Method and Path describe an http_request. Path is a template —
	// /users/{id} — and it is what the request's name tag is built from.
	Method string `json:"method,omitempty"`
	Path   string `json:"path,omitempty"`
	// Body is sent as is, after escaping.
	Body string `json:"body,omitempty"`
	// Headers are sent with the request. Sorted before emission, so that two
	// graphs that differ only in map order produce the same script.
	Headers map[string]string `json:"headers,omitempty"`
	// Query is the query string of the request, in the order it is written. It
	// is kept apart from Path so that the request's name tag stays the path
	// template: with a query in it, every page number would be its own metric.
	Query []QueryParam `json:"query,omitempty"`

	// Variable is the name an extract_variable writes, or a set_variable sets.
	Variable string `json:"variable,omitempty"`
	// Source is where an extract_variable reads in the response: its body, one of
	// its headers, or its status. Empty means the body, which is what every graph
	// stored before there was a choice did.
	Source ExtractSource `json:"source,omitempty"`
	// From says what to read from that source: a path in the JSON body, or the
	// name of a header. It is empty for the status, which has nothing to choose.
	From string `json:"from,omitempty"`
	// Value is what a set_variable stores.
	Value string `json:"value,omitempty"`
	// FromEnv is the environment variable a set_variable reads instead, which
	// is how a scenario uses a secret: the generated script reads __ENV, so the
	// value is never written into a script that anybody can see.
	FromEnv string `json:"from_env,omitempty"`

	// Expression is the test a condition or an assertion makes. It is parsed
	// by the grammar in condition.go, never copied into the script.
	Expression string `json:"expression,omitempty"`

	// Iterations is how many times a loop repeats, bounded by
	// MaxLoopIterations. For a loop over a list it is the most it will take: a
	// list another server returns has no length this scenario can promise, so the
	// maximum is asked for either way.
	Iterations int `json:"iterations,omitempty"`
	// Over names the variable holding a list, and Item the variable that takes
	// each element in turn. Both or neither: empty, the loop just counts.
	Over string `json:"over,omitempty"`
	Item string `json:"item,omitempty"`

	// Seconds is how long a think_time waits; with SecondsMax it is the shortest
	// of a range, and each pass waits a time picked between the two.
	Seconds    float64 `json:"seconds,omitempty"`
	SecondsMax float64 `json:"seconds_max,omitempty"`

	// Group and Label say where a node came from and what its author called it:
	// the folder of an imported collection, the name of a request in it. They
	// are for people, shown on the canvas and written as a comment in the
	// script; nothing is decided from them.
	Group string `json:"group,omitempty"`
	Label string `json:"label,omitempty"`

	// Critical marks a step the scenario cannot continue without. The
	// generator emits fail() after it.
	Critical bool `json:"critical,omitempty"`
}

// QueryParam is one name and value of a query string. The value may hold
// {{variables}}, like a header value; the name is plain text.
type QueryParam struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Edge joins two nodes. A condition has two outgoing edges, told apart by
// When; every other node has one, with When empty.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	// When labels the edges leaving a condition ("true", "false") or a loop ("body", "after").
	When string `json:"when,omitempty"`
}

// ScenarioGraph is what the canvas, the imports and the text editor all produce.
type ScenarioGraph struct {
	Start string `json:"start"`
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// Validate refuses a graph the generator could not turn into a script, or
// should not. It says the first thing wrong; Check says all of them.
//
// Everything here is checked before a version is stored, so the generator can
// assume a graph it is given is sound.
func (g ScenarioGraph) Validate() error {
	if findings := g.Check(); len(findings) > 0 {
		return findings[0].Err
	}
	return nil
}

// StarterGraph is where a scenario written as a graph begins when its author has
// drawn nothing: one request and the end it leads to. A graph with no node
// cannot be stored, so "blank" means the smallest graph that can be.
//
// A fresh value on every call, so that editing one scenario's graph in place can
// never change the next one's starting point.
func StarterGraph() ScenarioGraph {
	return ScenarioGraph{
		Start: "n1",
		Nodes: []Node{
			{ID: "n1", Kind: NodeHTTPRequest, Method: "GET", Path: "/"},
			{ID: "n2", Kind: NodeEnd},
		},
		Edges: []Edge{{From: "n1", To: "n2"}},
	}
}
