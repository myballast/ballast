package openapi

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// requestBody is what an operation says about the body it takes.
type requestBody struct {
	contentType string
	text        string
	// tooLong is set when an example exists but is longer than a node holds.
	tooLong bool
}

// bodyOf finds the content type and the example body of an operation.
//
// Only an example written in the file is used. A body is never invented from a
// schema: a guessed value would look like data somebody chose.
func (r *specReader) bodyOf(operation map[string]any, parameters []map[string]any) (requestBody, error) {
	if r.isSwagger2 {
		return r.swagger2Body(operation, parameters)
	}
	return r.openapi3Body(operation)
}

func (r *specReader) openapi3Body(operation map[string]any) (requestBody, error) {
	body, err := r.references.resolveMap(operation["requestBody"])
	if err != nil || body == nil {
		return requestBody{}, err
	}

	content := asMap(body["content"])
	contentType := preferredMediaType(content)
	if contentType == "" {
		return requestBody{}, nil
	}

	media, err := r.references.resolveMap(content[contentType])
	if err != nil {
		return requestBody{}, err
	}

	example, found, err := r.mediaExample(media)
	if err != nil {
		return requestBody{}, err
	}

	return bodyWithExample(contentType, example, found), nil
}

// preferredMediaType picks JSON when the operation accepts it, then any other
// JSON type, then the first by name.
func preferredMediaType(content map[string]any) string {
	if len(content) == 0 {
		return ""
	}

	names := sortedKeys(content)
	for _, name := range names {
		if name == "application/json" {
			return name
		}
	}
	for _, name := range names {
		if strings.Contains(name, "json") {
			return name
		}
	}
	return names[0]
}

// mediaExample reads the example of a media type: its own, the first of its
// named examples, or the one on its schema.
func (r *specReader) mediaExample(media map[string]any) (any, bool, error) {
	if example, found := media["example"]; found {
		return example, true, nil
	}

	examples := asMap(media["examples"])
	if len(examples) > 0 {
		first := sortedKeys(examples)[0]
		named, err := r.references.resolveMap(examples[first])
		if err != nil {
			return nil, false, err
		}
		if value, found := named["value"]; found {
			return value, true, nil
		}
	}

	schema, err := r.references.resolveMap(media["schema"])
	if err != nil || schema == nil {
		return nil, false, err
	}
	example, found := schema["example"]
	return example, found, nil
}

func (r *specReader) swagger2Body(operation map[string]any, parameters []map[string]any) (requestBody, error) {
	var bodyParameter map[string]any
	for _, parameter := range parameters {
		if asString(parameter["in"]) == "body" {
			bodyParameter = parameter
		}
	}
	if bodyParameter == nil {
		return requestBody{}, nil
	}

	contentType := firstConsumed(operation)
	if contentType == "" {
		contentType = firstConsumed(r.document)
	}
	if contentType == "" {
		contentType = "application/json"
	}

	if example, found := bodyParameter["x-example"]; found {
		return bodyWithExample(contentType, example, true), nil
	}

	schema, err := r.references.resolveMap(bodyParameter["schema"])
	if err != nil || schema == nil {
		return bodyWithExample(contentType, nil, false), err
	}
	example, found := schema["example"]
	return bodyWithExample(contentType, example, found), nil
}

func firstConsumed(source map[string]any) string {
	consumed := asList(source["consumes"])
	if len(consumed) == 0 {
		return ""
	}
	return asString(consumed[0])
}

// bodyWithExample writes the example as text, or leaves the body empty when
// there is none or it is too long. The content type stays either way: it tells
// the person this request takes a body to fill in.
func bodyWithExample(contentType string, example any, found bool) requestBody {
	// A type such as */* says nothing about what is sent.
	if strings.Contains(contentType, "*") {
		contentType = ""
	}

	result := requestBody{contentType: contentType}
	if !found || example == nil {
		return result
	}

	text, fits := exampleAsText(example, contentType)
	if !fits {
		result.tooLong = true
		return result
	}

	result.text = text
	return result
}

// exampleAsText writes an example the way it would be sent, and says whether
// it is short enough to keep.
func exampleAsText(example any, contentType string) (string, bool) {
	if text, isText := example.(string); isText {
		return textExample(text, contentType)
	}

	// The size is measured before anything is written, so that a reference to
	// something huge costs a few steps and not the whole of it.
	budget := domain.MaxImportedTextLength
	if !fitsInBudget(example, &budget) {
		return "", false
	}

	var written bytes.Buffer
	encoder := json.NewEncoder(&written)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(example); err != nil {
		return "", false
	}

	text := strings.TrimSpace(written.String())
	return text, len(text) <= domain.MaxImportedTextLength
}

// textExample handles an example written as a string. Many files write a JSON
// body as a string of JSON, which is sent as it is; a string for a JSON type
// that is not a container is a JSON string, with its quotes.
func textExample(text, contentType string) (string, bool) {
	if len(text) > domain.MaxImportedTextLength {
		return "", false
	}

	if !strings.Contains(contentType, "json") {
		return text, true
	}

	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return text, true
	}

	quoted, err := json.Marshal(text)
	if err != nil || len(quoted) > domain.MaxImportedTextLength {
		return "", false
	}
	return string(quoted), true
}

// fitsInBudget walks an example and spends one unit per character and per
// element, stopping as soon as the budget is gone.
func fitsInBudget(value any, budget *int) bool {
	*budget--
	if *budget < 0 {
		return false
	}

	switch typed := value.(type) {
	case string:
		*budget -= len(typed)
		return *budget >= 0

	case map[string]any:
		for key, inner := range typed {
			*budget -= len(key)
			if !fitsInBudget(inner, budget) {
				return false
			}
		}

	case []any:
		for _, inner := range typed {
			if !fitsInBudget(inner, budget) {
				return false
			}
		}
	}

	return true
}
