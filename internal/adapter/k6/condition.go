package k6

import (
	"fmt"
	"strconv"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// renderCondition writes a parsed expression as JavaScript.
//
// It takes the tree, never the text somebody typed. That is the whole
// arrangement: the grammar in the domain decides what an expression may be, and
// this writes down only what the grammar produced. A field on the canvas cannot
// become code, because the code is built from the tree rather than copied.
func renderCondition(expression string) (string, error) {
	tree, err := domain.ParseCondition(expression)
	if err != nil {
		return "", err
	}

	return writeCondition(tree)
}

func writeCondition(node *domain.Condition) (string, error) {
	switch node.Kind {
	case domain.CondOr:
		return writeBinary(node, "||")

	case domain.CondAnd:
		return writeBinary(node, "&&")

	case domain.CondCompare:
		return writeComparison(node)

	case domain.CondVariable:
		// Through vars, so an expression can only read what the scenario set.
		// Nothing a user writes becomes a bare identifier in the script.
		return "vars[" + jsString(node.Name) + "]", nil

	case domain.CondNumber:
		return strconv.FormatFloat(node.Number, 'f', -1, 64), nil

	case domain.CondString:
		return jsString(node.Text), nil

	default:
		return "", fmt.Errorf("the condition kind %q cannot be written", node.Kind)
	}
}

func writeBinary(node *domain.Condition, operator string) (string, error) {
	left, err := writeCondition(node.Left)
	if err != nil {
		return "", err
	}

	right, err := writeCondition(node.Right)
	if err != nil {
		return "", err
	}

	// Parentheses everywhere rather than working out when they are needed:
	// shorter to read and impossible to get wrong.
	return "(" + left + " " + operator + " " + right + ")", nil
}

// writeComparison writes a == b. The loose operators become strict ones, so
// that "0" and 0 are not the same thing in a scenario's verdict.
func writeComparison(node *domain.Condition) (string, error) {
	operator := node.Op
	switch operator {
	case "==":
		operator = "==="
	case "!=":
		operator = "!=="
	}

	left, err := writeCondition(node.Left)
	if err != nil {
		return "", err
	}

	right, err := writeCondition(node.Right)
	if err != nil {
		return "", err
	}

	return "(" + left + " " + operator + " " + right + ")", nil
}
