package domain

import (
	"fmt"
	"strings"
)

// TemplatePart is one piece of a value a user wrote: either literal text, or a
// variable to read at run time.
type TemplatePart struct {
	// Text is the literal, when Variable is empty.
	Text string
	// Variable is the name to read from the scenario's variables.
	Variable string
}

// ParsePathTemplate cuts a path on {name}.
//
// Single braces here, because that is what a path template looks like
// everywhere else — OpenAPI writes /users/{id} — and because this same text
// becomes the request's name tag, which somebody reads in a threshold.
func ParsePathTemplate(path string) ([]TemplatePart, error) {
	return parseTemplate(path, "{", "}", "path")
}

// ParseTemplate cuts a header value or a body on {{name}}.
//
// Two braces, and the asymmetry with a path is deliberate: a JSON body is full
// of single braces, and treating those as placeholders would refuse
// {"name":"thing"} — the most ordinary body there is. A single brace in a value
// is therefore literal, and only {{ }} reads a variable.
func ParseTemplate(text string) ([]TemplatePart, error) {
	return parseTemplate(text, "{{", "}}", "value")
}

// parseTemplate is the one piece of splitting logic, used for both syntaxes so
// that they cannot drift apart.
func parseTemplate(text, opening, closing, what string) ([]TemplatePart, error) {
	var parts []TemplatePart
	rest := text

	for {
		start := strings.Index(rest, opening)
		if start < 0 {
			break
		}

		end := strings.Index(rest[start:], closing)
		if end < 0 {
			return nil, fmt.Errorf("%w: the %s has a %s that is never closed: %q",
				ErrInvalidGraph, what, opening, text)
		}
		end += start

		name := rest[start+len(opening) : end]
		if !IsVariableName(name) {
			return nil, fmt.Errorf("%w: %q is not a usable variable name in the %s %q",
				ErrInvalidGraph, name, what, text)
		}

		if literal := rest[:start]; literal != "" {
			parts = append(parts, TemplatePart{Text: literal})
		}
		parts = append(parts, TemplatePart{Variable: name})

		rest = rest[end+len(closing):]
	}

	if rest != "" {
		parts = append(parts, TemplatePart{Text: rest})
	}

	return parts, nil
}
