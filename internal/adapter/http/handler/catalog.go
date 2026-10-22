package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/adapter/http/dto"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/catalog"
)

// Catalog maps HTTP requests onto the catalog service.
type Catalog struct {
	catalog *catalog.Service
	log     *slog.Logger
}

// NewCatalog returns the handler.
func NewCatalog(service *catalog.Service, log *slog.Logger) *Catalog {
	return &Catalog{catalog: service, log: log}
}

// ImportOpenAPI handles POST /projects/{projectId}/endpoints/import/openapi.
func (h *Catalog) ImportOpenAPI(w http.ResponseWriter, r *http.Request) {
	who, projectID, ok := projectRequest(w, r)
	if !ok {
		return
	}

	var body dto.CatalogImportRequest
	if !readImportBody(w, r, &body, maxImportBody) {
		return
	}

	references := make([]domain.EndpointRef, 0, len(body.Endpoints))
	for _, endpoint := range body.Endpoints {
		references = append(references, domain.EndpointRef{Method: endpoint.Method, Path: endpoint.Path})
	}

	count, err := h.catalog.Import(r.Context(), who, catalog.Import{
		ProjectID: projectID,
		Source:    body.Source,
		Spec:      []byte(body.Spec),
		Endpoints: references,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, dto.CatalogImportResponse{Count: count})
}

// List handles GET /projects/{projectId}/endpoints.
func (h *Catalog) List(w http.ResponseWriter, r *http.Request) {
	who, projectID, ok := projectRequest(w, r)
	if !ok {
		return
	}

	query := r.URL.Query()
	found, err := h.catalog.List(r.Context(), who, projectID, domain.CatalogFilter{
		Query:  query.Get("q"),
		Tag:    query.Get("tag"),
		Source: query.Get("source"),
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	response := dto.CatalogListResponse{Endpoints: make([]dto.CatalogEndpoint, 0, len(found))}
	for _, endpoint := range found {
		response.Endpoints = append(response.Endpoints, catalogEndpointOf(endpoint))
	}
	writeJSON(w, http.StatusOK, response)
}

// Delete handles DELETE /projects/{projectId}/endpoints/{endpointId}.
func (h *Catalog) Delete(w http.ResponseWriter, r *http.Request) {
	who, projectID, ok := projectRequest(w, r)
	if !ok {
		return
	}

	id, err := uuid.Parse(r.PathValue("endpointId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "the endpoint id is not a uuid")
		return
	}

	if err := h.catalog.Delete(r.Context(), who, projectID, id); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteSource handles DELETE /projects/{projectId}/endpoints?source=.
func (h *Catalog) DeleteSource(w http.ResponseWriter, r *http.Request) {
	who, projectID, ok := projectRequest(w, r)
	if !ok {
		return
	}

	removed, err := h.catalog.DeleteSource(r.Context(), who, projectID, r.URL.Query().Get("source"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.CatalogRemovedResponse{Removed: removed})
}

func catalogEndpointOf(endpoint domain.CatalogEndpoint) dto.CatalogEndpoint {
	params := endpoint.PathParams
	if params == nil {
		params = []string{}
	}
	headers := endpoint.Headers
	if headers == nil {
		headers = map[string]string{}
	}

	return dto.CatalogEndpoint{
		ID:          endpoint.ID.String(),
		Source:      endpoint.Source,
		Method:      endpoint.Method,
		Path:        endpoint.Path,
		Tag:         endpoint.Tag,
		Summary:     endpoint.Summary,
		PathParams:  params,
		Headers:     headers,
		ContentType: endpoint.ContentType,
		Body:        endpoint.Body,
		BodyTooLong: endpoint.BodyTooLong,
		CreatedAt:   endpoint.CreatedAt,
	}
}

func (h *Catalog) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrForbidden):
		writeError(w, http.StatusForbidden, "you do not have permission to do that here")

	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "no such endpoint")

	case errors.Is(err, domain.ErrInvalidSpec), errors.Is(err, domain.ErrInvalidCollection),
		errors.Is(err, domain.ErrInvalidGraph):
		writeError(w, http.StatusUnprocessableEntity, err.Error())

	case errors.Is(err, domain.ErrInvalidCatalog):
		writeError(w, http.StatusBadRequest, err.Error())

	default:
		h.log.ErrorContext(r.Context(), "request failed", "path", r.URL.Path, "error", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
	}
}
