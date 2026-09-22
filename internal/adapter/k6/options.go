package k6

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// ErrUnknownProfile reports a profile kind that has no k6 translation.
var ErrUnknownProfile = errors.New("k6: unknown load profile")

// ErrInvalidThreshold reports a threshold that would produce a k6 options block
// k6 refuses to load.
var ErrInvalidThreshold = errors.New("k6: invalid threshold")

// Our parameters describe concurrent users rather than a request rate, which
// rules out the arrival-rate executors and leaves these two.
const (
	executorConstantVUs = "constant-vus"
	executorRampingVUs  = "ramping-vus"
)

// k6 waits this long for in-flight iterations after a scenario's time is up.
// Its own default is 30s, which would let a run declared at 60 seconds take 90
// and quietly make environment.max_duration_s something other than the
// duration. Zero costs the iterations still in flight at the very end, which
// show up as errors in the last seconds of a run; a ceiling that does not hold
// costs more.
const gracefulStop = "0s"

// Options is the k6 options object, as k6 reads it.
type Options struct {
	Scenarios  map[string]Scenario `json:"scenarios"`
	Thresholds map[string][]string `json:"thresholds,omitempty"`
}

// Scenario is one entry of options.scenarios. Which fields carry meaning
// depends on Executor: constant-vus reads VUs and Duration, ramping-vus reads
// StartVUs and Stages.
type Scenario struct {
	Executor string `json:"executor"`

	VUs      int    `json:"vus,omitempty"`
	Duration string `json:"duration,omitempty"`

	// A pointer, because zero is the value that matters and must be written
	// out: k6 starts a ramp at one virtual user when startVUs is absent.
	StartVUs *int    `json:"startVUs,omitempty"`
	Stages   []Stage `json:"stages,omitempty"`

	GracefulStop string `json:"gracefulStop"`
}

// Stage is one leg of a ramping-vus scenario: reach Target over Duration,
// starting from wherever the previous leg ended.
type Stage struct {
	Duration string `json:"duration"`
	Target   int    `json:"target"`
}

// BuildOptions translates a load profile and its thresholds into the k6 options
// object.
//
// params are expected to have been clamped to the environment already. This
// function does not clamp: it refuses parameters that describe no runnable
// test, and otherwise reproduces exactly what it was given. Silently shrinking
// a profile here would put a second, invisible ceiling in the pipeline.
func BuildOptions(
	kind domain.ProfileKind,
	params domain.ProfileParams,
	thresholds []domain.Threshold,
) (Options, error) {
	if _, ok := kind.Defaults(); !ok {
		return Options{}, fmt.Errorf("%w: %q", ErrUnknownProfile, kind)
	}
	if err := params.Validate(); err != nil {
		return Options{}, err
	}

	rules, err := thresholdsFor(thresholds)
	if err != nil {
		return Options{}, err
	}

	// Naming the scenario after the profile is what makes k6's own output and
	// its summary readable to whoever launched the run.
	return Options{
		Scenarios:  map[string]Scenario{string(kind): scenarioFor(params)},
		Thresholds: rules,
	}, nil
}

// JSON renders the options as k6 reads them, indented so that a human debugging
// a run can read the file the worker handed over.
//
// HTML escaping is off. Threshold expressions are full of < and >, and the
// default encoder turns p(95)<300 into p(95)\u003c300 — which k6 parses back
// correctly and which nobody can read, defeating the point of indenting.
func (o Options) JSON() ([]byte, error) {
	var buf bytes.Buffer

	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")

	if err := enc.Encode(o); err != nil {
		return nil, err
	}

	// Encode appends a newline of its own.
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func scenarioFor(p domain.ProfileParams) Scenario {
	// With no ramp at either end the load is flat, and constant-vus says that
	// plainly. A single-stage ramping-vus would describe the same run while
	// reading, in k6's output, like something that ramps.
	if p.RampUpS == 0 && p.RampDownS == 0 {
		return Scenario{
			Executor:     executorConstantVUs,
			VUs:          p.TargetVUs,
			Duration:     seconds(p.TotalDuration()),
			GracefulStop: gracefulStop,
		}
	}

	startVUs := 0
	var stages []Stage

	if p.RampUpS > 0 {
		stages = append(stages, Stage{Duration: seconds(p.RampUpS), Target: p.TargetVUs})
	} else {
		// No climb, so the plateau has to begin at full load.
		startVUs = p.TargetVUs
	}
	if p.PlateauS > 0 {
		stages = append(stages, Stage{Duration: seconds(p.PlateauS), Target: p.TargetVUs})
	}
	if p.RampDownS > 0 {
		stages = append(stages, Stage{Duration: seconds(p.RampDownS), Target: 0})
	}

	return Scenario{
		Executor:     executorRampingVUs,
		StartVUs:     &startVUs,
		Stages:       stages,
		GracefulStop: gracefulStop,
	}
}

// thresholdsFor groups the rules by metric, which is the shape k6 expects. The
// metric keeps whatever tag selector it carries; k6 parses it, Ballast does
// not.
func thresholdsFor(thresholds []domain.Threshold) (map[string][]string, error) {
	if len(thresholds) == 0 {
		return nil, nil
	}

	rules := make(map[string][]string, len(thresholds))
	for _, t := range thresholds {
		switch {
		case t.Metric == "":
			return nil, fmt.Errorf("%w: a threshold has no metric", ErrInvalidThreshold)
		case t.Expression == "":
			return nil, fmt.Errorf("%w: %s has no expression", ErrInvalidThreshold, t.Metric)
		}
		rules[t.Metric] = append(rules[t.Metric], t.Expression)
	}

	return rules, nil
}

// seconds renders a duration the one way, so that two identical runs produce
// two identical options files. k6 accepts 1m and 1h30m just as well; picking a
// single form keeps the generated block comparable.
func seconds(s int) string {
	return strconv.Itoa(s) + "s"
}
