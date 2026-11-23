package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/youssefmahersi/ballast/internal/adapter/http/dto"
	"github.com/youssefmahersi/ballast/internal/domain"
)

// What a pipeline says about itself crosses the API twice: in the body of the
// request that launches a run, and in the page of the run that was launched. What
// these tests hold in place is that it crosses as itself, that a mistake in it is
// told as one, and that it stays out of the places it does not belong.
//
// Written before the code, which is why they do not compile yet.

func decodeRequest(t *testing.T, body string) dto.EnqueueRunRequest {
	t.Helper()

	var request dto.EnqueueRunRequest
	if err := json.Unmarshal([]byte(body), &request); err != nil {
		t.Fatalf("the body is not a request: %v", err)
	}
	return request
}

func TestTheCIObjectOfTheBodyBecomesWhatThePipelineSaid(t *testing.T) {
	request := decodeRequest(t, `{
		"scenario_id": "s", "environment_id": "e", "profile": "smoke",
		"ci": {
			"provider": "github_actions", "repository": "myballast/ballast",
			"branch": "feature/ci-metadata", "commit": "3f9c2a1b",
			"job_url": "https://github.com/myballast/ballast/actions/runs/42"
		}
	}`)

	got := commandCI(request)

	want := domain.RunCI{
		Provider:   domain.CIProviderGitHubActions,
		Repository: "myballast/ballast",
		Branch:     "feature/ci-metadata",
		Commit:     "3f9c2a1b",
		JobURL:     "https://github.com/myballast/ballast/actions/runs/42",
	}
	if got == nil || *got != want {
		t.Errorf("got %+v, want %+v — as it was sent: tidying it is the domain's, in one place", got, want)
	}
}

// Without the object, or with it null, there is nothing to keep, and the run is a
// run nobody said anything about.
func TestNoCIObjectMeansNoCIData(t *testing.T) {
	for _, body := range []string{
		`{"scenario_id": "s"}`,
		`{"scenario_id": "s", "ci": null}`,
	} {
		if got := commandCI(decodeRequest(t, body)); got != nil {
			t.Errorf("%s: got %+v, want nothing", body, got)
		}
	}
}

// An object with nothing in it is not nothing: it is a pipeline that did not say
// which CI it is, and the service tells it so. Dropping it here would hide the
// mistake.
func TestAnEmptyCIObjectIsStillAnObject(t *testing.T) {
	got := commandCI(decodeRequest(t, `{"scenario_id": "s", "ci": {}}`))

	if got == nil || *got != (domain.RunCI{}) {
		t.Errorf("got %+v, want an empty object that the domain will refuse", got)
	}
}

// A key that is not one of the five is not kept. The object is a closed set in what
// is stored, and ignoring the rest is how every other body of this API is read.
func TestAKeyThatIsNotOneOfTheFiveIsNotKept(t *testing.T) {
	got := commandCI(decodeRequest(t, `{"ci": {"provider": "other", "author_email": "ada@example.test", "token": "x"}}`))

	if got == nil || *got != (domain.RunCI{Provider: domain.CIProviderOther}) {
		t.Errorf("got %+v, want only the provider", got)
	}
}

// --- The mistake -----------------------------------------------------------------------

func TestAMistakeInTheCIDataIs422WithItsReason(t *testing.T) {
	h := &Run{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	_, err := domain.ParseRunCI(domain.RunCI{Provider: domain.CIProviderOther, JobURL: "http://ci.example.test/job"})
	if err == nil {
		t.Fatal("the domain accepted a plain link; the rest would prove nothing")
	}

	recorder := httptest.NewRecorder()
	h.writeDomainError(recorder, httptest.NewRequest(http.MethodPost, "/runs", nil), err)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422: %s", recorder.Code, recorder.Body)
	}
	if !strings.Contains(recorder.Body.String(), "job_url") {
		t.Errorf("the response does not say which field: %s", recorder.Body)
	}
}

// What was sent is somebody else's text and may be a credential. The reason is the
// domain's own sentence, and it never carries the value.
func TestTheReasonNeverRepeatsWhatWasSent(t *testing.T) {
	h := &Run{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	_, err := domain.ParseRunCI(domain.RunCI{
		Provider: domain.CIProviderOther, JobURL: "https://user:hunter2@ci.example.test/job",
	})

	recorder := httptest.NewRecorder()
	h.writeDomainError(recorder, httptest.NewRequest(http.MethodPost, "/runs", nil), err)

	if recorder.Code != http.StatusUnprocessableEntity || strings.Contains(recorder.Body.String(), "hunter2") {
		t.Errorf("status %d: %s", recorder.Code, recorder.Body)
	}
}

// Wrapped further down, it is still a mistake in the request and not a failure of
// Ballast: a 500 would send somebody looking for a fault that is theirs.
func TestAWrappedMistakeIsStillA422(t *testing.T) {
	h := &Run{log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	recorder := httptest.NewRecorder()
	h.writeDomainError(recorder, httptest.NewRequest(http.MethodPost, "/runs", nil),
		fmt.Errorf("enqueue: %w", fmt.Errorf("%w: provider must be github_actions, gitlab_ci or other", domain.ErrInvalidRunCI)))

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", recorder.Code)
	}
}

// --- The run's page --------------------------------------------------------------------

func TestTheRunsPageCarriesWhatThePipelineSaid(t *testing.T) {
	ci := domain.RunCI{
		Provider:   domain.CIProviderGitLab,
		Repository: "group/project",
		Branch:     "main",
		Commit:     "abcdef1",
		JobURL:     "https://gitlab.com/group/project/-/jobs/42",
	}

	raw, err := json.Marshal(dto.RunResponse{ID: "r", CI: toDTORunCI(&ci)})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var back struct {
		CI map[string]string `json:"ci"`
	}
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	want := map[string]string{
		"provider": "gitlab_ci", "repository": "group/project", "branch": "main",
		"commit": "abcdef1", "job_url": "https://gitlab.com/group/project/-/jobs/42",
	}
	if len(back.CI) != len(want) {
		t.Fatalf("ci = %v, want exactly %v", back.CI, want)
	}
	for k, v := range want {
		if back.CI[k] != v {
			t.Errorf("ci.%s = %q, want %q", k, back.CI[k], v)
		}
	}
}

// A run nobody said anything about has no "ci" key at all, and a field the
// pipeline left out is left out of it too: absence is spelled by leaving the key
// out, as everywhere else in this API.
func TestAbsenceIsSpelledByLeavingTheKeyOut(t *testing.T) {
	raw, _ := json.Marshal(dto.RunResponse{ID: "r", CI: toDTORunCI(nil)})
	if strings.Contains(string(raw), `"ci"`) {
		t.Errorf("a run with no CI data has a ci key: %s", raw)
	}

	partial := domain.RunCI{Provider: domain.CIProviderOther, Commit: "abcdef1"}
	raw, _ = json.Marshal(dto.RunResponse{ID: "r", CI: toDTORunCI(&partial)})
	for _, key := range []string{`"repository"`, `"branch"`, `"job_url"`} {
		if strings.Contains(string(raw), key) {
			t.Errorf("a field the pipeline left out is in the response: %s", raw)
		}
	}
	if !strings.Contains(string(raw), `"provider":"other"`) || !strings.Contains(string(raw), `"commit":"abcdef1"`) {
		t.Errorf("what the pipeline said is missing: %s", raw)
	}
}

// The list a project's runs page reads is unchanged: a run's context is read on the
// run's own page.
func TestTheListOfRunsDoesNotCarryIt(t *testing.T) {
	raw, _ := json.Marshal(dto.RunSummary{ID: "r"})
	if strings.Contains(string(raw), `"ci"`) {
		t.Errorf("the summary of a run carries CI data: %s", raw)
	}
}
