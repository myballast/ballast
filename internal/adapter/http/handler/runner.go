package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/youssefmahersi/ballast/internal/adapter/http/dto"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/runner"
)

// Runner serves the state of the machines that execute runs.
type Runner struct {
	runners *runner.Service
	log     *slog.Logger
}

// NewRunner returns the handler.
func NewRunner(runners *runner.Service, log *slog.Logger) *Runner {
	return &Runner{runners: runners, log: log}
}

// List handles GET /runners.
//
// Reserved to an administrator of every project of the organization. It is a
// state of the whole platform, and somebody who administers only part of it is
// refused rather than shown a picture that leaves out the rest.
func (h *Runner) List(w http.ResponseWriter, r *http.Request) {
	who, ok := signedIn(w, r)
	if !ok {
		return
	}

	overview, err := h.runners.Overview(r.Context(), who)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response := dto.RunnerListResponse{
		Runners:         make([]dto.Runner, 0, len(overview.Runners)),
		MinimumContract: overview.MinimumContract,
	}
	for _, view := range overview.Runners {
		response.Runners = append(response.Runners, dto.Runner{
			Name:       view.Runner.Name,
			Version:    view.Runner.Version,
			Contract:   view.Runner.Contract,
			Status:     string(view.Status),
			Outdated:   view.Outdated,
			StartedAt:  view.Runner.StartedAt,
			LastSeenAt: view.Runner.HeartbeatAt,
		})
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Runner) writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrForbidden):
		writeError(w, http.StatusForbidden, "only an administrator of every project of the organization may read its runners")

	default:
		h.log.ErrorContext(r.Context(), "request failed", "path", r.URL.Path, "error", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
	}
}
