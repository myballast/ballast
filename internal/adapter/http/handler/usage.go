package handler

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/adapter/http/dto"
	"github.com/youssefmahersi/ballast/internal/domain"
)

// Usage handles GET /usage: what the whole organization moved in a month.
//
// Reserved to an administrator of every project of it. A figure about the whole
// organization is the whole organization's, and somebody who administers part
// of it is refused rather than handed a total that silently leaves out the rest.
func (h *Run) Usage(w http.ResponseWriter, r *http.Request) {
	who, ok := signedIn(w, r)
	if !ok {
		return
	}

	from, to, ok := monthAsked(w, r)
	if !ok {
		return
	}

	usage, err := h.runs.UsageInMonth(r.Context(), who, from, to)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, usageResponse(usage))
}

// ProjectUsage handles GET /projects/{projectId}/usage.
func (h *Run) ProjectUsage(w http.ResponseWriter, r *http.Request) {
	who, ok := signedIn(w, r)
	if !ok {
		return
	}

	projectID, err := uuid.Parse(r.PathValue("projectId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "the project id is not a uuid")
		return
	}

	from, to, ok := monthAsked(w, r)
	if !ok {
		return
	}

	usage, err := h.runs.ProjectUsageInMonth(r.Context(), who, projectID, from, to)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, usageResponse(usage))
}

// monthAsked reads the period, defaulting to the month now in UTC.
//
// The default is a convenience; the timezone is not. A figure read as a cost
// must not depend on where the reader happens to be, so a month is a month in
// UTC for everybody.
func monthAsked(w http.ResponseWriter, r *http.Request) (from, to time.Time, ok bool) {
	raw := r.URL.Query().Get("month")
	if raw == "" {
		from, to = domain.MonthOf(time.Now())
		return from, to, true
	}

	from, to, err := domain.ParseMonth(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "month must be written as YYYY-MM")
		return time.Time{}, time.Time{}, false
	}

	return from, to, true
}

func usageResponse(usage domain.Usage) dto.Usage {
	return dto.Usage{
		From:              usage.From,
		To:                usage.To,
		EgressBytes:       usage.EgressBytes,
		IngressBytes:      usage.IngressBytes,
		DataSentBytes:     usage.DataSentBytes,
		DataReceivedBytes: usage.DataReceivedBytes,
		Runs:              usage.Runs,
		UnmeasuredRuns:    usage.UnmeasuredRuns,
	}
}
