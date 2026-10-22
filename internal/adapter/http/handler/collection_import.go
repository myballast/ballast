package handler

import (
	"net/http"

	"github.com/youssefmahersi/ballast/internal/adapter/http/dto"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/catalog"
	"github.com/youssefmahersi/ballast/internal/usecase/scenario"
)

// PreviewPostman handles POST /projects/{projectId}/scenarios/import/postman/preview.
func (h *Scenario) PreviewPostman(w http.ResponseWriter, r *http.Request) {
	who, projectID, ok := projectRequest(w, r)
	if !ok {
		return
	}

	var body dto.CollectionPreviewRequest
	if !readImportBody(w, r, &body, maxCollectionBody) {
		return
	}

	preview, err := h.scenarios.PreviewCollection(
		r.Context(), who, projectID, []byte(body.Collection), []byte(body.Environment))
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, collectionPreviewOf(preview))
}

// ImportPostman handles POST /projects/{projectId}/scenarios/import/postman.
func (h *Scenario) ImportPostman(w http.ResponseWriter, r *http.Request) {
	who, projectID, ok := projectRequest(w, r)
	if !ok {
		return
	}

	var body dto.ImportCollectionRequest
	if !readImportBody(w, r, &body, maxCollectionBody) {
		return
	}

	created, report, result, err := h.scenarios.ImportCollection(r.Context(), who, scenario.CollectionImport{
		ProjectID:   projectID,
		Name:        body.Name,
		Collection:  []byte(body.Collection),
		Environment: []byte(body.Environment),
		Requests:    body.Requests,
	})
	if err != nil {
		h.writeScriptError(w, r, err, report)
		return
	}

	writeJSON(w, http.StatusCreated, dto.ImportCollectionResponse{
		Scenario: scenarioOf(created, report),
		Secrets:  secretsOf(result.Secrets),
		Findings: findingsOf(result.Findings),
	})
}

// ImportPostman handles POST /projects/{projectId}/endpoints/import/postman.
func (h *Catalog) ImportPostman(w http.ResponseWriter, r *http.Request) {
	who, projectID, ok := projectRequest(w, r)
	if !ok {
		return
	}

	var body dto.CatalogCollectionImportRequest
	if !readImportBody(w, r, &body, maxCollectionBody) {
		return
	}

	result, err := h.catalog.ImportCollection(r.Context(), who, catalog.CollectionImport{
		ProjectID:   projectID,
		Source:      body.Source,
		Collection:  []byte(body.Collection),
		Environment: []byte(body.Environment),
		Requests:    body.Requests,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, dto.CatalogCollectionImportResponse{
		Count:    result.Count,
		Findings: findingsOf(result.Findings),
	})
}

func collectionPreviewOf(preview domain.CollectionPreview) dto.CollectionPreviewResponse {
	response := dto.CollectionPreviewResponse{
		Name:     preview.Name,
		Requests: make([]dto.CollectionRequest, 0, len(preview.Requests)),
		Secrets:  secretsOf(preview.Secrets),
		Findings: findingsOf(preview.Findings),
	}

	for _, request := range preview.Requests {
		folders := request.Folders
		if folders == nil {
			folders = []string{}
		}
		findings := request.Findings
		if findings == nil {
			findings = []string{}
		}

		response.Requests = append(response.Requests, dto.CollectionRequest{
			Position: request.Position,
			Folders:  folders,
			Name:     request.Name,
			Method:   request.Method,
			Path:     request.Path,
			HasBody:  request.HasBody,
			LeftOut:  request.LeftOut,
			Findings: findings,
		})
	}
	return response
}

func secretsOf(secrets []domain.DetectedSecret) []dto.DetectedSecret {
	converted := make([]dto.DetectedSecret, 0, len(secrets))
	for _, secret := range secrets {
		converted = append(converted, dto.DetectedSecret{Key: secret.Key, Variable: secret.Variable})
	}
	return converted
}

func findingsOf(findings []domain.ImportFinding) []dto.ImportFinding {
	converted := make([]dto.ImportFinding, 0, len(findings))
	for _, finding := range findings {
		converted = append(converted, dto.ImportFinding{Request: finding.Request, Message: finding.Message})
	}
	return converted
}
