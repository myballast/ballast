package openapi

import (
	"strings"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// readSchemes lists the ways the file says a caller authenticates.
//
// Only the kind of scheme and where it goes are kept: never a description, an
// example or an address, since none of that is needed to tell somebody what
// their requests will have to carry.
func (r *specReader) readSchemes() ([]domain.AuthScheme, error) {
	definitions := r.schemeDefinitions()

	schemes := make([]domain.AuthScheme, 0, len(definitions))
	for _, name := range sortedKeys(definitions) {
		definition, err := r.references.resolveMap(definitions[name])
		if err != nil {
			return nil, err
		}
		if definition == nil {
			continue
		}

		scheme := domain.AuthScheme{
			Name:   name,
			Type:   asString(definition["type"]),
			Detail: r.schemeDetail(definition),
		}
		schemes = append(schemes, scheme)

		if scheme.Type == "apiKey" && asString(definition["in"]) == "header" {
			r.credentialHeaders[strings.ToLower(asString(definition["name"]))] = true
		}
	}

	return schemes, nil
}

func (r *specReader) schemeDefinitions() map[string]any {
	if r.isSwagger2 {
		return asMap(r.document["securityDefinitions"])
	}
	return asMap(asMap(r.document["components"])["securitySchemes"])
}

// schemeDetail says where or how a scheme is used, in a few words.
func (r *specReader) schemeDetail(definition map[string]any) string {
	switch asString(definition["type"]) {
	case "apiKey":
		return asString(definition["in"]) + " " + asString(definition["name"])

	case "http":
		return asString(definition["scheme"])

	case "oauth2":
		if r.isSwagger2 {
			return asString(definition["flow"])
		}
		return strings.Join(sortedKeys(asMap(definition["flows"])), ", ")
	}

	return ""
}
