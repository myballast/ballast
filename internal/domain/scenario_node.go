package domain

import (
	"fmt"
	"regexp"
	"strings"
)

// nodeID is the shape of a node identifier. It ends up in a JavaScript
// comment and in a variable name, so it is kept to characters that cannot
// close either.
var nodeID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// variableName is the shape of a scenario variable. It becomes a key in an
// object the generated script builds.
var variableName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// IsVariableName reports whether name may be used as a scenario variable.
//
// Exported because the generator asks the same question about a {name} written
// in a path, and the answer has to be the same one in both places.
func IsVariableName(name string) bool {
	return variableName.MatchString(name)
}

// httpMethods are the methods a request node may use.
var httpMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true, "HEAD": true,
}

// validate refuses a node the generator could not emit.
//
// Each kind is checked for what it needs and nothing else, so a field left
// empty by a kind that does not use it is not an error.
func (n Node) validate() error {
	if !nodeID.MatchString(n.ID) {
		return fmt.Errorf("%w: %q is not a usable node id", ErrInvalidGraph, n.ID)
	}

	if err := n.validateLabels(); err != nil {
		return err
	}

	switch n.Kind {
	case NodeHTTPRequest:
		return n.validateRequest()
	case NodeExtractVariable:
		return n.validateExtract()
	case NodeSetVariable:
		return n.validateSet()
	case NodeCondition, NodeAssertion:
		return n.validateExpression()
	case NodeLoop:
		return n.validateLoop()
	case NodeThinkTime:
		return n.validateThinkTime()
	case NodeEnd:
		return nil
	default:
		return fmt.Errorf("%w: the node %q has the unknown kind %q", ErrInvalidGraph, n.ID, n.Kind)
	}
}

// validateLabels bounds the text a node carries for people to read.
func (n Node) validateLabels() error {
	if len(n.Group) > maxNodeLabelLength || len(n.Label) > maxNodeLabelLength {
		return fmt.Errorf("%w: the group or the label of the node %q is longer than %d characters",
			ErrInvalidGraph, n.ID, maxNodeLabelLength)
	}
	return nil
}

// validateQuery checks a request's query parameters.
func (n Node) validateQuery() error {
	if len(n.Query) > maxQueryParams {
		return fmt.Errorf("%w: the node %q has %d query parameters, more than the %d allowed",
			ErrInvalidGraph, n.ID, len(n.Query), maxQueryParams)
	}

	for _, param := range n.Query {
		if strings.TrimSpace(param.Name) == "" {
			return fmt.Errorf("%w: the node %q has a query parameter with no name", ErrInvalidGraph, n.ID)
		}
		if len(param.Name) > maxNodeTextLength || len(param.Value) > maxNodeTextLength {
			return fmt.Errorf("%w: a query parameter of the node %q is longer than %d characters",
				ErrInvalidGraph, n.ID, maxNodeTextLength)
		}
		if _, err := ParseTemplate(param.Value); err != nil {
			return err
		}
	}
	return nil
}

func (n Node) validateRequest() error {
	if err := n.validateQuery(); err != nil {
		return err
	}

	if !httpMethods[n.Method] {
		return fmt.Errorf("%w: the node %q uses the method %q", ErrInvalidGraph, n.ID, n.Method)
	}
	if !strings.HasPrefix(n.Path, "/") {
		return fmt.Errorf("%w: the path of the node %q does not start with a slash", ErrInvalidGraph, n.ID)
	}
	if len(n.Path) > maxNodeTextLength || len(n.Body) > maxNodeTextLength {
		return fmt.Errorf("%w: the node %q carries more than %d characters",
			ErrInvalidGraph, n.ID, maxNodeTextLength)
	}

	if _, err := ParsePathTemplate(n.Path); err != nil {
		return err
	}
	if _, err := ParseTemplate(n.Body); err != nil {
		return err
	}

	for header, value := range n.Headers {
		if strings.TrimSpace(header) == "" {
			return fmt.Errorf("%w: the node %q has a header with no name", ErrInvalidGraph, n.ID)
		}
		if len(value) > maxNodeTextLength {
			return fmt.Errorf("%w: the header %q of the node %q is longer than %d characters",
				ErrInvalidGraph, header, n.ID, maxNodeTextLength)
		}
		parts, err := ParseTemplate(value)
		if err != nil {
			return err
		}

		// A credential is a reference to a variable, never a value. A graph and the
		// script written from it are stored in the clear, so a token typed into a
		// header would stay in the database, in every version after it, and in the
		// script anybody who can read the scenario can read. The message names the
		// header and never repeats what was typed.
		if IsCredentialHeader(header) && !readsAVariable(parts) {
			return fmt.Errorf(
				"%w: the header %q of the node %q must read a variable, such as Bearer {{token}}, "+
					"filled from a secret of the environment: a value typed here would be stored in the clear",
				ErrInvalidGraph, header, n.ID)
		}
	}

	return nil
}

// readsAVariable reports whether a parsed value reads at least one variable.
func readsAVariable(parts []TemplatePart) bool {
	for _, part := range parts {
		if part.Variable != "" {
			return true
		}
	}
	return false
}

// ExtractSource is the part of a response an extract_variable reads.
type ExtractSource string

// The three places a value can be taken from. The set is closed, like the kinds
// of node: the generator writes each of them and nothing else.
const (
	ExtractFromBody   ExtractSource = "body"
	ExtractFromHeader ExtractSource = "header"
	ExtractFromStatus ExtractSource = "status"
)

// Where is the source an extraction reads, with the default made explicit: a
// node with none reads the body.
func (n Node) Where() ExtractSource {
	if n.Source == "" {
		return ExtractFromBody
	}
	return n.Source
}

// BodyPath is the selector k6 reads from the JSON body. k6 takes data.token and
// not $.data.token, but the second is what people write and what the mock-ups
// show, so a leading "$." is accepted and taken off here: the one place that
// decides it, for the validation and the generator alike.
func (n Node) BodyPath() string {
	path := strings.TrimSpace(n.From)
	return strings.TrimPrefix(path, "$.")
}

// headerName is what a header's name may be made of (RFC 9110's token).
var headerName = regexp.MustCompile("^[A-Za-z0-9!#$%&'*+.^_`|~-]+$")

func (n Node) validateExtract() error {
	if !variableName.MatchString(n.Variable) {
		return fmt.Errorf("%w: %q is not a usable variable name", ErrInvalidGraph, n.Variable)
	}
	if len(n.From) > maxNodeTextLength {
		return fmt.Errorf("%w: the path of the node %q is longer than %d characters",
			ErrInvalidGraph, n.ID, maxNodeTextLength)
	}

	switch n.Where() {
	case ExtractFromBody:
		if n.BodyPath() == "" || n.BodyPath() == "$" {
			return fmt.Errorf("%w: the node %q extracts from nothing: give a path in the body, such as data.token",
				ErrInvalidGraph, n.ID)
		}
	case ExtractFromHeader:
		name := strings.TrimSpace(n.From)
		if name == "" {
			return fmt.Errorf("%w: the node %q extracts from nothing: give the name of a header",
				ErrInvalidGraph, n.ID)
		}
		if !headerName.MatchString(name) {
			return fmt.Errorf("%w: %q is not a header name", ErrInvalidGraph, name)
		}
	case ExtractFromStatus:
		if strings.TrimSpace(n.From) != "" {
			return fmt.Errorf("%w: the node %q reads the status, which takes no path",
				ErrInvalidGraph, n.ID)
		}
	default:
		return fmt.Errorf("%w: the node %q reads the source %q, wanted body, header or status",
			ErrInvalidGraph, n.ID, n.Source)
	}
	return nil
}

func (n Node) validateSet() error {
	if !variableName.MatchString(n.Variable) {
		return fmt.Errorf("%w: %q is not a usable variable name", ErrInvalidGraph, n.Variable)
	}
	if len(n.Value) > maxNodeTextLength {
		return fmt.Errorf("%w: the value of the node %q is longer than %d characters",
			ErrInvalidGraph, n.ID, maxNodeTextLength)
	}

	if n.FromEnv == "" {
		_, err := ParseTemplate(n.Value)
		return err
	}

	if n.Value != "" {
		return fmt.Errorf("%w: the node %q both holds a value and reads one from the environment",
			ErrInvalidGraph, n.ID)
	}

	// The same rule as a secret's key, which is where the value comes from. It
	// refuses the K6_ prefix and BASE_URL, so a graph cannot reach past the
	// ceilings or redirect the load by naming a variable.
	if err := ValidateSecretKey(n.FromEnv); err != nil {
		return fmt.Errorf("%w: the node %q reads %q: %v", ErrInvalidGraph, n.ID, n.FromEnv, err)
	}

	return nil
}

// validateExpression parses the expression rather than looking at it. The
// grammar is the only thing standing between a text field and a script that
// runs whatever somebody typed.
func (n Node) validateExpression() error {
	if _, err := ParseCondition(n.Expression); err != nil {
		return fmt.Errorf("%w: the expression of the node %q: %v", ErrInvalidGraph, n.ID, err)
	}
	return nil
}

func (n Node) validateLoop() error {
	if n.Iterations < 1 {
		return fmt.Errorf("%w: the loop %q repeats %d times", ErrInvalidGraph, n.ID, n.Iterations)
	}
	if n.Iterations > MaxLoopIterations {
		return fmt.Errorf("%w: the loop %q repeats %d times, more than the %d allowed",
			ErrInvalidGraph, n.ID, n.Iterations, MaxLoopIterations)
	}
	return nil
}

func (n Node) validateThinkTime() error {
	if n.Seconds <= 0 || n.Seconds > 300 {
		return fmt.Errorf("%w: the node %q waits %v seconds, wanted between 0 and 300",
			ErrInvalidGraph, n.ID, n.Seconds)
	}
	return nil
}
