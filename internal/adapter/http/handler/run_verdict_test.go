package handler

import (
	"encoding/json"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/run"
)

// What a pipeline reads from GET /runs/{id}: one word for the thresholds and the
// address of the page a person opens. Both are added to the response, and the
// fields the front end already reads stay exactly as they were.
//
// Written before the code, which is why they do not compile yet.

var theRunID = uuid.MustParse("11111111-2222-4333-8444-555555555555")

func aDetail(status domain.RunStatus) run.Listed {
	started := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	return run.Listed{
		Run: domain.Run{
			ID: theRunID, Number: 7, ProjectID: uuid.New(), ScenarioID: uuid.New(),
			EnvironmentID: uuid.New(), ScenarioVersion: 3, Profile: domain.ProfileSmoke,
			Status: status, CreatedAt: started, StartedAt: &started,
		},
		TriggeredByName: "Ada",
	}
}

func webAt(t *testing.T, raw string) *url.URL {
	t.Helper()

	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return parsed
}

func asObject(t *testing.T, value any) map[string]any {
	t.Helper()

	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return object
}

func TestTheRunSaysWhetherItsThresholdsHeld(t *testing.T) {
	held := domain.ThresholdResult{Metric: "http_req_duration", Expression: "p(95)<500", Passed: true}
	broken := domain.ThresholdResult{Metric: "http_req_failed", Expression: "rate<0.01"}

	cases := []struct {
		name       string
		status     domain.RunStatus
		thresholds []domain.ThresholdResult
		want       string
	}{
		{"passed", domain.StatusPassed, []domain.ThresholdResult{held}, "met"},
		{"failed", domain.StatusFailed, []domain.ThresholdResult{held, broken}, "breached"},
		{"a run with no threshold", domain.StatusPassed, nil, "none"},
		// A run that has not produced results has nothing to say, and says so
		// rather than "met".
		{"still running", domain.StatusRunning, nil, "none"},
		{"queued", domain.StatusQueued, nil, "none"},
		{"errored", domain.StatusErrored, nil, "none"},
		{"cancelled", domain.StatusCancelled, nil, "none"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			response := toDTORun(aDetail(c.status), domain.RunResults{Thresholds: c.thresholds}, nil)

			if response.Verdict != c.want {
				t.Errorf("verdict %q, want %q", response.Verdict, c.want)
			}
			if got := asObject(t, response)["verdict"]; got != c.want {
				t.Errorf("verdict in the JSON %v, want %q: it is always present, never omitted", got, c.want)
			}
		})
	}
}

// What the front end reads is untouched: every field that was there is still
// there, and the only keys added are the two this ticket is about.
func TestTheResponseKeepsEveryFieldItHadAndAddsTwo(t *testing.T) {
	response := toDTORun(aDetail(domain.StatusPassed), domain.RunResults{}, webAt(t, "https://app.ballast.test"))
	object := asObject(t, response)

	before := []string{
		"id", "number", "project_id", "scenario_id", "environment_id", "scenario_version",
		"profile", "params", "status", "triggered_by", "created_at", "started_at",
		"metrics", "thresholds",
	}
	for _, key := range before {
		if _, ok := object[key]; !ok {
			t.Errorf("the field %q is gone from the response", key)
		}
	}

	var got []string
	for key := range object {
		got = append(got, key)
	}
	sort.Strings(got)

	allowed := map[string]bool{"verdict": true, "url": true}
	known := map[string]bool{}
	for _, key := range before {
		known[key] = true
	}
	for _, key := range got {
		if !known[key] && !allowed[key] {
			t.Errorf("the field %q appeared in the response and is not one of the two added", key)
		}
	}

	if object["id"] != theRunID.String() || object["status"] != "passed" || object["triggered_by"] != "Ada" {
		t.Errorf("values changed: id %v, status %v, triggered_by %v", object["id"], object["status"], object["triggered_by"])
	}
}

func TestTheRunCarriesTheAddressOfItsPage(t *testing.T) {
	cases := []struct{ base, want string }{
		{"https://app.ballast.test", "https://app.ballast.test/runs/" + theRunID.String()},
		{"https://app.ballast.test/", "https://app.ballast.test/runs/" + theRunID.String()},
		{"http://localhost:3000", "http://localhost:3000/runs/" + theRunID.String()},
		// A front end served under a prefix keeps it.
		{"https://example.test/ballast", "https://example.test/ballast/runs/" + theRunID.String()},
		// Whatever is on the configured address besides where the front end lives
		// is not part of a link to a run.
		{"https://app.ballast.test/?x=1#top", "https://app.ballast.test/runs/" + theRunID.String()},
	}

	for _, c := range cases {
		got := runPageURL(webAt(t, c.base), theRunID)
		if got != c.want {
			t.Errorf("base %q: %q, want %q", c.base, got, c.want)
		}
	}
}

// With no configured address the field is absent. An address built from nothing,
// or from the request's own Host header — which the caller chooses — is worse
// than none.
func TestWithoutAConfiguredFrontEndThereIsNoAddress(t *testing.T) {
	response := toDTORun(aDetail(domain.StatusPassed), domain.RunResults{}, nil)

	if response.URL != "" {
		t.Errorf("url %q, want none", response.URL)
	}
	if _, ok := asObject(t, response)["url"]; ok {
		t.Error("the url key is present with no front end configured")
	}
	if strings.Contains(runPageURL(nil, theRunID), "runs") {
		t.Error("an address was built from nothing")
	}
}
