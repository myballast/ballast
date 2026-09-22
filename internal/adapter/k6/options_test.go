package k6_test

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/youssefmahersi/ballast/internal/adapter/k6"
	"github.com/youssefmahersi/ballast/internal/domain"
)

var update = flag.Bool("update", false, "rewrite the golden files instead of comparing against them")

// golden compares got against testdata/<name>.json, or rewrites it under
// -update. The generated options are what k6 is handed, so they are worth
// reading in a review rather than only asserting field by field.
func golden(t *testing.T, name string, got []byte) {
	t.Helper()

	path := filepath.Join("testdata", name+".json")
	got = append(got, '\n')

	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s (run go test -tags= -run %s ./... -update to create it): %v",
			path, t.Name(), err)
	}

	if !bytes.Equal(got, want) {
		t.Errorf("%s does not match\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}

func build(t *testing.T, kind domain.ProfileKind, p domain.ProfileParams, ts []domain.Threshold) []byte {
	t.Helper()

	opts, err := k6.BuildOptions(kind, p, ts)
	if err != nil {
		t.Fatalf("BuildOptions: %v", err)
	}

	raw, err := opts.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	return raw
}

// One golden file per profile, built from the template the domain hardcodes.
func TestBuildOptionsForEveryProfile(t *testing.T) {
	kinds := []domain.ProfileKind{
		domain.ProfileSmoke,
		domain.ProfileLoad,
		domain.ProfileStress,
		domain.ProfileSpike,
		domain.ProfileSoak,
	}

	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			params, ok := kind.Defaults()
			if !ok {
				t.Fatalf("%s has no template", kind)
			}
			golden(t, string(kind), build(t, kind, params, nil))
		})
	}
}

// The tagged syntax is what allows one threshold per endpoint. It travels
// through as an opaque key: k6 parses it, Ballast does not.
func TestBuildOptionsWithTaggedThresholds(t *testing.T) {
	thresholds := []domain.Threshold{
		{Metric: "http_req_duration", Expression: "p(95)<300"},
		{Metric: "http_req_duration", Expression: "p(99)<800"},
		{Metric: "http_req_duration{name:POST /login}", Expression: "p(95)<1000"},
		{Metric: "http_req_failed", Expression: "rate<0.01"},
		{Metric: "login_duration", Expression: "p(95)<1000"},
	}

	params, _ := domain.ProfileLoad.Defaults()
	raw := build(t, domain.ProfileLoad, params, thresholds)

	// The default JSON encoder escapes < and > , turning p(95)<300 into
	// p(95)\u003c300. k6 reads that back correctly and no human does.
	for _, expression := range []string{"p(95)<300", "rate<0.01"} {
		if !bytes.Contains(raw, []byte(expression)) {
			t.Errorf("%s was escaped out of the rendered options:\n%s", expression, raw)
		}
	}

	golden(t, "load_with_thresholds", raw)
}

// What a soak profile becomes once a narrow environment has clamped it. The
// ramp-down is gone, the plateau is cut, and the block still has to be
// coherent k6.
func TestBuildOptionsAfterClamp(t *testing.T) {
	params, _ := domain.ProfileSoak.Defaults()
	clamped := params.ClampTo(domain.Environment{MaxVUs: 5, MaxDurationS: 400})

	golden(t, "soak_clamped", build(t, domain.ProfileSoak, clamped, nil))
}

// A profile clamped down to a flat plateau must come out as constant-vus, not
// as a ramping scenario with one stage.
func TestFlatProfileUsesConstantVUs(t *testing.T) {
	opts, err := k6.BuildOptions(
		domain.ProfileSmoke,
		domain.ProfileParams{TargetVUs: 3, PlateauS: 45},
		nil,
	)
	if err != nil {
		t.Fatalf("BuildOptions: %v", err)
	}

	scenario := opts.Scenarios[string(domain.ProfileSmoke)]
	if scenario.Executor != "constant-vus" {
		t.Errorf("executor is %q, want constant-vus", scenario.Executor)
	}
	if len(scenario.Stages) != 0 {
		t.Errorf("a flat profile produced %d stages", len(scenario.Stages))
	}
}

// k6 starts a ramp at one virtual user when startVUs is absent, so a run would
// begin already under load. Zero has to be written out.
func TestRampingScenarioStartsFromZero(t *testing.T) {
	params, _ := domain.ProfileLoad.Defaults()

	opts, err := k6.BuildOptions(domain.ProfileLoad, params, nil)
	if err != nil {
		t.Fatalf("BuildOptions: %v", err)
	}

	scenario := opts.Scenarios[string(domain.ProfileLoad)]
	if scenario.StartVUs == nil {
		t.Fatal("startVUs is absent, so k6 will start the ramp at one virtual user")
	}
	if *scenario.StartVUs != 0 {
		t.Errorf("startVUs is %d, want 0", *scenario.StartVUs)
	}

	raw, err := opts.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if !bytes.Contains(raw, []byte(`"startVUs": 0`)) {
		t.Errorf("startVUs was dropped from the rendered options:\n%s", raw)
	}
}

// No stage may be emitted with a duration of zero, whatever the clamp left
// behind.
func TestNoZeroDurationStage(t *testing.T) {
	cases := []domain.ProfileParams{
		{TargetVUs: 10, RampUpS: 30, PlateauS: 0, RampDownS: 30},
		{TargetVUs: 10, RampUpS: 0, PlateauS: 0, RampDownS: 30},
		{TargetVUs: 10, RampUpS: 30, PlateauS: 60, RampDownS: 0},
	}

	for _, p := range cases {
		opts, err := k6.BuildOptions(domain.ProfileLoad, p, nil)
		if err != nil {
			t.Fatalf("%+v: %v", p, err)
		}

		for _, stage := range opts.Scenarios[string(domain.ProfileLoad)].Stages {
			if stage.Duration == "0s" {
				t.Errorf("%+v produced a stage of zero duration", p)
			}
		}
	}
}

// The ceiling the environment sets is the ceiling k6 is given. Nothing in this
// package may add to it, and the graceful stop is the part that would do it
// silently.
func TestGeneratedDurationMatchesTheProfileExactly(t *testing.T) {
	kinds := []domain.ProfileKind{
		domain.ProfileSmoke, domain.ProfileLoad, domain.ProfileStress,
		domain.ProfileSpike, domain.ProfileSoak,
	}
	ceilings := []domain.Environment{
		{MaxVUs: 1, MaxDurationS: 1},
		{MaxVUs: 5, MaxDurationS: 400},
		{MaxVUs: 50, MaxDurationS: 3600},
		{MaxVUs: 1000, MaxDurationS: 100000},
	}

	for _, kind := range kinds {
		params, _ := kind.Defaults()

		for _, env := range ceilings {
			clamped := params.ClampTo(env)

			opts, err := k6.BuildOptions(kind, clamped, nil)
			if err != nil {
				t.Fatalf("%s clamped to %+v: %v", kind, env, err)
			}
			scenario := opts.Scenarios[string(kind)]

			if scenario.GracefulStop != "0s" {
				t.Errorf("%s: gracefulStop is %q, so the run may outlast its ceiling",
					kind, scenario.GracefulStop)
			}

			total := parseSeconds(t, scenario.Duration)
			for _, stage := range scenario.Stages {
				total += parseSeconds(t, stage.Duration)
			}

			if total != clamped.TotalDuration() {
				t.Errorf("%s clamped to %+v: k6 is given %ds for a %ds profile",
					kind, env, total, clamped.TotalDuration())
			}
			if total > env.MaxDurationS {
				t.Errorf("%s: k6 is given %ds, the environment allows %ds",
					kind, total, env.MaxDurationS)
			}
		}
	}
}

func parseSeconds(t *testing.T, d string) int {
	t.Helper()

	if d == "" {
		return 0
	}
	if !strings.HasSuffix(d, "s") {
		t.Fatalf("duration %q is not written in seconds", d)
	}

	n, err := strconv.Atoi(strings.TrimSuffix(d, "s"))
	if err != nil {
		t.Fatalf("duration %q: %v", d, err)
	}
	return n
}

func TestBuildOptionsRejectsBadInput(t *testing.T) {
	validParams, _ := domain.ProfileLoad.Defaults()

	cases := []struct {
		name       string
		kind       domain.ProfileKind
		params     domain.ProfileParams
		thresholds []domain.Threshold
		wantErr    error
	}{
		{
			name:    "a profile that is not one of the five",
			kind:    domain.ProfileKind("chaos"),
			params:  validParams,
			wantErr: k6.ErrUnknownProfile,
		},
		{
			name:    "no virtual users",
			kind:    domain.ProfileLoad,
			params:  domain.ProfileParams{TargetVUs: 0, PlateauS: 60},
			wantErr: domain.ErrInvalidProfileParams,
		},
		{
			name:    "no duration",
			kind:    domain.ProfileLoad,
			params:  domain.ProfileParams{TargetVUs: 10},
			wantErr: domain.ErrInvalidProfileParams,
		},
		{
			name:       "a threshold without a metric",
			kind:       domain.ProfileLoad,
			params:     validParams,
			thresholds: []domain.Threshold{{Expression: "p(95)<300"}},
			wantErr:    k6.ErrInvalidThreshold,
		},
		{
			name:       "a threshold without an expression",
			kind:       domain.ProfileLoad,
			params:     validParams,
			thresholds: []domain.Threshold{{Metric: "http_req_duration"}},
			wantErr:    k6.ErrInvalidThreshold,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := k6.BuildOptions(c.kind, c.params, c.thresholds)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("got %v, want %v", err, c.wantErr)
			}
		})
	}
}
