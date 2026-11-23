package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/adapter/http/dto"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/apitoken"
)

// maxAPITokenBody bounds what a creation may send: a name and a date fit in a
// few hundred bytes, and a body that is far larger is a mistake or an attack and
// is not read to its end.
const maxAPITokenBody = 4 << 10

// APIToken maps HTTP requests onto the token manager.
type APIToken struct {
	tokens *apitoken.Manager
	log    *slog.Logger
}

// NewAPIToken returns the handler.
func NewAPIToken(tokens *apitoken.Manager, log *slog.Logger) *APIToken {
	return &APIToken{tokens: tokens, log: log}
}

// Create handles POST /projects/{projectId}/api-tokens.
func (h *APIToken) Create(w http.ResponseWriter, r *http.Request) {
	who, projectID, ok := projectRequest(w, r)
	if !ok {
		return
	}

	var body dto.CreateAPITokenRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAPITokenBody)).Decode(&body); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "the request body is too large")
			return
		}
		writeError(w, http.StatusBadRequest, "the request body is not valid JSON")
		return
	}

	created, err := h.tokens.Create(r.Context(), who, projectID, apitoken.CreateCommand{
		Name: body.Name, ExpiresAt: body.ExpiresAt,
	})
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	// The secret is in this body, and a body with a secret in it is not for a
	// cache, a proxy or a browser's history.
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, dto.CreatedAPIToken{
		Token: toDTOAPIToken(apitoken.Item{Token: created.Token, State: domain.APITokenActive}),
		// The one place the secret leaves the process.
		Secret: created.Secret,
	})
}

// List handles GET /projects/{projectId}/api-tokens.
func (h *APIToken) List(w http.ResponseWriter, r *http.Request) {
	who, projectID, ok := projectRequest(w, r)
	if !ok {
		return
	}

	items, err := h.tokens.List(r.Context(), who, projectID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response := dto.APITokenListResponse{
		Tokens: make([]dto.APIToken, 0, len(items)),
		Limit:  domain.MaxAPITokensPerProject,
	}
	for _, item := range items {
		response.Tokens = append(response.Tokens, toDTOAPIToken(item))
	}
	writeJSON(w, http.StatusOK, response)
}

// Revoke handles DELETE /projects/{projectId}/api-tokens/{tokenId}.
func (h *APIToken) Revoke(w http.ResponseWriter, r *http.Request) {
	who, projectID, ok := projectRequest(w, r)
	if !ok {
		return
	}

	id, err := uuid.Parse(r.PathValue("tokenId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "the token id is not a uuid")
		return
	}

	if err := h.tokens.Revoke(r.Context(), who, projectID, id); err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toDTOAPIToken(item apitoken.Item) dto.APIToken {
	t := item.Token
	out := dto.APIToken{
		ID:            t.ID.String(),
		Name:          t.Name,
		Prefix:        t.Prefix,
		CreatedByName: item.CreatedByName,
		State:         string(item.State),
		ExpiresAt:     t.ExpiresAt,
		LastUsedAt:    t.LastUsedAt,
		RevokedAt:     t.RevokedAt,
		CreatedAt:     t.CreatedAt,
	}
	if t.CreatedBy != nil {
		out.CreatedBy = t.CreatedBy.String()
	}
	return out
}

// writeDomainError turns a domain error into a status code.
func (h *APIToken) writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrForbidden):
		writeError(w, http.StatusForbidden, "you do not have permission to do that here")

	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "no such API token")

	case errors.Is(err, domain.ErrTooManyAPITokens):
		writeError(w, http.StatusConflict,
			fmt.Sprintf("this project already holds %d API tokens, the most it may: revoke one first",
				domain.MaxAPITokensPerProject))

	case errors.Is(err, domain.ErrInvalidAPIToken):
		writeError(w, http.StatusUnprocessableEntity, err.Error())

	default:
		h.log.ErrorContext(r.Context(), "request failed", "path", r.URL.Path, "error", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
	}
}
