package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The command line itself: what is typed, what is read from the environment, and
// what the tool says when it is used wrongly.
//
// What these tests hold in place:
//
//   - the token is read from BALLAST_TOKEN and from nowhere else — an argument would
//     show in a process list and in the log of the pipeline that typed it — and it is
//     never printed, including when somebody types it where it does not belong;
//   - every way of being used wrongly leaves with 2, says what to do, and starts
//     nothing;
//   - there is no --project, and the tool says why, rather than accepting a word it
//     does nothing with.
//
// Written before the code, which is why they do not compile yet.

type invocation struct {
	code     int
	out, err string
}

func invoke(t *testing.T, b *ballast, environment []string, args ...string) invocation {
	t.Helper()

	server := httptest.NewServer(b.handler())
	t.Cleanup(server.Close)

	env := envOf(append([]string{"BALLAST_URL", server.URL, "BALLAST_TOKEN", aToken}, environment...)...)
	return invokeWith(t, env, args...)
}

func invokeWith(t *testing.T, env func(string) string, args ...string) invocation {
	t.Helper()

	var out, errs bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	code := Main(ctx, nil, args, env, &out, &errs)
	return invocation{code: code, out: out.String(), err: errs.String()}
}

func runArgs(extra ...string) []string {
	return append([]string{"run", "--scenario", scenarioID, "--environment", environmentID, "--profile", "smoke"}, extra...)
}

// --- Using it ---------------------------------------------------------------------------------------------

func TestRunLaunchesWhatItIsAskedForAndLeavesWithTheCode(t *testing.T) {
	b := &ballast{}

	got := invoke(t, b, nil, runArgs()...)

	if got.code != ExitMet {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", got.code, got.out, got.err)
	}
	body := b.launchBody()
	if body["scenario_id"] != scenarioID || body["environment_id"] != environmentID || body["profile"] != "smoke" {
		t.Errorf("launched %v", body)
	}
	if !strings.Contains(got.out, "Verdict: met") {
		t.Errorf("stdout:\n%s", got.out)
	}
}

func TestTheFlagsReachTheRun(t *testing.T) {
	t.Run("--no-fail-on-breach", func(t *testing.T) {
		b := &ballast{poll: pollsIn(state("failed", "breached", broken))}

		if got := invoke(t, b, nil, runArgs("--no-fail-on-breach")...); got.code != ExitMet {
			t.Errorf("exit %d, want 0", got.code)
		}
		if got := invoke(t, &ballast{poll: pollsIn(state("failed", "breached", broken))}, nil, runArgs()...); got.code != ExitBreached {
			t.Errorf("without the flag: exit %d, want 1", got.code)
		}
	})

	t.Run("--allow-no-threshold", func(t *testing.T) {
		if got := invoke(t, &ballast{poll: pollsIn(state("passed", "none"))}, nil, runArgs("--allow-no-threshold")...); got.code != ExitMet {
			t.Errorf("exit %d, want 0", got.code)
		}
		if got := invoke(t, &ballast{poll: pollsIn(state("passed", "none"))}, nil, runArgs()...); got.code != ExitError {
			t.Errorf("without the flag: exit %d, want 2", got.code)
		}
	})

	t.Run("--timeout", func(t *testing.T) {
		b := &ballast{poll: running()}

		start := time.Now()
		got := invoke(t, b, nil, runArgs("--timeout", "200ms")...)

		if got.code != ExitError || b.deletes.Load() != 1 {
			t.Errorf("exit %d, %d cancellation(s)\nstderr: %s", got.code, b.deletes.Load(), got.err)
		}
		// The time that was given, and not the harness's own limit, ended it: both
		// leave with 2, and only the message and the clock tell them apart.
		if !strings.Contains(got.err, "timeout of 200ms") || time.Since(start) > 5*time.Second {
			t.Errorf("the run was not ended by the timeout that was given (after %s):\n%s", time.Since(start), got.err)
		}
	})
}

// A server that asks for an hour never makes these wait: the answer does not fit, and
// the tool says what budget it was working with.
func TestTheQueueTimeoutReachesTheRunAndDefaultsToTenMinutes(t *testing.T) {
	tooLong := func(w http.ResponseWriter) {
		w.Header().Set("Retry-After", "3600")
		writeJSON(w, 429, `{"error":"30 runs in the last hour"}`)
	}

	t.Run("the default", func(t *testing.T) {
		b := &ballast{launch: tooLong}
		got := invoke(t, b, nil, runArgs()...)
		if got.code != ExitError || !strings.Contains(got.err, "--queue-timeout of 10m") {
			t.Errorf("exit %d, the default is not ten minutes:\n%s", got.code, got.err)
		}
	})
	t.Run("a given one", func(t *testing.T) {
		b := &ballast{launch: tooLong}
		got := invoke(t, b, nil, runArgs("--queue-timeout", "90s")...)
		if !strings.Contains(got.err, "--queue-timeout of 1m30s") {
			t.Errorf("the value given did not reach the run:\n%s", got.err)
		}
	})
	t.Run("zero leaves at the first refusal", func(t *testing.T) {
		b := &ballast{launch: tooLong}
		got := invoke(t, b, nil, runArgs("--queue-timeout", "0")...)
		if got.code != ExitError || b.posts.Load() != 1 || strings.Contains(got.err, "--queue-timeout") {
			t.Errorf("exit %d, %d launches:\n%s", got.code, b.posts.Load(), got.err)
		}
	})
}

func TestTheEnvironmentOfAGitHubJobIsSentAsCIMetadata(t *testing.T) {
	b := &ballast{}

	got := invoke(t, b, gitHubPush(), runArgs()...)

	if got.code != ExitMet {
		t.Fatalf("exit %d\n%s", got.code, got.err)
	}
	ci, _ := b.launchBody()["ci"].(map[string]any)
	if ci["provider"] != "github_actions" || ci["commit"] != aCommit {
		t.Errorf("ci %v", ci)
	}
}

// --- The token ------------------------------------------------------------------------------------------------

func TestTheTokenIsNeverAcceptedAsAnArgument(t *testing.T) {
	for _, flag := range []string{"--token", "-token", "--api-token", "--password"} {
		for _, form := range []func(string) []string{
			func(v string) []string { return []string{flag + "=" + v} },
			func(v string) []string { return []string{flag, v} },
		} {
			b := &ballast{}
			args := runArgs(form("hunter2-typed-by-mistake")...)

			got := invoke(t, b, nil, args...)

			if got.code != ExitError {
				t.Errorf("%s: exit %d, want 2", flag, got.code)
			}
			if !strings.Contains(got.err, "BALLAST_TOKEN") {
				t.Errorf("%s: the message does not say where the token goes:\n%s", flag, got.err)
			}
			if strings.Contains(got.all(), "hunter2-typed-by-mistake") {
				t.Errorf("%s: what was typed was printed back:\n%s", flag, got.all())
			}
			if b.posts.Load() != 0 {
				t.Errorf("%s: a run was launched with an argument that is refused", flag)
			}
		}
	}
}

// Whatever is typed anywhere, the token is not printed: not as a value that does not
// parse, not as a word the tool does not know.
func TestTheTokenIsNeverPrintedWhereverItIsTyped(t *testing.T) {
	for name, args := range map[string][]string{
		"as a timeout":         runArgs("--timeout", aToken),
		"as a scenario":        {"run", "--scenario", aToken, "--environment", environmentID, "--profile", "smoke"},
		"as a profile":         {"run", "--scenario", scenarioID, "--environment", environmentID, "--profile", aToken},
		"as a command":         {aToken},
		"as a flag":            {"run", "--" + aToken},
		"as a positional word": runArgs(aToken),
	} {
		t.Run(name, func(t *testing.T) {
			b := &ballast{}

			got := invoke(t, b, nil, args...)

			if got.code != ExitError {
				t.Errorf("exit %d, want 2", got.code)
			}
			if strings.Contains(got.all(), aToken) || strings.Contains(got.all(), aToken[4:]) {
				t.Errorf("the token was printed:\n%s", got.all())
			}
			if b.posts.Load() != 0 {
				t.Error("a run was launched")
			}
		})
	}
}

func TestTheEnvironmentMustHoldTheAddressAndTheToken(t *testing.T) {
	cases := map[string]struct {
		env  []string
		want string
	}{
		"no address":                {[]string{"BALLAST_TOKEN", aToken}, "BALLAST_URL"},
		"no token":                  {[]string{"BALLAST_URL", "https://ballast.example.test"}, "BALLAST_TOKEN"},
		"an address that is unsafe": {[]string{"BALLAST_URL", "http://ballast.example.test", "BALLAST_TOKEN", aToken}, "BALLAST_URL"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := invokeWith(t, envOf(c.env...), runArgs()...)

			if got.code != ExitError || !strings.Contains(got.err, c.want) {
				t.Errorf("exit %d\n%s", got.code, got.err)
			}
		})
	}
}

// --- Using it wrongly --------------------------------------------------------------------------------------------

func TestThereIsNoProjectFlagAndTheToolSaysWhy(t *testing.T) {
	for _, form := range [][]string{{"--project", "checkout"}, {"--project=checkout"}, {"-project", "checkout"}} {
		b := &ballast{}

		got := invoke(t, b, nil, runArgs(form...)...)

		if got.code != ExitError {
			t.Errorf("%v: exit %d, want 2", form, got.code)
		}
		// Not "flag provided but not defined": the reason, and where the project is.
		if !strings.Contains(got.err, "token") || !strings.Contains(got.err, "scenario") {
			t.Errorf("%v: the message does not say why:\n%s", form, got.err)
		}
		if b.posts.Load() != 0 {
			t.Errorf("%v: a run was launched", form)
		}
	}
}

func TestWhatIsUsedWrongLeavesWithTwoAndStartsNothing(t *testing.T) {
	cases := map[string][]string{
		"no command":                      {},
		"a command that does not exist":   {"launch"},
		"an unknown flag":                 runArgs("--verbose"),
		"a word that is not a flag":       runArgs("staging"),
		"a timeout that is not one":       runArgs("--timeout", "soon"),
		"a queue timeout that is not one": runArgs("--queue-timeout", "soon"),
		"a negative queue timeout":        runArgs("--queue-timeout", "-5m"),
		"no scenario":                     {"run", "--environment", environmentID, "--profile", "smoke"},
		"no environment":                  {"run", "--scenario", scenarioID, "--profile", "smoke"},
		"no profile":                      {"run", "--scenario", scenarioID, "--environment", environmentID},
		"a scenario that is not one":      {"run", "--scenario", "checkout", "--environment", environmentID, "--profile", "smoke"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			b := &ballast{}

			got := invoke(t, b, nil, args...)

			if got.code != ExitError {
				t.Errorf("exit %d, want 2\n%s", got.code, got.err)
			}
			if got.err == "" {
				t.Error("nothing was said")
			}
			if b.posts.Load() != 0 {
				t.Error("a run was launched")
			}
		})
	}
}

// What is typed in the place of a command is not repeated: it is whatever was in the
// clipboard, and a message that quotes it is a message that prints it.
func TestAWordTypedInThePlaceOfACommandIsNotRepeated(t *testing.T) {
	got := invokeWith(t, envOf(), "hunter2-typed-by-mistake")

	if got.code != ExitError || strings.Contains(got.all(), "hunter2") {
		t.Errorf("exit %d:\n%s", got.code, got.all())
	}
}

// Asking for help is not a mistake.
func TestHelpIsAnAnswerAndNotAnError(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"--help"}, {"-h"}, {"run", "-h"}, {"run", "--help"}} {
		got := invokeWith(t, envOf(), args...)

		if got.code != 0 {
			t.Errorf("%v: exit %d, want 0", args, got.code)
		}
		if !strings.Contains(got.out, "BALLAST_TOKEN") {
			t.Errorf("%v: the help does not say where the token goes:\n%s", args, got.out)
		}
	}
}

// The help is what a person reads in the middle of a broken pipeline: it says what
// every code means, where each setting comes from, and what is left to the defaults.
func TestTheHelpSaysWhatAPipelineNeedsToKnow(t *testing.T) {
	got := invokeWith(t, envOf(), "help")

	for _, want := range []string{
		"BALLAST_URL", "BALLAST_TOKEN",
		"--scenario", "--environment", "--profile", "--timeout", "--no-fail-on-breach", "--allow-no-threshold",
		"2h", // the default timeout, so that nobody finds it out by waiting
		"--queue-timeout", "10m",
		"0", "1", "2",
	} {
		if !strings.Contains(got.out, want) {
			t.Errorf("the help lacks %q:\n%s", want, got.out)
		}
	}
	if strings.Contains(got.out, "--project") && !strings.Contains(got.out, "no --project") {
		t.Errorf("the help offers a --project:\n%s", got.out)
	}
	if strings.Contains(got.out, "--token") {
		t.Errorf("the help offers a --token:\n%s", got.out)
	}
}

func (i invocation) all() string { return i.out + "\n" + i.err }
