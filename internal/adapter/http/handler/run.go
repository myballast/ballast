package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/adapter/http/dto"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/run"
)

// Run maps HTTP requests onto the run service.
//
// The organization is taken from the configuration rather than from the
// request: until authentication exists there is nobody to read it from, and
// reading it from a header would be a hole rather than a placeholder.
type Run struct {
	runs  *run.Service
	orgID uuid.UUID
	log   *slog.Logger
}

// NewRun returns the handler.
func NewRun(runs *run.Service, orgID uuid.UUID, log *slog.Logger) *Run {
	return &Run{runs: runs, orgID: orgID, log: log}
}

// Enqueue handles POST /runs.
func (h *Run) Enqueue(w http.ResponseWriter, r *http.Request) {
	var body dto.EnqueueRunRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "the request body is not valid JSON")
		return
	}

	scenarioID, err := uuid.Parse(body.ScenarioID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "scenario_id is not a uuid")
		return
	}

	environmentID, err := uuid.Parse(body.EnvironmentID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "environment_id is not a uuid")
		return
	}

	cmd := run.EnqueueCommand{
		OrgID:         h.orgID,
		ScenarioID:    scenarioID,
		EnvironmentID: environmentID,
		Profile:       domain.ProfileKind(body.Profile),
	}
	if body.Params != nil {
		cmd.Params = &domain.ProfileParams{
			TargetVUs: body.Params.TargetVUs,
			RampUpS:   body.Params.RampUpS,
			PlateauS:  body.Params.PlateauS,
			RampDownS: body.Params.RampDownS,
		}
	}

	queued, err := h.runs.Enqueue(r.Context(), cmd)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	writeJSON(w, http.StatusAccepted, dto.EnqueueRunResponse{
		ID:     queued.ID.String(),
		Status: string(queued.Status),
	})
}

// ByID handles GET /runs/{id}.
func (h *Run) ByID(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "the run id is not a uuid")
		return
	}

	found, results, err := h.runs.ByID(r.Context(), h.orgID, id)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response := dto.RunResponse{
		ID:              found.ID.String(),
		ProjectID:       found.ProjectID.String(),
		ScenarioID:      found.ScenarioID.String(),
		EnvironmentID:   found.EnvironmentID.String(),
		ScenarioVersion: found.ScenarioVersion,
		Profile:         string(found.Profile),
		Params: dto.ProfileParams{
			TargetVUs: found.ProfileParams.TargetVUs,
			RampUpS:   found.ProfileParams.RampUpS,
			PlateauS:  found.ProfileParams.PlateauS,
			RampDownS: found.ProfileParams.RampDownS,
		},
		Status:       string(found.Status),
		ErrorMessage: found.ErrorMessage,
		CreatedAt:    found.CreatedAt,
		StartedAt:    found.StartedAt,
		EndedAt:      found.EndedAt,
		Metrics:      make([]dto.Metric, 0, len(results.Metrics)),
		Thresholds:   make([]dto.Threshold, 0, len(results.Thresholds)),
	}

	for _, metric := range results.Metrics {
		response.Metrics = append(response.Metrics, dto.Metric{
			Metric: metric.Metric,
			Tag:    metric.Tag,
			Stat:   metric.Stat,
			Value:  metric.Value,
		})
	}
	for _, threshold := range results.Thresholds {
		response.Thresholds = append(response.Thresholds, dto.Threshold{
			Metric:     threshold.Metric,
			Expression: threshold.Expression,
			Passed:     threshold.Passed,
		})
	}

	writeJSON(w, http.StatusOK, response)
}

// writeDomainError turns a domain error into a status code.
//
// Anything it does not recognise becomes a 500 with no detail: an error message
// from deeper down can name a table, a column or an identifier belonging to
// somebody else.
func (h *Run) writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "no such run")

	case errors.Is(err, domain.ErrTargetNotVerified):
		// The refusal the whole product rests on. 403, and the attempt is
		// logged above, in the service.
		writeError(w, http.StatusForbidden, "this environment has not been verified")

	case errors.Is(err, domain.ErrCrossProject):
		writeError(w, http.StatusBadRequest, "the scenario and the environment belong to different projects")

	case errors.Is(err, domain.ErrInvalidProfileParams):
		writeError(w, http.StatusBadRequest, err.Error())

	default:
		h.log.ErrorContext(r.Context(), "request failed", "path", r.URL.Path, "error", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, dto.Error{Error: message})
}
