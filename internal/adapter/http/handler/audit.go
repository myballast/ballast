package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/adapter/http/dto"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/audit"
)

// Audit serves the journal.
type Audit struct {
	journal *audit.Service
	log     *slog.Logger
}

// NewAudit returns the handler.
func NewAudit(journal *audit.Service, log *slog.Logger) *Audit {
	return &Audit{journal: journal, log: log}
}

// InProject handles GET /projects/{projectId}/audit.
func (h *Audit) InProject(w http.ResponseWriter, r *http.Request) {
	who, ok := signedIn(w, r)
	if !ok {
		return
	}

	projectID, err := uuid.Parse(r.PathValue("projectId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "the project id is not a uuid")
		return
	}

	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 {
			writeError(w, http.StatusBadRequest, "limit is not a positive whole number")
			return
		}
	}

	entries, err := h.journal.InProject(r.Context(), who, projectID, limit)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response := dto.AuditListResponse{Entries: make([]dto.AuditEntry, 0, len(entries))}
	for _, entry := range entries {
		response.Entries = append(response.Entries, dto.AuditEntry{
			Action:         string(entry.Action),
			Outcome:        string(entry.Outcome),
			ActorID:        idOrEmpty(entry.ActorID),
			ActorTokenID:   idOrEmpty(entry.ActorTokenID),
			ActorWebhookID: idOrEmpty(entry.ActorWebhookID),
			ProjectID:      idOrEmpty(entry.ProjectID),
			TargetType:     entry.TargetType,
			TargetID:       idOrEmpty(entry.TargetID),
			Details:        entry.Details,
			At:             entry.At,
		})
	}

	writeJSON(w, http.StatusOK, response)
}

// writeDomainError turns a domain error into a status code. Anything it does
// not recognise becomes a 500 with no detail: an error from deeper down can
// name a table, a column or an identifier belonging to somebody else.
func (h *Audit) writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrForbidden):
		// The caller can see this project — the middleware let them in — so
		// there is nothing to hide by answering 404 instead.
		writeError(w, http.StatusForbidden, "only an administrator of this project may read its journal")

	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "no such project")

	default:
		h.log.ErrorContext(r.Context(), "request failed", "path", r.URL.Path, "error", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
	}
}

// idOrEmpty renders an identifier that may be absent. An entry Ballast wrote
// itself has no author, and an action on the organization has no project.
func idOrEmpty(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}
