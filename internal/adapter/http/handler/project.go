package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/adapter/http/dto"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/project"
)

// Project maps HTTP requests onto the project service.
type Project struct {
	projects *project.Service
	log      *slog.Logger
}

// NewProject returns the handler.
func NewProject(projects *project.Service, log *slog.Logger) *Project {
	return &Project{projects: projects, log: log}
}

// List handles GET /projects.
func (h *Project) List(w http.ResponseWriter, r *http.Request) {
	who, ok := signedIn(w, r)
	if !ok {
		return
	}

	found, err := h.projects.List(r.Context(), who)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response := dto.ProjectListResponse{Projects: make([]dto.Project, 0, len(found))}
	for _, p := range found {
		response.Projects = append(response.Projects, toDTOProject(p))
	}

	writeJSON(w, http.StatusOK, response)
}

// Create handles POST /projects.
func (h *Project) Create(w http.ResponseWriter, r *http.Request) {
	who, ok := signedIn(w, r)
	if !ok {
		return
	}

	var body dto.CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "the request body is not valid JSON")
		return
	}

	created, err := h.projects.Create(r.Context(), who, body.Name, body.Slug)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, toDTOProject(created))
}

// ByID handles GET /projects/{projectId}.
func (h *Project) ByID(w http.ResponseWriter, r *http.Request) {
	who, id, ok := projectRequest(w, r)
	if !ok {
		return
	}

	found, err := h.projects.ByID(r.Context(), who, id)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, toDTOProject(found))
}

// Update handles PATCH /projects/{projectId}.
func (h *Project) Update(w http.ResponseWriter, r *http.Request) {
	who, id, ok := projectRequest(w, r)
	if !ok {
		return
	}

	var body dto.UpdateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "the request body is not valid JSON")
		return
	}

	renamed, err := h.projects.Rename(r.Context(), who, id, body.Name)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, toDTOProject(renamed))
}

// Delete handles DELETE /projects/{projectId}.
func (h *Project) Delete(w http.ResponseWriter, r *http.Request) {
	who, id, ok := projectRequest(w, r)
	if !ok {
		return
	}

	if err := h.projects.Delete(r.Context(), who, id); err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Members handles GET /projects/{projectId}/members.
func (h *Project) Members(w http.ResponseWriter, r *http.Request) {
	who, id, ok := projectRequest(w, r)
	if !ok {
		return
	}

	members, err := h.projects.Members(r.Context(), who, id)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response := dto.MemberListResponse{Members: make([]dto.Member, 0, len(members))}
	for _, member := range members {
		response.Members = append(response.Members, dto.Member{
			UserID:               member.UserID.String(),
			Email:                member.Email,
			Name:                 member.Name,
			Role:                 string(member.Role),
			HasCustomPermissions: member.Override != nil,
			CreatedAt:            member.CreatedAt,
			LastActivity:         member.LastActivity,
		})
	}

	writeJSON(w, http.StatusOK, response)
}

// SetMemberRole handles PUT /projects/{projectId}/members/{userId}.
func (h *Project) SetMemberRole(w http.ResponseWriter, r *http.Request) {
	who, projectID, ok := projectRequest(w, r)
	if !ok {
		return
	}

	userID, err := uuid.Parse(r.PathValue("userId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "the user id is not a uuid")
		return
	}

	var body dto.SetMemberRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "the request body is not valid JSON")
		return
	}

	if err := h.projects.SetMemberRole(
		r.Context(), who, projectID, userID, domain.Role(body.Role),
	); err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// RemoveMember handles DELETE /projects/{projectId}/members/{userId}.
func (h *Project) RemoveMember(w http.ResponseWriter, r *http.Request) {
	who, projectID, ok := projectRequest(w, r)
	if !ok {
		return
	}

	userID, err := uuid.Parse(r.PathValue("userId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "the user id is not a uuid")
		return
	}

	if err := h.projects.RemoveMember(r.Context(), who, projectID, userID); err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Permissions handles GET /projects/{projectId}/permissions.
func (h *Project) Permissions(w http.ResponseWriter, r *http.Request) {
	who, projectID, ok := projectRequest(w, r)
	if !ok {
		return
	}

	access, err := h.projects.Access(r.Context(), who, projectID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, dto.AccessResponse{
		Role:        string(access.Role),
		Permissions: dto.PermissionNames(access.Permissions),
		Custom:      access.Custom,
	})
}

// projectRequest reads the caller and the project the path names.
func projectRequest(w http.ResponseWriter, r *http.Request) (domain.Identity, uuid.UUID, bool) {
	who, ok := signedIn(w, r)
	if !ok {
		return domain.Identity{}, uuid.Nil, false
	}

	id, err := uuid.Parse(r.PathValue("projectId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "the project id is not a uuid")
		return domain.Identity{}, uuid.Nil, false
	}

	return who, id, true
}

func toDTOProject(p domain.Project) dto.Project {
	return dto.Project{
		ID:        p.ID.String(),
		Name:      p.Name,
		Slug:      p.Slug,
		CreatedAt: p.CreatedAt,
	}
}

// writeDomainError turns a domain error into a status code.
//
// A refusal and a project that does not exist are both 403, deliberately: a 404
// would tell somebody with no business there that the project exists.
func (h *Project) writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrForbidden):
		writeError(w, http.StatusForbidden, "you do not have permission to do that here")

	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "no such project or member")

	case errors.Is(err, domain.ErrProjectNotEmpty):
		writeError(w, http.StatusConflict,
			"this project still holds runs; deleting it would take their history with it")

	case errors.Is(err, domain.ErrRoleAboveOwn):
		writeError(w, http.StatusForbidden, "you cannot grant a role above your own")

	case errors.Is(err, domain.ErrLastAdministrator):
		writeError(w, http.StatusConflict,
			"this project would be left with no administrator")

	case errors.Is(err, domain.ErrAlreadyExists):
		writeError(w, http.StatusConflict, "a project already uses that slug")

	case errors.Is(err, domain.ErrInvalidProject):
		writeError(w, http.StatusBadRequest, err.Error())

	default:
		h.log.ErrorContext(r.Context(), "request failed", "path", r.URL.Path, "error", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
	}
}

// Roles handles GET /roles: the permissions matrix. It is the same for every
// project and every caller, and is derived from the table the checks use.
func (h *Project) Roles(w http.ResponseWriter, _ *http.Request) {
	capabilities := domain.Capabilities()

	response := dto.RolesResponse{
		Capabilities: make([]dto.Capability, 0, len(capabilities)),
		Roles:        make([]dto.RoleCapabilities, 0, len(domain.Roles())),
	}
	for _, capability := range capabilities {
		response.Capabilities = append(response.Capabilities, dto.Capability{ID: capability.ID, Label: capability.Label})
	}
	for _, role := range domain.Roles() {
		held := make([]string, 0, len(capabilities))
		for _, capability := range capabilities {
			if capability.Grants(role) {
				held = append(held, capability.ID)
			}
		}
		response.Roles = append(response.Roles, dto.RoleCapabilities{Role: string(role), Capabilities: held})
	}

	writeJSON(w, http.StatusOK, response)
}
