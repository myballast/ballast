package run

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// parsing stands in for the inspector: it refuses what it is told to.
type parsing struct{ refuse bool }

func (p parsing) Inspect(string) domain.ScriptReport {
	if p.refuse {
		return domain.ScriptReport{Findings: []domain.ScriptFinding{{
			Severity: domain.ScriptError, Code: domain.ScriptUnparsable, Message: "the script is not valid JavaScript",
		}}}
	}
	return domain.ScriptReport{}
}

func withGraph(graph domain.ScenarioGraph) func(*domain.Scenario, *domain.Environment) {
	return func(s *domain.Scenario, _ *domain.Environment) {
		s.Mode, s.Graph = domain.ModeGraph, &graph
	}
}

// goodGraph and cyclicGraph are a graph that passes today's rules, and one saved
// before the rule on cycles existed.
func goodGraph() domain.ScenarioGraph { return domain.StarterGraph() }

func cyclicGraph() domain.ScenarioGraph {
	return domain.ScenarioGraph{
		Start: "a",
		Nodes: []domain.Node{
			{ID: "a", Kind: domain.NodeHTTPRequest, Method: "GET", Path: "/a"},
			{ID: "b", Kind: domain.NodeThinkTime, Seconds: 1},
			{ID: "end", Kind: domain.NodeEnd},
		},
		Edges: []domain.Edge{{From: "a", To: "b"}, {From: "b", To: "a"}},
	}
}

// A graph that was stored under older rules, and would be refused now, does not
// start: the run is the moment it would cost something.
func TestAGraphThatIsNoLongerValidCannotBeRun(t *testing.T) {
	f := newFixture(t, withGraph(cyclicGraph()))

	_, err := f.service.Enqueue(context.Background(), f.command())

	if !errors.Is(err, domain.ErrInvalidGraph) {
		t.Fatalf("got %v, wanted ErrInvalidGraph", err)
	}
	var refused *domain.GraphRefused
	if !errors.As(err, &refused) || len(refused.Findings) == 0 {
		t.Fatalf("the refusal does not carry the reasons: %v", err)
	}
	if f.runs.created.ID != [16]byte{} {
		t.Error("a run was created for a graph that is not valid")
	}
}

func TestAGraphThatIsValidIsRun(t *testing.T) {
	f := newFixture(t, withGraph(goodGraph()))
	f.service.CheckingScriptsWith(parsing{})

	if _, err := f.service.Enqueue(context.Background(), f.command()); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
}

// k6 would not load a script that does not parse, and the run would end in an
// error nobody can read. It is found before the run exists.
func TestAScriptThatDoesNotParseCannotBeRun(t *testing.T) {
	for name, mutate := range map[string]func(*domain.Scenario, *domain.Environment){
		"a graph": withGraph(goodGraph()),
		"code":    nil,
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, mutate)
			f.service.CheckingScriptsWith(parsing{refuse: true})

			_, err := f.service.Enqueue(context.Background(), f.command())

			if !errors.Is(err, domain.ErrInvalidScript) {
				t.Fatalf("got %v, wanted ErrInvalidScript", err)
			}
			if f.runs.created.ID != [16]byte{} {
				t.Error("a run was created for a script that does not parse")
			}
		})
	}
}

// Without an inspector the check is simply not made, which is what the tests of
// everything else rely on.
func TestWithoutAnInspectorTheScriptIsNotParsed(t *testing.T) {
	f := newFixture(t, nil)

	if _, err := f.service.Enqueue(context.Background(), f.command()); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
}

// Repeating a run executes the version it froze, so that is the version looked
// over: the scenario may have been fixed since, or broken.
func TestRepeatingARunLooksOverTheVersionItFroze(t *testing.T) {
	f := newFixture(t, nil)
	f.service.CheckingScriptsWith(parsing{refuse: true})

	source := domain.Run{
		ID: uuid.New(), ScenarioID: f.scenario.ID, ScenarioVersion: 1, ProjectID: f.scenario.ProjectID,
		EnvironmentID: f.env.ID, Profile: domain.ProfileSmoke, ProfileParams: domain.ProfileParams{TargetVUs: 1, PlateauS: 5},
	}
	f.runs.byID = map[uuid.UUID]domain.Run{source.ID: source}

	_, err := f.service.Rerun(context.Background(), f.actor, source.ID)
	if !errors.Is(err, domain.ErrInvalidScript) {
		t.Fatalf("got %v, wanted ErrInvalidScript from the frozen version", err)
	}
}
