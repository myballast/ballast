package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/adapter/http/dto"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/environment"
)

// Environment maps HTTP requests onto the environment service.
type Environment struct {
	environments *environment.Service
	log          *slog.Logger
}

// NewEnvironment returns the handler.
func NewEnvironment(environments *environment.Service, log *slog.Logger) *Environment {
	return &Environment{environments: environments, log: log}
}

// List handles the environment list of a project.
func (h *Environment) List(w http.ResponseWriter, r *http.Request) {
	who, projectID, ok := projectRequest(w, r)
	if !ok {
		return
	}

	found, err := h.environments.InProject(r.Context(), who, projectID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response := dto.EnvironmentListResponse{Environments: make([]dto.Environment, 0, len(found))}
	for _, e := range found {
		response.Environments = append(response.Environments, environmentOf(e))
	}

	writeJSON(w, http.StatusOK, response)
}

// Create handles declaring a target.
func (h *Environment) Create(w http.ResponseWriter, r *http.Request) {
	who, projectID, ok := projectRequest(w, r)
	if !ok {
		return
	}

	var body dto.CreateEnvironmentRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "the request body is not valid JSON")
		return
	}

	window, err := windowOf(body.AllowedWindow)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	created, err := h.environments.Create(r.Context(), who, environment.NewEnvironment{
		ProjectID:    projectID,
		Name:         body.Name,
		BaseURL:      body.BaseURL,
		MaxVUs:       body.MaxVUs,
		MaxDurationS: body.MaxDurationS,
		Window:       window,
	})
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, environmentOf(created))
}

// ByID handles reading one environment.
func (h *Environment) ByID(w http.ResponseWriter, r *http.Request) {
	who, projectID, id, ok := environmentRequest(w, r)
	if !ok {
		return
	}

	found, err := h.environments.ByID(r.Context(), who, projectID, id)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, environmentOf(found))
}

// Update handles changing a target.
func (h *Environment) Update(w http.ResponseWriter, r *http.Request) {
	who, projectID, id, ok := environmentRequest(w, r)
	if !ok {
		return
	}

	var body dto.UpdateEnvironmentRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "the request body is not valid JSON")
		return
	}

	update := environment.Update{
		Name:         body.Name,
		BaseURL:      body.BaseURL,
		MaxVUs:       body.MaxVUs,
		MaxDurationS: body.MaxDurationS,
	}
	// Present in the body at all, whether as a window or as null.
	if body.AllowedWindow != nil {
		window, err := windowOf(*body.AllowedWindow)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		update.Window, update.WindowSet = window, true
	}

	updated, err := h.environments.Update(r.Context(), who, projectID, id, update)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, environmentOf(updated))
}

// Delete handles removing a target.
func (h *Environment) Delete(w http.ResponseWriter, r *http.Request) {
	who, projectID, id, ok := environmentRequest(w, r)
	if !ok {
		return
	}

	if err := h.environments.Delete(r.Context(), who, projectID, id); err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Secrets handles the list of keys set on an environment. The values are not part
// of any response.
func (h *Environment) Secrets(w http.ResponseWriter, r *http.Request) {
	who, projectID, id, ok := environmentRequest(w, r)
	if !ok {
		return
	}

	found, err := h.environments.SecretKeys(r.Context(), who, projectID, id)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response := dto.SecretListResponse{Secrets: make([]dto.Secret, 0, len(found))}
	for _, secret := range found {
		response.Secrets = append(response.Secrets, secretOf(secret))
	}

	writeJSON(w, http.StatusOK, response)
}

// SetSecret handles storing a credential under the key in the path.
//
// What comes back is the key and its dates. Echoing the value would put it in
// every proxy log between here and the browser, which is the whole reason the
// response type has no field for one.
func (h *Environment) SetSecret(w http.ResponseWriter, r *http.Request) {
	who, projectID, id, ok := environmentRequest(w, r)
	if !ok {
		return
	}

	key := r.PathValue("key")

	var body dto.SetSecretRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "the request body is not valid JSON")
		return
	}

	stored, err := h.environments.SetSecret(r.Context(), who, projectID, id, key, body.Value)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, secretOf(stored))
}

// RemoveSecret handles dropping a credential.
func (h *Environment) RemoveSecret(w http.ResponseWriter, r *http.Request) {
	who, projectID, id, ok := environmentRequest(w, r)
	if !ok {
		return
	}

	if err := h.environments.RemoveSecret(r.Context(), who, projectID, id, r.PathValue("key")); err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// environmentRequest reads the caller, the project and the environment the path
// names.
func environmentRequest(w http.ResponseWriter, r *http.Request) (domain.Identity, uuid.UUID, uuid.UUID, bool) {
	who, projectID, ok := projectRequest(w, r)
	if !ok {
		return domain.Identity{}, uuid.Nil, uuid.Nil, false
	}

	id, err := uuid.Parse(r.PathValue("environmentId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "the environment id is not a uuid")
		return domain.Identity{}, uuid.Nil, uuid.Nil, false
	}

	return who, projectID, id, true
}

func environmentOf(e domain.Environment) dto.Environment {
	out := dto.Environment{
		ID:                 e.ID.String(),
		Name:               e.Name,
		BaseURL:            e.BaseURL,
		VerificationStatus: string(e.VerificationStatus),
		MaxVUs:             e.MaxVUs,
		MaxDurationS:       e.MaxDurationS,
		CreatedAt:          e.CreatedAt,
	}

	if e.Window != nil {
		out.AllowedWindow = &dto.AllowedWindow{
			Start:    clockOf(e.Window.StartMinute),
			End:      clockOf(e.Window.EndMinute),
			Timezone: e.Window.Zone,
		}
	}

	return out
}

func secretOf(s domain.Secret) dto.Secret {
	return dto.Secret{
		ID:        s.ID.String(),
		Key:       s.Key,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
	}
}

// clockOf renders minutes since midnight as HH:MM, which is how a window reads to
// a person and how it arrives from one.
func clockOf(minute int) string {
	return fmt.Sprintf("%02d:%02d", minute/60, minute%60)
}

// windowOf reads a window from the body. A nil window is no window, which the
// caller distinguishes from an absent field itself.
func windowOf(window *dto.AllowedWindow) (*domain.AllowedWindow, error) {
	if window == nil {
		return nil, nil //nolint:nilnil // no window is a legitimate answer here
	}

	start, err := minuteOf(window.Start)
	if err != nil {
		return nil, fmt.Errorf("allowed_window.start: %w", err)
	}
	end, err := minuteOf(window.End)
	if err != nil {
		return nil, fmt.Errorf("allowed_window.end: %w", err)
	}

	return &domain.AllowedWindow{StartMinute: start, EndMinute: end, Zone: window.Timezone}, nil
}

// minuteOf reads HH:MM. The hours and minutes are checked here rather than left
// to the domain, so that "9h" comes back as something the caller can fix instead
// of as an arithmetic accident.
func minuteOf(clock string) (int, error) {
	hour, minute, found := strings.Cut(strings.TrimSpace(clock), ":")
	if !found {
		return 0, fmt.Errorf("%q is not a time of day; write it as HH:MM", clock)
	}

	h, err := strconv.Atoi(hour)
	if err != nil || h < 0 || h > 23 {
		return 0, fmt.Errorf("%q is not a time of day; the hour must be 00 to 23", clock)
	}
	m, err := strconv.Atoi(minute)
	if err != nil || m < 0 || m > 59 {
		return 0, fmt.Errorf("%q is not a time of day; the minutes must be 00 to 59", clock)
	}

	return h*60 + m, nil
}

func (h *Environment) writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrForbidden):
		writeError(w, http.StatusForbidden, "you do not have permission to do that here")

	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "no such environment or secret")

	case errors.Is(err, domain.ErrEnvironmentNotEmpty):
		writeError(w, http.StatusConflict,
			"this environment still holds runs; deleting it would take their history with it")

	case errors.Is(err, domain.ErrAlreadyExists):
		writeError(w, http.StatusConflict, "an environment of this project already uses that name")

	// The attempt was made and proved nothing. The message is the one thing the
	// caller can act on, and it never carries the token.
	case errors.Is(err, domain.ErrVerificationFailed):
		writeError(w, http.StatusUnprocessableEntity, err.Error())

	case errors.Is(err, domain.ErrNotProven):
		writeError(w, http.StatusConflict,
			"this target has not served its challenge yet; verify it before approving it")

	case errors.Is(err, domain.ErrAlreadyVerified):
		writeError(w, http.StatusConflict,
			"this target is already verified; edit its base URL if you mean to point it elsewhere")

	// The message of an invalid secret names the key and never the value: the
	// domain builds it that way, which is what makes returning it safe.
	case errors.Is(err, domain.ErrInvalidEnvironment), errors.Is(err, domain.ErrInvalidSecret):
		writeError(w, http.StatusBadRequest, err.Error())

	default:
		h.log.ErrorContext(r.Context(), "request failed", "path", r.URL.Path, "error", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
	}
}

// Challenge handles reading what a target has to serve.
func (h *Environment) Challenge(w http.ResponseWriter, r *http.Request) {
	who, projectID, id, ok := environmentRequest(w, r)
	if !ok {
		return
	}

	found, err := h.environments.Challenge(r.Context(), who, projectID, id)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	proofURL, err := found.ProofURL()
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	body := dto.VerificationChallenge{
		Status:     string(found.VerificationStatus),
		Token:      found.VerificationToken,
		URL:        proofURL,
		VerifiedAt: found.VerifiedAt,
	}
	if found.VerifiedBy != nil {
		body.VerifiedBy = found.VerifiedBy.String()
	}

	writeJSON(w, http.StatusOK, body)
}

// Verify handles fetching the challenge from the target.
//
// A target that served it answers 200 with the environment in proof_ok — which
// is not yet a target load may be sent to. One that did not answers 422 with the
// reason, because the reasons are fixed differently: put the file there, correct
// its contents, give the host a publicly reachable address, stop redirecting.
func (h *Environment) Verify(w http.ResponseWriter, r *http.Request) {
	who, projectID, id, ok := environmentRequest(w, r)
	if !ok {
		return
	}

	proven, err := h.environments.Verify(r.Context(), who, projectID, id)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, environmentOf(proven))
}

// Approve handles the decision that lets load reach a target.
func (h *Environment) Approve(w http.ResponseWriter, r *http.Request) {
	who, projectID, id, ok := environmentRequest(w, r)
	if !ok {
		return
	}

	approved, err := h.environments.Approve(r.Context(), who, projectID, id)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, environmentOf(approved))
}
