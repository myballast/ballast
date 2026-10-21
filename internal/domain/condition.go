package domain

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// ErrInvalidCondition reports an expression the grammar refuses.
var ErrInvalidCondition = errors.New("invalid condition")

// ConditionKind is what one piece of a parsed expression is.
type ConditionKind string

// The kinds a piece of a parsed expression can have.
const (
	CondOr       ConditionKind = "or"
	CondAnd      ConditionKind = "and"
	CondCompare  ConditionKind = "compare"
	CondVariable ConditionKind = "variable"
	CondNumber   ConditionKind = "number"
	CondString   ConditionKind = "string"
)

// Condition is a parsed expression.
//
// One struct with a kind rather than an interface per node: the tree is small,
// and this way it reads top to bottom.
type Condition struct {
	Kind  ConditionKind
	Left  *Condition
	Right *Condition

	// Op is the comparison, for CondCompare: ==, !=, <, <=, > or >=.
	Op string
	// Name is the variable, for CondVariable.
	Name string
	// Number is the value, for CondNumber.
	Number float64
	// Text is the value, for CondString.
	Text string
}

// ParseCondition turns an expression written by a user into a tree.
//
// This is the whole reason a condition is not copied into the generated script:
// what comes out of here is a tree Ballast built, so the script can only contain
// comparisons, and, or, variables and literals. Anything else never parses, so
// it never reaches the script.
func ParseCondition(expression string) (*Condition, error) {
	tokens, err := tokenize(expression)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("%w: it is empty", ErrInvalidCondition)
	}

	parser := &conditionParser{tokens: tokens}

	tree, err := parser.parseOr()
	if err != nil {
		return nil, err
	}
	if parser.position != len(parser.tokens) {
		return nil, fmt.Errorf("%w: %q was not expected here",
			ErrInvalidCondition, parser.tokens[parser.position])
	}

	return tree, nil
}

// maxConditionLength keeps one expression from becoming a program.
const maxConditionLength = 500

// tokenize cuts an expression into the pieces the parser reads.
func tokenize(expression string) ([]string, error) {
	if len(expression) > maxConditionLength {
		return nil, fmt.Errorf("%w: it is longer than %d characters",
			ErrInvalidCondition, maxConditionLength)
	}

	var tokens []string
	runes := []rune(expression)

	for i := 0; i < len(runes); {
		current := runes[i]

		switch {
		case unicode.IsSpace(current):
			i++

		case current == '(' || current == ')':
			tokens = append(tokens, string(current))
			i++

		case current == '"':
			text, width, err := readString(runes[i:])
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, text)
			i += width

		case unicode.IsDigit(current) || current == '-':
			number, width := readWhile(runes[i:], func(r rune) bool {
				return unicode.IsDigit(r) || r == '.' || r == '-'
			})
			tokens = append(tokens, number)
			i += width

		case unicode.IsLetter(current) || current == '_':
			name, width := readWhile(runes[i:], func(r rune) bool {
				return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
			})
			tokens = append(tokens, name)
			i += width

		default:
			operator, width, err := readOperator(runes[i:])
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, operator)
			i += width
		}
	}

	return tokens, nil
}

// readString reads a quoted literal. A literal may not contain a quote or a
// backslash: refusing those is simpler than escaping them, and nothing a
// condition needs to say requires either.
func readString(runes []rune) (string, int, error) {
	for i := 1; i < len(runes); i++ {
		switch runes[i] {
		case '"':
			return string(runes[:i+1]), i + 1, nil
		case '\\', '\n', '\r':
			return "", 0, fmt.Errorf("%w: a text value may not contain a backslash or a line break",
				ErrInvalidCondition)
		}
	}

	return "", 0, fmt.Errorf("%w: a text value is not closed", ErrInvalidCondition)
}

// readWhile reads as long as keep says so, and reports how many runes it took.
func readWhile(runes []rune, keep func(rune) bool) (string, int) {
	end := 0
	for end < len(runes) && keep(runes[end]) {
		end++
	}
	return string(runes[:end]), end
}

// operators are the only ones the grammar knows.
var operators = []string{"==", "!=", "<=", ">=", "&&", "||", "<", ">"}

func readOperator(runes []rune) (string, int, error) {
	rest := string(runes)
	for _, operator := range operators {
		if strings.HasPrefix(rest, operator) {
			return operator, len([]rune(operator)), nil
		}
	}

	return "", 0, fmt.Errorf("%w: %q is not part of the grammar",
		ErrInvalidCondition, string(runes[0]))
}

// conditionParser walks the tokens once, left to right.
type conditionParser struct {
	tokens   []string
	position int
}

func (p *conditionParser) next() (string, bool) {
	if p.position >= len(p.tokens) {
		return "", false
	}
	return p.tokens[p.position], true
}

// takeIf consumes the next token when it is the one asked for.
func (p *conditionParser) takeIf(token string) bool {
	if current, ok := p.next(); ok && current == token {
		p.position++
		return true
	}
	return false
}

// parseOr reads a || b || c.
func (p *conditionParser) parseOr() (*Condition, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}

	for p.takeIf("||") {
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &Condition{Kind: CondOr, Left: left, Right: right}
	}

	return left, nil
}

// parseAnd reads a && b && c.
func (p *conditionParser) parseAnd() (*Condition, error) {
	left, err := p.parseComparison()
	if err != nil {
		return nil, err
	}

	for p.takeIf("&&") {
		right, err := p.parseComparison()
		if err != nil {
			return nil, err
		}
		left = &Condition{Kind: CondAnd, Left: left, Right: right}
	}

	return left, nil
}

// parseComparison reads a == b, and also a bare operand such as a variable
// used on its own.
func (p *conditionParser) parseComparison() (*Condition, error) {
	left, err := p.parseOperand()
	if err != nil {
		return nil, err
	}

	current, ok := p.next()
	if !ok || !isComparison(current) {
		return left, nil
	}
	p.position++

	right, err := p.parseOperand()
	if err != nil {
		return nil, err
	}

	return &Condition{Kind: CondCompare, Op: current, Left: left, Right: right}, nil
}

func isComparison(token string) bool {
	switch token {
	case "==", "!=", "<", "<=", ">", ">=":
		return true
	}
	return false
}

// parseOperand reads a variable, a number, a text value or a group.
func (p *conditionParser) parseOperand() (*Condition, error) {
	token, ok := p.next()
	if !ok {
		return nil, fmt.Errorf("%w: it stops in the middle", ErrInvalidCondition)
	}
	p.position++

	if token == "(" {
		inside, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if !p.takeIf(")") {
			return nil, fmt.Errorf("%w: a group is not closed", ErrInvalidCondition)
		}
		return inside, nil
	}

	if strings.HasPrefix(token, `"`) {
		return &Condition{Kind: CondString, Text: strings.Trim(token, `"`)}, nil
	}

	if number, err := strconv.ParseFloat(token, 64); err == nil {
		return &Condition{Kind: CondNumber, Number: number}, nil
	}

	if variableName.MatchString(token) {
		return &Condition{Kind: CondVariable, Name: token}, nil
	}

	return nil, fmt.Errorf("%w: %q is neither a variable, a number nor a text value",
		ErrInvalidCondition, token)
}
