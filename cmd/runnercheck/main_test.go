package main

import (
	"testing"
	"time"

	"github.com/youssefmahersi/ballast/internal/domain"
)

var instant = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

func runner(name string, state domain.RunnerState, silentFor time.Duration) domain.Runner {
	return domain.Runner{
		Name: name, Version: "a1b2c3d", Contract: domain.RunnerContract, State: state,
		StartedAt: instant.Add(-time.Hour), HeartbeatAt: instant.Add(-silentFor),
	}
}

// What the deployment tests ask: is this runner what I expect it to be, now? The
// answer is read from the same fields, with the same rule, as the Runners screen,
// so that "online" here and "online" there cannot disagree.
func TestARunnerIsWhatTheScreenWouldSay(t *testing.T) {
	cases := []struct {
		name    string
		runners []domain.Runner
		expect  string
		want    bool
	}{
		{"online, as expected", []domain.Runner{runner("vps-1", domain.RunnerServing, 3*time.Second)}, "online", true},
		{"online, but expected offline", []domain.Runner{runner("vps-1", domain.RunnerServing, 3*time.Second)}, "offline", false},
		{"silent too long is offline", []domain.Runner{runner("vps-1", domain.RunnerServing, time.Minute)}, "offline", true},
		{"a clean stop is offline at once", []domain.Runner{runner("vps-1", domain.RunnerStopped, time.Second)}, "offline", true},
		{"draining", []domain.Runner{runner("vps-1", domain.RunnerDraining, time.Second)}, "draining", true},
		{"a runner that never reported is absent", nil, "absent", true},
		{"a runner that never reported is not online", nil, "online", false},
		{"another runner's row is not this one's", []domain.Runner{runner("vps-2", domain.RunnerServing, time.Second)}, "online", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, saw := check(c.runners, "vps-1", instant, c.expect)
			if got != c.want {
				t.Fatalf("check = %v (%s), want %v", got, saw, c.want)
			}
			if saw == "" {
				t.Fatal("what was seen is not said: a failure that does not say what it found is a puzzle")
			}
		})
	}
}

// A misspelt expectation must not read as a failure of the runner, nor as a pass.
func TestAnUnknownExpectationIsRefused(t *testing.T) {
	if _, err := parseExpectation("onlien"); err == nil {
		t.Fatal("an expectation that is not one was accepted")
	}
	for _, valid := range []string{"online", "draining", "offline", "absent"} {
		if _, err := parseExpectation(valid); err != nil {
			t.Fatalf("%q was refused: %v", valid, err)
		}
	}
}
