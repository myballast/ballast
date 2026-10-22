package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/adapter/http/dto"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/scenario"
)

// Scenario maps HTTP requests onto the scenario service.
type Scenario struct {
	scenarios *scenario.Service
	log       *slog.Logger
}

// NewScenario returns the handler.
func NewScenario(scenarios *scenario.Service, log *slog.Logger) *Scenario {
	return &Scenario{scenarios: scenarios, log: log}
}

// List handles the scenario list of a project.
func (h *Scenario) List(w http.ResponseWriter, r *http.Request) {
	who, projectID, ok := projectRequest(w, r)
	if !ok {
		return
	}

	found, err := h.scenarios.InProject(r.Context(), who, projectID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response := dto.ScenarioListResponse{Scenarios: make([]dto.ScenarioListItem, 0, len(found))}
	for _, s := range found {
		response.Scenarios = append(response.Scenarios, dto.ScenarioListItem{
			ScenarioSummary: summaryOf(s.Scenario),
			ThresholdCount:  s.Thresholds,
		})
	}

	writeJSON(w, http.StatusOK, response)
}

// Create handles creating a scenario.
func (h *Scenario) Create(w http.ResponseWriter, r *http.Request) {
	who, projectID, ok := projectRequest(w, r)
	if !ok {
		return
	}

	var body dto.CreateScenarioRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "the request body is not valid JSON")
		return
	}

	created, report, err := h.scenarios.Create(r.Context(), who, scenario.NewScenario{
		ProjectID:            projectID,
		Name:                 body.Name,
		Script:               body.Script,
		DefaultProfile:       domain.ProfileKind(body.DefaultProfile),
		DefaultProfileParams: paramsOf(body.DefaultParams),
	})
	if err != nil {
		h.writeScriptError(w, r, err, report)
		return
	}

	writeJSON(w, http.StatusCreated, scenarioOf(created, report))
}

// ByID handles reading one scenario.
func (h *Scenario) ByID(w http.ResponseWriter, r *http.Request) {
	who, projectID, id, ok := scenarioRequest(w, r)
	if !ok {
		return
	}

	found, report, err := h.scenarios.ByID(r.Context(), who, projectID, id)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, scenarioOf(found, report))
}

// Update handles changing a scenario.
func (h *Scenario) Update(w http.ResponseWriter, r *http.Request) {
	who, projectID, id, ok := scenarioRequest(w, r)
	if !ok {
		return
	}

	var body dto.UpdateScenarioRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "the request body is not valid JSON")
		return
	}

	update := scenario.Update{Name: body.Name, Script: body.Script}
	if body.DefaultProfile != nil {
		profile := domain.ProfileKind(*body.DefaultProfile)
		update.DefaultProfile = &profile
	}
	if body.DefaultParams != nil {
		update.DefaultProfileParams = paramsOf(body.DefaultParams)
	}

	updated, report, err := h.scenarios.Update(r.Context(), who, projectID, id, update)
	if err != nil {
		h.writeScriptError(w, r, err, report)
		return
	}

	writeJSON(w, http.StatusOK, scenarioOf(updated, report))
}

// Delete handles removing a scenario.
func (h *Scenario) Delete(w http.ResponseWriter, r *http.Request) {
	who, projectID, id, ok := scenarioRequest(w, r)
	if !ok {
		return
	}

	if err := h.scenarios.Delete(r.Context(), who, projectID, id); err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Versions handles the list of versions, newest first.
func (h *Scenario) Versions(w http.ResponseWriter, r *http.Request) {
	who, projectID, id, ok := scenarioRequest(w, r)
	if !ok {
		return
	}

	versions, err := h.scenarios.Versions(r.Context(), who, projectID, id)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response := dto.ScenarioVersionListResponse{
		Versions: make([]dto.ScenarioVersion, 0, len(versions)),
	}
	for _, version := range versions {
		entry := dto.ScenarioVersion{
			Version:        version.Version,
			Script:         version.Script,
			DefaultProfile: string(version.DefaultProfile),
			DefaultParams:  paramsDTO(version.DefaultProfileParams),
			CreatedAt:      version.CreatedAt,
		}
		if version.CreatedBy != nil {
			entry.CreatedBy = version.CreatedBy.String()
		}
		response.Versions = append(response.Versions, entry)
	}

	writeJSON(w, http.StatusOK, response)
}

// Thresholds handles the list of rules a scenario imposes.
func (h *Scenario) Thresholds(w http.ResponseWriter, r *http.Request) {
	who, projectID, id, ok := scenarioRequest(w, r)
	if !ok {
		return
	}

	thresholds, err := h.scenarios.Thresholds(r.Context(), who, projectID, id)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response := dto.ThresholdListResponse{
		Thresholds: make([]dto.ThresholdRule, 0, len(thresholds)),
	}
	for _, threshold := range thresholds {
		response.Thresholds = append(response.Thresholds, dto.ThresholdRule{
			ID:         threshold.ID.String(),
			Metric:     threshold.Metric,
			Expression: threshold.Expression,
		})
	}

	writeJSON(w, http.StatusOK, response)
}

// AddThreshold handles imposing a rule.
func (h *Scenario) AddThreshold(w http.ResponseWriter, r *http.Request) {
	who, projectID, id, ok := scenarioRequest(w, r)
	if !ok {
		return
	}

	var body dto.CreateThresholdRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "the request body is not valid JSON")
		return
	}

	added, err := h.scenarios.AddThreshold(r.Context(), who, projectID, id, body.Metric, body.Expression)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, dto.ThresholdRule{
		ID:         added.ID.String(),
		Metric:     added.Metric,
		Expression: added.Expression,
	})
}

// UpdateThreshold handles changing a rule in place.
func (h *Scenario) UpdateThreshold(w http.ResponseWriter, r *http.Request) {
	who, projectID, id, ok := scenarioRequest(w, r)
	if !ok {
		return
	}

	thresholdID, err := uuid.Parse(r.PathValue("thresholdId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "the threshold id is not a uuid")
		return
	}

	var body dto.UpdateThresholdRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "the request body is not valid JSON")
		return
	}

	updated, err := h.scenarios.UpdateThreshold(r.Context(), who, projectID, id, thresholdID, scenario.ThresholdChange{
		Metric:     body.Metric,
		Expression: body.Expression,
	})
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, dto.ThresholdRule{
		ID:         updated.ID.String(),
		Metric:     updated.Metric,
		Expression: updated.Expression,
	})
}

// RemoveThreshold handles dropping a rule.
func (h *Scenario) RemoveThreshold(w http.ResponseWriter, r *http.Request) {
	who, projectID, id, ok := scenarioRequest(w, r)
	if !ok {
		return
	}

	thresholdID, err := uuid.Parse(r.PathValue("thresholdId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "the threshold id is not a uuid")
		return
	}

	if err := h.scenarios.RemoveThreshold(r.Context(), who, projectID, id, thresholdID); err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// scenarioRequest reads the caller, the project and the scenario the path names.
func scenarioRequest(w http.ResponseWriter, r *http.Request) (domain.Identity, uuid.UUID, uuid.UUID, bool) {
	who, projectID, ok := projectRequest(w, r)
	if !ok {
		return domain.Identity{}, uuid.Nil, uuid.Nil, false
	}

	id, err := uuid.Parse(r.PathValue("scenarioId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "the scenario id is not a uuid")
		return domain.Identity{}, uuid.Nil, uuid.Nil, false
	}

	return who, projectID, id, true
}

func summaryOf(s domain.Scenario) dto.ScenarioSummary {
	return dto.ScenarioSummary{
		ID:             s.ID.String(),
		Name:           s.Name,
		Version:        s.Version,
		DefaultProfile: string(s.DefaultProfile),
		DefaultParams:  paramsDTO(s.DefaultProfileParams),
		CreatedAt:      s.CreatedAt,
		UpdatedAt:      s.UpdatedAt,
	}
}

func scenarioOf(s domain.Scenario, report domain.ScriptReport) dto.Scenario {
	warnings := report.Warnings()

	mode := s.Mode
	if mode == "" {
		mode = domain.ModeCode
	}

	out := dto.Scenario{
		ScenarioSummary: summaryOf(s),
		Script:          s.Script,
		Mode:            string(mode),
		Warnings:        make([]dto.ScriptFinding, 0, len(warnings)),
	}
	for _, finding := range warnings {
		out.Warnings = append(out.Warnings, dto.ScriptFinding{
			Code:    finding.Code,
			Message: finding.Message,
			Detail:  finding.Detail,
			Line:    finding.Line,
		})
	}
	return out
}

func paramsOf(params *dto.ProfileParams) *domain.ProfileParams {
	if params == nil {
		return nil
	}
	return &domain.ProfileParams{
		TargetVUs: params.TargetVUs,
		RampUpS:   params.RampUpS,
		PlateauS:  params.PlateauS,
		RampDownS: params.RampDownS,
	}
}

func paramsDTO(params domain.ProfileParams) dto.ProfileParams {
	return dto.ProfileParams{
		TargetVUs: params.TargetVUs,
		RampUpS:   params.RampUpS,
		PlateauS:  params.PlateauS,
		RampDownS: params.RampDownS,
	}
}

// writeScriptError answers a refused script with what was wrong with it, rather
// than with a status code and nothing to act on.
func (h *Scenario) writeScriptError(
	w http.ResponseWriter, r *http.Request, err error, report domain.ScriptReport,
) {
	if !errors.Is(err, domain.ErrInvalidScript) {
		h.writeDomainError(w, r, err)
		return
	}

	findings := report.Errors()
	body := struct {
		Error    string              `json:"error"`
		Findings []dto.ScriptFinding `json:"findings"`
	}{
		Error:    "the script was refused",
		Findings: make([]dto.ScriptFinding, 0, len(findings)),
	}
	for _, finding := range findings {
		body.Findings = append(body.Findings, dto.ScriptFinding{
			Code:    finding.Code,
			Message: finding.Message,
			Detail:  finding.Detail,
			Line:    finding.Line,
		})
	}

	writeJSON(w, http.StatusUnprocessableEntity, body)
}

func (h *Scenario) writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrForbidden):
		writeError(w, http.StatusForbidden, "you do not have permission to do that here")

	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "no such scenario or threshold")

	case errors.Is(err, domain.ErrScenarioNotEmpty):
		writeError(w, http.StatusConflict,
			"this scenario still holds runs; deleting it would take their history with it")

	case errors.Is(err, domain.ErrAlreadyExists):
		writeError(w, http.StatusConflict, "a scenario or threshold already uses that")

	case errors.Is(err, domain.ErrScriptIsGenerated):
		writeError(w, http.StatusConflict,
			"this scenario is written as a graph; its script is generated, so edit the graph instead")

	case errors.Is(err, domain.ErrInvalidSpec),
		errors.Is(err, domain.ErrInvalidCollection),
		errors.Is(err, domain.ErrInvalidGraph):
		writeError(w, http.StatusUnprocessableEntity, err.Error())

	case errors.Is(err, domain.ErrInvalidScenario),
		errors.Is(err, domain.ErrInvalidThreshold),
		errors.Is(err, domain.ErrInvalidProfileParams):
		writeError(w, http.StatusBadRequest, err.Error())

	default:
		h.log.ErrorContext(r.Context(), "request failed", "path", r.URL.Path, "error", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
	}
}
