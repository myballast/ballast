package domain

import (
	"errors"
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
