package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/youssefmahersi/ballast/internal/adapter/http/dto"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/scenario"
)

// maxImportBody is the most a request may carry. The specification travels as a
// JSON string, where a quote or a line break takes two bytes, so the body is
// allowed room beyond the limit on the file itself. The file is then measured
// on its own, by the reader, and refused with a message that names its limit.
const maxImportBody = 2*domain.MaxSpecBytes + 1024*1024

// PreviewOpenAPI handles POST /projects/{projectId}/scenarios/import/openapi/preview.
func (h *Scenario) PreviewOpenAPI(w http.ResponseWriter, r *http.Request) {
	who, projectID, ok := projectRequest(w, r)
	if !ok {
		return
	}

	var body dto.OpenAPIPreviewRequest
	if !readImportBody(w, r, &body) {
		return
	}

	spec, err := h.scenarios.PreviewOpenAPI(r.Context(), who, projectID, []byte(body.Spec))
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, previewOf(spec))
}

// ImportOpenAPI handles POST /projects/{projectId}/scenarios/import/openapi.
func (h *Scenario) ImportOpenAPI(w http.ResponseWriter, r *http.Request) {
	who, projectID, ok := projectRequest(w, r)
	if !ok {
		return
	}

	var body dto.ImportOpenAPIRequest
	if !readImportBody(w, r, &body) {
		return
	}

	references := make([]domain.EndpointRef, 0, len(body.Endpoints))
	for _, endpoint := range body.Endpoints {
		references = append(references, domain.EndpointRef{Method: endpoint.Method, Path: endpoint.Path})
	}

	created, report, notes, err := h.scenarios.ImportOpenAPI(r.Context(), who, scenario.OpenAPIImport{
		ProjectID: projectID,
		Name:      body.Name,
		Spec:      []byte(body.Spec),
		Endpoints: references,
	})
	if err != nil {
		h.writeScriptError(w, r, err, report)
		return
	}

	if notes == nil {
		notes = []string{}
	}
	writeJSON(w, http.StatusCreated, dto.ImportOpenAPIResponse{
		Scenario: scenarioOf(created, report),
		Notes:    notes,
	})
}

// readImportBody decodes a request that carries a specification, and answers
// the request itself when it cannot: 413 for a body past the limit, 400 for
// anything that is not the JSON expected.
func readImportBody(w http.ResponseWriter, r *http.Request, into any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxImportBody)

	if err := json.NewDecoder(r.Body).Decode(into); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge,
				"the request is larger than Ballast reads; a specification can be at most 5 MB")
			return false
		}
		writeError(w, http.StatusBadRequest, "the request body is not valid JSON")
		return false
	}
	return true
}

func previewOf(spec domain.ImportedSpec) dto.OpenAPIPreviewResponse {
	response := dto.OpenAPIPreviewResponse{
		Title:     spec.Title,
		BasePath:  spec.BasePath,
		Endpoints: make([]dto.OpenAPIEndpoint, 0, len(spec.Endpoints)),
		Schemes:   make([]dto.OpenAPIAuthScheme, 0, len(spec.Schemes)),
		Notes:     spec.Notes,
	}
	if response.Notes == nil {
		response.Notes = []string{}
	}

	for _, endpoint := range spec.Endpoints {
		params := endpoint.PathParams
		if params == nil {
			params = []string{}
		}

		response.Endpoints = append(response.Endpoints, dto.OpenAPIEndpoint{
			Method:      endpoint.Method,
			Path:        endpoint.Path,
			Tag:         endpoint.Tag,
			Summary:     endpoint.Summary,
			PathParams:  params,
			HasBody:     endpoint.ContentType != "" || endpoint.Body != "",
			BodyTooLong: endpoint.BodyTooLong,
		})
	}

	for _, scheme := range spec.Schemes {
		response.Schemes = append(response.Schemes, dto.OpenAPIAuthScheme{
			Name: scheme.Name, Type: scheme.Type, Detail: scheme.Detail,
		})
	}

	return response
}
