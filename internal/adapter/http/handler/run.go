package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/adapter/http/dto"
	"github.com/youssefmahersi/ballast/internal/adapter/http/session"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/run"
)

// Run maps HTTP requests onto the run service.
//
// The organization is the one in the session, put in the request's context by
// middleware.Authenticate. It is never read from the request itself: a header
// or a parameter naming an organization would be a hole, not a feature.
type Run struct {
	runs *run.Service
	log  *slog.Logger
}

// NewRun returns the handler.
func NewRun(runs *run.Service, log *slog.Logger) *Run {
	return &Run{runs: runs, log: log}
}

// Enqueue handles POST /runs.
func (h *Run) Enqueue(w http.ResponseWriter, r *http.Request) {
	who, ok := signedIn(w, r)
	if !ok {
		return
	}

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
		Actor:         who,
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
	who, ok := signedIn(w, r)
	if !ok {
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "the run id is not a uuid")
		return
	}

	found, results, err := h.runs.ByID(r.Context(), who, id)
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
		Status:            string(found.Status),
		ErrorMessage:      found.ErrorMessage,
		CancelRequestedAt: found.CancelRequestedAt,
		CreatedAt:         found.CreatedAt,
		StartedAt:         found.StartedAt,
		EndedAt:           found.EndedAt,
		Metrics:           make([]dto.Metric, 0, len(results.Metrics)),
		Thresholds:        make([]dto.Threshold, 0, len(results.Thresholds)),
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

// Cancel handles DELETE /runs/{id}.
//
// Two answers, and the difference is not cosmetic. A queued run is cancelled by
// the time this returns: 200, it is over. A run being executed has been asked
// to stop, and its generator is still loading the target for a second or two:
// 202, because saying 200 would claim something that is not true yet. The body
// says which happened either way.
func (h *Run) Cancel(w http.ResponseWriter, r *http.Request) {
	who, ok := signedIn(w, r)
	if !ok {
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "the run id is not a uuid")
		return
	}

	outcome, err := h.runs.Cancel(r.Context(), who, id)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response := dto.CancelRunResponse{
		ID:      id.String(),
		Status:  string(domain.StatusCancelled),
		Outcome: string(outcome),
	}
	status := http.StatusOK

	if outcome == domain.CancellationRequested {
		// Still running, and that is the honest thing to report: the generator
		// is destroyed by the worker executing it, within a few seconds.
		response.Status = string(domain.StatusRunning)
		status = http.StatusAccepted
	}

	writeJSON(w, status, response)
}

// StopAll handles POST /runs/stop-all.
//
// The emergency stop. Reserved to somebody who administers every project of the
// organization, which the service checks project by project; anything less is
// refused rather than given a stop that leaves part of the load running.
func (h *Run) StopAll(w http.ResponseWriter, r *http.Request) {
	who, ok := signedIn(w, r)
	if !ok {
		return
	}

	cancelled, asked, err := h.runs.StopEverything(r.Context(), who)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, dto.StopAllResponse{
		Cancelled:   cancelled,
		AskedToStop: asked,
	})
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

	case errors.Is(err, domain.ErrForbidden):
		// The caller holds no right to run in this project. A run they may not
		// read answers 404 instead, from ByID; queueing one is a request they
		// made about a project they named, so there is nothing to hide here.
		writeError(w, http.StatusForbidden, "you do not have permission to do that here")

	case errors.Is(err, domain.ErrTargetNotVerified):
		// The refusal the whole product rests on. 403, and the attempt is
		// logged above, in the service.
		writeError(w, http.StatusForbidden, "this environment has not been verified")

	case errors.Is(err, domain.ErrOutsideAllowedWindow):
		// The hours the target agreed to be loaded. The message carries the
		// window, or how much of it is left against how long the run would
		// last, because the two are fixed differently: wait, or ask for less.
		writeError(w, http.StatusForbidden, err.Error())

	case errors.Is(err, domain.ErrNotCancellable):
		// 409 rather than 400: nothing about the request is wrong, the run has
		// simply already finished. Trying again would not help.
		writeError(w, http.StatusConflict, "this run has already finished")

	case errors.Is(err, domain.ErrRunsSuspended):
		// 503, because it is the installation that is unavailable for this,
		// not the request that is wrong. Somebody threw a switch.
		writeError(w, http.StatusServiceUnavailable, "Ballast is not accepting new runs")

	case errors.Is(err, domain.ErrCrossProject):
		writeError(w, http.StatusBadRequest, "the scenario and the environment belong to different projects")

	case errors.Is(err, domain.ErrInvalidProfileParams):
		writeError(w, http.StatusBadRequest, err.Error())

	default:
		h.log.ErrorContext(r.Context(), "request failed", "path", r.URL.Path, "error", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
	}
}

// signedIn returns who the session names. The middleware has already refused
// a request without one; this is the second lock, for a route that was ever
// registered without the first.
func signedIn(w http.ResponseWriter, r *http.Request) (domain.Identity, bool) {
	who, ok := session.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "sign in to continue")
	}
	return who, ok
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, dto.Error{Error: message})
}

// List handles GET /projects/{projectId}/runs.
func (h *Run) List(w http.ResponseWriter, r *http.Request) {
	who, projectID, ok := projectRequest(w, r)
	if !ok {
		return
	}

	limit, err := listLimit(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var before *run.Cursor
	if raw := r.URL.Query().Get("before"); raw != "" {
		cursor, err := decodeCursor(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "before is not a cursor this API gave out")
			return
		}
		before = &cursor
	}

	page, err := h.runs.InProject(r.Context(), who, projectID, before, limit)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response := dto.RunListResponse{Runs: make([]dto.RunSummary, 0, len(page.Runs))}
	if page.Next != nil {
		response.Next = encodeCursor(*page.Next)
	}
	for _, listed := range page.Runs {
		response.Runs = append(response.Runs, dto.RunSummary{
			ID:              listed.ID.String(),
			ProjectID:       listed.ProjectID.String(),
			ScenarioID:      listed.ScenarioID.String(),
			EnvironmentID:   listed.EnvironmentID.String(),
			ScenarioVersion: listed.ScenarioVersion,
			Profile:         string(listed.Profile),
			Params: dto.ProfileParams{
				TargetVUs: listed.ProfileParams.TargetVUs,
				RampUpS:   listed.ProfileParams.RampUpS,
				PlateauS:  listed.ProfileParams.PlateauS,
				RampDownS: listed.ProfileParams.RampDownS,
			},
			Status:    string(listed.Status),
			CreatedAt: listed.CreatedAt,
			StartedAt: listed.StartedAt,
			EndedAt:   listed.EndedAt,
		})
	}

	writeJSON(w, http.StatusOK, response)
}

// listLimit reads ?limit=. Absent means the default; the service bounds what
// is given, so only something that is not a number is refused here.
func listLimit(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return 0, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 {
		return 0, errors.New("limit must be a positive whole number")
	}
	return limit, nil
}

// A cursor travels as base64url of "<created_at, RFC 3339 with nanoseconds>
// <space> <run id>". It is opaque to clients: the format may change, so it is
// never documented, only handed back.
func encodeCursor(c run.Cursor) string {
	return base64.RawURLEncoding.EncodeToString([]byte(c.CreatedAt.UTC().Format(time.RFC3339Nano) + " " + c.ID.String()))
}

func decodeCursor(raw string) (run.Cursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return run.Cursor{}, err
	}
	at, id, ok := strings.Cut(string(decoded), " ")
	if !ok {
		return run.Cursor{}, errors.New("cursor: no separator")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return run.Cursor{}, err
	}
	runID, err := uuid.Parse(id)
	if err != nil {
		return run.Cursor{}, err
	}
	return run.Cursor{CreatedAt: createdAt, ID: runID}, nil
}
