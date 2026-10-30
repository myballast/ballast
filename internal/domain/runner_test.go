package domain

import (
	"strings"
	"testing"
	"time"
)

var anInstant = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

func aRunner(state RunnerState, heardAgo time.Duration) Runner {
	return Runner{
		Name:        "vps-1",
		Version:     "a1b2c3d",
		Contract:    RunnerContract,
		State:       state,
		StartedAt:   anInstant.Add(-time.Hour),
		HeartbeatAt: anInstant.Add(-heardAgo),
	}
}

// Online is read, never stored: a worker that was killed without a word is
// offline because nothing was said, and nothing had to write that down.
func TestARunnerIsOnlineWhileItKeepsTalking(t *testing.T) {
	cases := []struct {
		name   string
		runner Runner
		want   RunnerStatus
	}{
		{"just spoke", aRunner(RunnerServing, 0), RunnerOnline},
		{"spoke between two beats", aRunner(RunnerServing, RunnerHeartbeatEvery+time.Second), RunnerOnline},
		{"exactly at the limit", aRunner(RunnerServing, RunnerOfflineAfter), RunnerOnline},
		{"a second past the limit", aRunner(RunnerServing, RunnerOfflineAfter+time.Second), RunnerOffline},
		{"gone for an hour", aRunner(RunnerServing, time.Hour), RunnerOffline},
		// The database's clock and the worker's can differ by a little. A beat
		// that looks like it comes from the future is a fresh one.
		{"a clock a few seconds ahead", aRunner(RunnerServing, -5*time.Second), RunnerOnline},

		{"draining and fresh", aRunner(RunnerDraining, time.Second), RunnerStatusDraining},
		{"draining and silent", aRunner(RunnerDraining, RunnerOfflineAfter+time.Second), RunnerOffline},

		// A clean exit says so, and is believed even a second after it wrote.
		{"stopped, a second ago", aRunner(RunnerStopped, time.Second), RunnerOffline},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.runner.StatusAt(anInstant); got != c.want {
				t.Fatalf("status = %q, want %q", got, c.want)
			}
		})
	}
}

// A single late write must not flip a runner offline: the limit has to leave
// room for beats that were missed.
func TestTheOfflineLimitToleratesMissedBeats(t *testing.T) {
	if RunnerOfflineAfter < 3*RunnerHeartbeatEvery {
		t.Fatalf("a runner is offline after %s but beats every %s: two late writes would be enough",
			RunnerOfflineAfter, RunnerHeartbeatEvery)
	}
}

func TestARunnerBelowTheMinimumContractIsOutdated(t *testing.T) {
	runner := aRunner(RunnerServing, 0)
	runner.Contract = 2

	if !runner.Outdated(3) {
		t.Fatal("a contract under the minimum was not outdated")
	}
	if runner.Outdated(2) {
		t.Fatal("a contract at the minimum was outdated")
	}
	if runner.Outdated(1) {
		t.Fatal("a contract over the minimum was outdated")
	}
}

// The checks the table makes, said in the domain too, so that no other writer
// can store a row the database would refuse.
func TestARunnerThatTheTableWouldRefuseIsRefusedHere(t *testing.T) {
	cases := map[string]func(*Runner){
		"no name":              func(r *Runner) { r.Name = "" },
		"a name too long":      func(r *Runner) { r.Name = strings.Repeat("x", 81) },
		"no version":           func(r *Runner) { r.Version = "" },
		"a version too long":   func(r *Runner) { r.Version = strings.Repeat("x", 65) },
		"no contract":          func(r *Runner) { r.Contract = 0 },
		"a contract too large": func(r *Runner) { r.Contract = MaxRunnerContract + 1 },
		"an unknown state":     func(r *Runner) { r.State = "asleep" },
	}

	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			runner := aRunner(RunnerServing, 0)
			breakIt(&runner)

			if err := runner.Validate(); err == nil {
				t.Fatal("an invalid runner was accepted")
			}
		})
	}

	valid := aRunner(RunnerServing, 0)
	if err := valid.Validate(); err != nil {
		t.Fatalf("a valid runner was refused: %v", err)
	}
}
