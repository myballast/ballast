package postman

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Reader turns the text of a collection, and of an environment file, into the
// requests they describe. It holds no state.
type Reader struct{}

// NewReader returns a reader.
func NewReader() Reader {
	return Reader{}
}

// Read parses a collection, and merges the variables of an environment file when
// one is given. An empty environment is no environment.
func (Reader) Read(collection, environment []byte) (domain.ImportedCollection, error) {
	if len(collection) > domain.MaxCollectionBytes || len(environment) > domain.MaxCollectionBytes {
		return domain.ImportedCollection{}, fmt.Errorf("%w: a file is larger than %d MB",
			domain.ErrInvalidCollection, domain.MaxCollectionBytes/(1024*1024))
	}

	document, err := decodeObject(collection, "the collection")
	if err != nil {
		return domain.ImportedCollection{}, err
	}
	if err := checkFormat(document); err != nil {
		return domain.ImportedCollection{}, err
	}

	walk := &walker{}
	imported := domain.ImportedCollection{Name: shortened(stringAt(objectAt(document, "info"), "name"), 120)}

	walk.collectionEvents(document)
	if err := walk.items(arrayAt(document, "item"), nil, authOf(document), 0); err != nil {
		return domain.ImportedCollection{}, err
	}
	imported.Requests = walk.requests
	imported.Findings = walk.findings

	imported.Variables = variablesOf(document)
	if len(bytes.TrimSpace(environment)) > 0 {
		env, err := decodeObject(environment, "the environment file")
		if err != nil {
			return domain.ImportedCollection{}, err
		}
		imported.Variables = mergeEnvironment(imported.Variables, env)
	}

	return imported, nil
}

// decodeObject reads one JSON object. what names the file in a message.
func decodeObject(data []byte, what string) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("%w: %s is not valid JSON: %v", domain.ErrInvalidCollection, what, err)
	}

	object, isObject := decoded.(map[string]any)
	if !isObject {
		return nil, fmt.Errorf("%w: %s is not a JSON object", domain.ErrInvalidCollection, what)
	}
	return object, nil
}

// checkFormat refuses what is not a v2.0 or v2.1 collection. A v1 collection
// lists its requests at the top, under "requests", and has no folders in the
// same sense; reading it as v2 would find nothing and say so badly.
func checkFormat(document map[string]any) error {
	info := objectAt(document, "info")
	schema := stringAt(info, "schema")

	if schema == "" {
		if _, v1 := document["requests"]; v1 {
			return fmt.Errorf("%w: this is a Postman v1 collection; export it again as v2.1", domain.ErrInvalidCollection)
		}
		return fmt.Errorf("%w: the file has no info.schema, so it is not a Postman collection", domain.ErrInvalidCollection)
	}

	if !strings.Contains(schema, "/collection/v2.0") && !strings.Contains(schema, "/collection/v2.1") {
		return fmt.Errorf("%w: only Postman collections of format v2.0 and v2.1 are read", domain.ErrInvalidCollection)
	}
	if _, hasItems := document["item"]; !hasItems {
		return fmt.Errorf("%w: the collection has no item list", domain.ErrInvalidCollection)
	}
	return nil
}

// variablesOf reads the variables a collection defines.
func variablesOf(document map[string]any) []domain.CollectionVariable {
	var variables []domain.CollectionVariable

	for _, entry := range arrayAt(document, "variable") {
		variable := asObject(entry)
		name := stringAt(variable, "key")
		if name == "" || boolAt(variable, "disabled") {
			continue
		}
		variables = append(variables, domain.CollectionVariable{Name: name, Value: stringAt(variable, "value")})
	}
	return variables
}

// mergeEnvironment lets an environment's values replace the collection's, and
// adds the ones the collection does not have. The environment says which of its
// variables are secrets.
func mergeEnvironment(variables []domain.CollectionVariable, environment map[string]any) []domain.CollectionVariable {
	position := map[string]int{}
	for index, variable := range variables {
		position[variable.Name] = index
	}

	for _, entry := range arrayAt(environment, "values") {
		value := asObject(entry)
		name := stringAt(value, "key")
		if name == "" || (value["enabled"] != nil && !boolAt(value, "enabled")) {
			continue
		}

		merged := domain.CollectionVariable{
			Name:   name,
			Value:  stringAt(value, "value"),
			Secret: stringAt(value, "type") == "secret",
		}
		if index, exists := position[name]; exists {
			// A variable the collection keeps public is still a secret if the
			// environment says so.
			merged.Secret = merged.Secret || variables[index].Secret
			variables[index] = merged
			continue
		}
		position[name] = len(variables)
		variables = append(variables, merged)
	}
	return variables
}
