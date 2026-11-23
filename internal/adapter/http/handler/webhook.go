package handler

import (
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/adapter/http/dto"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/webhook"
)

// Webhook maps HTTP requests onto a project's webhook.
//
// It has two faces. The management routes are behind a session and an administrator's
// permission, like the API tokens'. The receiving route has no session: the signature is
// what authenticates it, and until the receiver has checked it the caller is nobody.
//
// An installation that was built without webhooks passes nil for both, and every route
// answers 404, as though it did not exist.
type Webhook struct {
	manager  *webhook.Manager
	receiver *webhook.Receiver
	// runs maps what the run service refuses into the statuses POST /runs uses: a
	// webhook is one more way to ask for a run, and it is answered in the same words.
	runs *Run
	log  *slog.Logger
}

// NewWebhook returns the handler.
func NewWebhook(manager *webhook.Manager, receiver *webhook.Receiver, log *slog.Logger) *Webhook {
	return &Webhook{manager: manager, receiver: receiver, runs: &Run{log: log}, log: log}
}

// unauthorized is the one answer to a request that did not prove it holds the secret,
// whatever was wrong with it: the same status and the same bytes, with nothing in them
// that was sent, so that nobody learns from the answer which part of a guess was right.
func unauthorized(w http.ResponseWriter) {
	writeError(w, http.StatusUnauthorized, "the request was not accepted")
}

// Receive handles POST /webhooks/{orgId}/{projectId}.
func (h *Webhook) Receive(w http.ResponseWriter, r *http.Request) {
	if h.receiver == nil {
		http.NotFound(w, r)
		return
	}

	// The two identifiers name where to look. One that is not an identifier finds
	// nothing, and gets the answer a wrong signature gets.
	orgID, err := uuid.Parse(r.PathValue("orgId"))
	if err != nil {
		unauthorized(w)
		return
	}
	projectID, err := uuid.Parse(r.PathValue("projectId"))
	if err != nil {
		unauthorized(w)
		return
	}

	// One byte more than the limit is read, so that a body over it is told from one
	// that is exactly at it; the receiver refuses it.
	body, err := io.ReadAll(io.LimitReader(r.Body, domain.MaxWebhookBody+1))
	if err != nil {
		unauthorized(w)
		return
	}

	queued, err := h.receiver.Receive(r.Context(), webhook.Request{
		OrgID: orgID, ProjectID: projectID,
		Timestamp: r.Header.Get("X-Ballast-Timestamp"),
		Delivery:  r.Header.Get("X-Ballast-Delivery"),
		Signature: r.Header.Get("X-Ballast-Signature"),
		Body:      body,
	})
	switch {
	case err == nil:
		writeJSON(w, http.StatusAccepted, dto.EnqueueRunResponse{ID: queued.ID.String(), Status: string(queued.Status)})
	case errors.Is(err, domain.ErrWebhookRefused):
		unauthorized(w)
	case errors.Is(err, domain.ErrInvalidWebhookBody):
		// The sender is who they say, and the request is wrong: told in this code's
		// own words, which name the field and never the value.
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		h.runs.writeDomainError(w, r, err)
	}
}

// Get handles GET /projects/{projectId}/webhook.
func (h *Webhook) Get(w http.ResponseWriter, r *http.Request) {
	if h.manager == nil {
		http.NotFound(w, r)
		return
	}
	who, projectID, ok := projectRequest(w, r)
	if !ok {
		return
	}

	hook, found, err := h.manager.Get(r.Context(), who, projectID)
	if err != nil {
		h.writeManageError(w, r, err)
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "this project has no webhook")
		return
	}
	writeJSON(w, http.StatusOK, toDTOWebhook(hook))
}

// Create handles POST /projects/{projectId}/webhook.
func (h *Webhook) Create(w http.ResponseWriter, r *http.Request) {
	if h.manager == nil {
		http.NotFound(w, r)
		return
	}
	who, projectID, ok := projectRequest(w, r)
	if !ok {
		return
	}

	created, err := h.manager.Create(r.Context(), who, projectID)
	if err != nil {
		h.writeManageError(w, r, err)
		return
	}
	h.writeCreated(w, http.StatusCreated, created)
}

// Rotate handles POST /projects/{projectId}/webhook/rotate.
func (h *Webhook) Rotate(w http.ResponseWriter, r *http.Request) {
	if h.manager == nil {
		http.NotFound(w, r)
		return
	}
	who, projectID, ok := projectRequest(w, r)
	if !ok {
		return
	}

	rotated, err := h.manager.Rotate(r.Context(), who, projectID)
	if err != nil {
		h.writeManageError(w, r, err)
		return
	}
	h.writeCreated(w, http.StatusOK, rotated)
}

// Delete handles DELETE /projects/{projectId}/webhook.
func (h *Webhook) Delete(w http.ResponseWriter, r *http.Request) {
	if h.manager == nil {
		http.NotFound(w, r)
		return
	}
	who, projectID, ok := projectRequest(w, r)
	if !ok {
		return
	}

	if err := h.manager.Delete(r.Context(), who, projectID); err != nil {
		h.writeManageError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeCreated answers with a secret, which is in this one response and in no other. A
// body with a secret in it is not for a cache, a proxy or a browser's history.
func (h *Webhook) writeCreated(w http.ResponseWriter, status int, created webhook.Created) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, dto.CreatedWebhook{Webhook: toDTOWebhook(created.Webhook), Secret: created.Secret})
}

func (h *Webhook) writeManageError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrForbidden):
		writeError(w, http.StatusForbidden, "you do not have permission to do that here")
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "this project has no webhook")
	case errors.Is(err, domain.ErrWebhookExists):
		writeError(w, http.StatusConflict, "this project already has a webhook: rotate its secret instead")
	default:
		h.log.ErrorContext(r.Context(), "request failed", "path", r.URL.Path, "error", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
	}
}

func toDTOWebhook(hook domain.Webhook) dto.Webhook {
	return dto.Webhook{
		ID:        hook.ID.String(),
		ProjectID: hook.ProjectID.String(),
		Path:      "/webhooks/" + hook.OrgID.String() + "/" + hook.ProjectID.String(),
		CreatedAt: hook.CreatedAt,
		RotatedAt: hook.RotatedAt,
	}
}
