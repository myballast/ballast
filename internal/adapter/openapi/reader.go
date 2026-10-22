package openapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Reader turns the text of a specification into the endpoints it describes. It
// holds no state: everything one reading needs lives in specReader.
type Reader struct{}

// NewReader returns a reader.
func NewReader() Reader {
	return Reader{}
}

// maxTitleLength keeps the title short enough to be offered as a scenario name.
const maxTitleLength = 120

// Read parses a JSON or YAML OpenAPI 3.x or Swagger 2.0 document.
//
// Only what a scenario needs is read: the paths, their operations, the headers
// and the example body of each, and the authentication schemes. Schemas are not
// walked, which is also why a schema that refers to itself does no harm.
func (Reader) Read(data []byte) (domain.ImportedSpec, error) {
	if len(data) > domain.MaxSpecBytes {
		return domain.ImportedSpec{}, fmt.Errorf("%w: the file is larger than %d MB",
			domain.ErrInvalidSpec, domain.MaxSpecBytes/(1024*1024))
	}

	document, err := decode(data)
	if err != nil {
		return domain.ImportedSpec{}, err
	}

	isSwagger2, err := versionOf(document)
	if err != nil {
		return domain.ImportedSpec{}, err
	}

	paths := asMap(document["paths"])
	if paths == nil {
		return domain.ImportedSpec{}, fmt.Errorf("%w: the file has no paths object", domain.ErrInvalidSpec)
	}

	reading := &specReader{
		document:          document,
		isSwagger2:        isSwagger2,
		references:        &resolver{root: document},
		credentialHeaders: map[string]bool{},
	}

	return reading.read(paths)
}

// decode reads JSON or YAML into plain maps, lists and scalars.
//
// A document that starts with a brace is JSON. Anything else is handed to the
// YAML reader, which also refuses a document whose aliases expand into
// something enormous.
func decode(data []byte) (map[string]any, error) {
	trimmed := bytes.TrimLeft(data, "\xef\xbb\xbf \t\r\n")
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("%w: the file is empty", domain.ErrInvalidSpec)
	}

	var decoded any

	if trimmed[0] == '{' {
		decoder := json.NewDecoder(bytes.NewReader(trimmed))
		// Numbers stay as written, so a large identifier in an example is not
		// rounded on its way through a float.
		decoder.UseNumber()
		if err := decoder.Decode(&decoded); err != nil {
			return nil, fmt.Errorf("%w: the file is not valid JSON: %v", domain.ErrInvalidSpec, err)
		}
	} else {
		if err := yaml.Unmarshal(trimmed, &decoded); err != nil {
			return nil, fmt.Errorf("%w: the file is not valid YAML: %v", domain.ErrInvalidSpec, err)
		}
		decoded = withTextKeys(decoded)
	}

	document := asMap(decoded)
	if document == nil {
		return nil, fmt.Errorf("%w: the file is not an object", domain.ErrInvalidSpec)
	}
	return document, nil
}

// withTextKeys makes every map in a YAML tree a map[string]any. YAML allows
// keys that are not text — a response code written 200 is a number — and the
// rest of the reader would otherwise have to handle both kinds of map.
func withTextKeys(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, inner := range typed {
			typed[key] = withTextKeys(inner)
		}
		return typed

	case map[any]any:
		converted := make(map[string]any, len(typed))
		for key, inner := range typed {
			converted[fmt.Sprint(key)] = withTextKeys(inner)
		}
		return converted

	case []any:
		for position, inner := range typed {
			typed[position] = withTextKeys(inner)
		}
		return typed
	}

	return value
}

// versionOf says whether the document is Swagger 2.0, and refuses what is
// neither that nor an OpenAPI 3.x file.
func versionOf(document map[string]any) (bool, error) {
	if declared, found := document["openapi"]; found {
		if strings.HasPrefix(fmt.Sprint(declared), "3.") {
			return false, nil
		}
		return false, fmt.Errorf("%w: OpenAPI version %q is not supported, only 3.x and Swagger 2.0 are",
			domain.ErrInvalidSpec, shortened(fmt.Sprint(declared), 20))
	}

	if declared, found := document["swagger"]; found {
		// Written without quotes, 2.0 is a number and reads back as 2.
		text := fmt.Sprint(declared)
		if text == "2" || text == "2.0" {
			return true, nil
		}
		return false, fmt.Errorf("%w: Swagger version %q is not supported, only 2.0 is",
			domain.ErrInvalidSpec, shortened(text, 20))
	}

	return false, fmt.Errorf("%w: the file has neither an openapi nor a swagger version, so it is not a specification",
		domain.ErrInvalidSpec)
}
