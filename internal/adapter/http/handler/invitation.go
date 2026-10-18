package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/adapter/http/dto"
	"github.com/youssefmahersi/ballast/internal/adapter/http/session"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/invitation"
)

// Invitation maps HTTP requests onto the invitation service.
type Invitation struct {
	invitations *invitation.Service
	sessions    *session.Manager
	log         *slog.Logger
}

// NewInvitation returns the handler. sessions is needed by one route: accepting
// an invitation changes the organization the caller acts for.
func NewInvitation(invitations *invitation.Service, sessions *session.Manager, log *slog.Logger) *Invitation {
	return &Invitation{invitations: invitations, sessions: sessions, log: log}
}

// Create handles POST /projects/{projectId}/invitations.
func (h *Invitation) Create(w http.ResponseWriter, r *http.Request) {
	who, projectID, ok := projectRequest(w, r)
	if !ok {
		return
	}

	var body dto.InviteRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "the request body is not valid JSON")
		return
	}

	created, err := h.invitations.Create(r.Context(), who, projectID, body.Email, domain.Role(body.Role))
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	if created.Added != nil {
		writeJSON(w, http.StatusOK, dto.MemberAddedResponse{UserID: created.Added.String()})
		return
	}

	writeJSON(w, http.StatusCreated, dto.InvitationCreatedResponse{
		Invitation: toDTOInvitation(*created.Invitation, domain.InvitationPending),
		Token:      created.Token,
	})
}

// List handles GET /projects/{projectId}/invitations.
func (h *Invitation) List(w http.ResponseWriter, r *http.Request) {
	who, projectID, ok := projectRequest(w, r)
	if !ok {
		return
	}

	open, err := h.invitations.Pending(r.Context(), who, projectID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response := dto.InvitationListResponse{Invitations: make([]dto.Invitation, 0, len(open))}
	for _, o := range open {
		response.Invitations = append(response.Invitations, toDTOInvitation(o.Invitation, o.State))
	}
	writeJSON(w, http.StatusOK, response)
}

// Revoke handles DELETE /projects/{projectId}/invitations/{invitationId}.
func (h *Invitation) Revoke(w http.ResponseWriter, r *http.Request) {
	who, projectID, ok := projectRequest(w, r)
	if !ok {
		return
	}

	id, err := uuid.Parse(r.PathValue("invitationId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "the invitation id is not a uuid")
		return
	}

	if err := h.invitations.Revoke(r.Context(), who, projectID, id); err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Describe handles GET /invitations/{token}. Public: the page has to say who is
// asking before the person it asks has signed in.
func (h *Invitation) Describe(w http.ResponseWriter, r *http.Request) {
	described, state, err := h.invitations.Describe(r.Context(), r.PathValue("token"))
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, dto.InvitationPreview{
		OrganizationName: described.OrgName,
		ProjectName:      described.ProjectName,
		Email:            described.Email,
		Role:             string(described.Role),
		State:            string(state),
		ExpiresAt:        described.ExpiresAt,
	})
}

// Accept handles POST /invitations/{token}/accept.
func (h *Invitation) Accept(w http.ResponseWriter, r *http.Request) {
	who, ok := signedIn(w, r)
	if !ok {
		return
	}

	joined, err := h.invitations.Accept(r.Context(), who, r.PathValue("token"))
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	// The caller now acts for the organization that invited them: a session for
	// the one they came from would not see the project they have just joined.
	if err := h.sessions.Issue(w, joined.Identity); err != nil {
		h.log.ErrorContext(r.Context(), "issue a session", "error", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
		return
	}

	writeJSON(w, http.StatusOK, dto.InvitationAcceptedResponse{
		OrganizationID: joined.OrgID.String(),
		ProjectID:      joined.ProjectID.String(),
	})
}

func toDTOInvitation(i domain.Invitation, state domain.InvitationState) dto.Invitation {
	out := dto.Invitation{
		ID:        i.ID.String(),
		Email:     i.Email,
		Role:      string(i.Role),
		State:     string(state),
		ExpiresAt: i.ExpiresAt,
		CreatedAt: i.CreatedAt,
	}
	if i.InvitedBy != nil {
		out.InvitedBy = i.InvitedBy.String()
	}
	return out
}

// writeDomainError turns a domain error into a status code.
func (h *Invitation) writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrForbidden):
		writeError(w, http.StatusForbidden, "you do not have permission to do that here")

	case errors.Is(err, domain.ErrRoleAboveOwn):
		writeError(w, http.StatusForbidden, "you cannot grant a role above your own")

	case errors.Is(err, domain.ErrWrongInvitee):
		writeError(w, http.StatusForbidden, "this invitation was written for another email address")

	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "no such invitation")

	case errors.Is(err, domain.ErrInvitationUnusable):
		writeError(w, http.StatusGone, "this invitation was used, revoked or has expired")

	case errors.Is(err, domain.ErrAlreadyMember):
		writeError(w, http.StatusConflict, "that person already belongs to this project")

	case errors.Is(err, domain.ErrAlreadyExists):
		writeError(w, http.StatusConflict, "that address already has a pending invitation or an account here")

	case errors.Is(err, domain.ErrInvalidInvitation):
		writeError(w, http.StatusBadRequest, err.Error())

	default:
		h.log.ErrorContext(r.Context(), "request failed", "path", r.URL.Path, "error", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
	}
}
