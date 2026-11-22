package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// This is the command itself: launch a run, follow it, wait for it, say how it
// went, and leave with a code a pipeline can act on.
//
//	0   the thresholds held
//	1   a threshold was breached
//	2   anything else: a refusal, an error, a run that did not finish, a result
//	    nobody can read as success
//
// What these tests hold in place is that the code is never wrong in the one
// direction that costs something — a run that was not a success must not leave with
// 0 — and that what is printed never carries the token, whatever the server says.
//
// The fake below answers the four routes the tool uses, as Ballast does.
//
// Written before the code, which is why they do not compile yet.

const (
	scenarioID    = "8f14e45f-ceea-467a-9e2a-3f1f0f5b1f11"
	environmentID = "c9f0f895-fb98-4b91-99f4-1bf0c1f0d6a1"
	theRunID      = "4d7b2c9e-1a55-4e0f-8f64-9a1c3d1e7b20"
	thePageURL    = "https://app.ballast.test/runs/" + theRunID
)

// state is what GET /runs/{id} answers.
func state(status, verdict string, thresholds ...string) string {
	return stateWith(status, verdict, "", thresholds...)
}

func stateWith(status, verdict, errorMessage string, thresholds ...string) string {
	list := "[" + strings.Join(thresholds, ",") + "]"
	return fmt.Sprintf(`{"id":%q,"status":%q,"verdict":%q,"url":%q,"error_message":%q,"thresholds":%s}`,
		theRunID, status, verdict, thePageURL, errorMessage, list)
}

func threshold(metric, expression string, passed bool) string {
	return fmt.Sprintf(`{"metric":%q,"expression":%q,"passed":%t}`, metric, expression, passed)
}

var (
	held   = threshold("http_req_duration", "p(95)<500", true)
	broken = threshold("http_req_failed", "rate<0.01", false)
)

type ballast struct {
	posts, polls, deletes, streams atomic.Int32

	// launched is the body of the last POST /runs.
	launchedMu sync.Mutex
	launched   []byte

	// launch answers POST /runs. Left nil, it queues the run.
	launch func(w http.ResponseWriter)
	// poll answers the nth read of the run, from 1. Left nil, the run has passed.
	poll func(n int, w http.ResponseWriter)
	// logs answers the log stream. Left nil, it ends the run at once.
	logs func(w http.ResponseWriter, r *http.Request)
	// cancel answers DELETE /runs/{id}.
	cancel func(w http.ResponseWriter)
}

// pollsIn serves the given states one after the other, the last one for ever.
func pollsIn(answers ...string) func(int, http.ResponseWriter) {
	return func(n int, w http.ResponseWriter) {
		if n > len(answers) {
			n = len(answers)
		}
		writeJSON(w, 200, answers[n-1])
	}
}

func (b *ballast) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/runs":
			b.posts.Add(1)
			body, _ := io.ReadAll(r.Body)
			b.launchedMu.Lock()
			b.launched = body
			b.launchedMu.Unlock()
			if b.launch != nil {
				b.launch(w)
				return
			}
			writeJSON(w, 202, fmt.Sprintf(`{"id":%q,"status":"queued"}`, theRunID))

		case r.Method == http.MethodGet && r.URL.Path == "/runs/"+theRunID:
			n := int(b.polls.Add(1))
			if b.poll != nil {
				b.poll(n, w)
				return
			}
			writeJSON(w, 200, state("passed", "met", held))

		case r.Method == http.MethodGet && r.URL.Path == "/runs/"+theRunID+"/logs":
			b.streams.Add(1)
			if b.logs != nil {
				b.logs(w, r)
				return
			}
			startStream(w)
			sendEnd(w, "passed")

		case r.Method == http.MethodDelete && r.URL.Path == "/runs/"+theRunID:
			b.deletes.Add(1)
			if b.cancel != nil {
				b.cancel(w)
				return
			}
			writeJSON(w, 202, fmt.Sprintf(`{"id":%q,"status":"running","outcome":"cancellation_requested"}`, theRunID))

		default:
			writeJSON(w, 404, `{"error":"no such route"}`)
		}
	}
}

// launchBody is what the last launch sent.
func (b *ballast) launchBody() map[string]any {
	b.launchedMu.Lock()
	defer b.launchedMu.Unlock()

	var body map[string]any
	_ = json.Unmarshal(b.launched, &body)
	return body
}

type outcome struct {
	code     int
	out, err string
}

func (o outcome) all() string { return o.out + "\n" + o.err }

func wholeRun(t *testing.T, b *ballast, mutate func(*RunOptions, *Runner)) outcome {
	t.Helper()

	// A regression that made the tool loop would otherwise hold the test until the
	// toolchain's own limit; this ends it as a failure within seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	return wholeRunCtx(ctx, t, b, mutate)
}

// wholeRunCtx is wholeRun with a context the test holds: cancelling it is the
// first signal.
func wholeRunCtx(ctx context.Context, t *testing.T, b *ballast, mutate func(*RunOptions, *Runner)) outcome {
	t.Helper()

	client, _ := clientFor(t, b.handler(), WithStreamBackoff(func(int) time.Duration { return time.Millisecond }))

	var out, errs bytes.Buffer
	runner := &Runner{
		Client: client, Out: &out, Err: &errs,
		PollInterval: 5 * time.Millisecond,
		PollBackoff:  func(int) time.Duration { return time.Millisecond },
		DrainWait:    300 * time.Millisecond,
	}
	options := RunOptions{ScenarioID: scenarioID, EnvironmentID: environmentID, Profile: "smoke"}
	if mutate != nil {
		mutate(&options, runner)
	}

	code := runner.Run(ctx, options)
	return outcome{code: code, out: out.String(), err: errs.String()}
}

// --- The exit codes -------------------------------------------------------------------------------------

func TestTheExitCodesAreTheOnesAPipelineReads(t *testing.T) {
	if ExitMet != 0 || ExitBreached != 1 || ExitError != 2 {
		t.Errorf("the codes are %d, %d, %d, want 0, 1, 2", ExitMet, ExitBreached, ExitError)
	}
}

func TestARunWhoseThresholdsHeldLeavesWithZeroAndSaysSo(t *testing.T) {
	got := wholeRun(t, &ballast{poll: pollsIn(state("queued", "none"), state("running", "none"), state("passed", "met", held))}, nil)

	if got.code != ExitMet {
		t.Fatalf("exit %d, want 0\nstdout: %s\nstderr: %s", got.code, got.out, got.err)
	}
	for _, want := range []string{"http_req_duration", "p(95)<500", "met", thePageURL} {
		if !strings.Contains(got.out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, got.out)
		}
	}
}

func TestARunWithABreachedThresholdLeavesWithOneAndShowsWhichOne(t *testing.T) {
	got := wholeRun(t, &ballast{poll: pollsIn(state("failed", "breached", held, broken))}, nil)

	if got.code != ExitBreached {
		t.Fatalf("exit %d, want 1\nstdout: %s\nstderr: %s", got.code, got.out, got.err)
	}
	// The breach is said in words and not by position or by colour: a log has none.
	if !strings.Contains(got.out, "BREACHED") || !strings.Contains(got.out, "http_req_failed") || !strings.Contains(got.out, "rate<0.01") {
		t.Errorf("the table does not show what was breached:\n%s", got.out)
	}
	if !strings.Contains(got.out, thePageURL) {
		t.Errorf("no link to the run:\n%s", got.out)
	}
}

func TestNoFailOnBreachStillShowsTheBreachAndOnlyChangesTheCode(t *testing.T) {
	got := wholeRun(t, &ballast{poll: pollsIn(state("failed", "breached", held, broken))},
		func(o *RunOptions, _ *Runner) { o.NoFailOnBreach = true })

	if got.code != ExitMet {
		t.Errorf("exit %d, want 0", got.code)
	}
	if !strings.Contains(got.out, "BREACHED") {
		t.Errorf("the breach is not shown any more:\n%s", got.out)
	}
	if !strings.Contains(got.err, "no-fail-on-breach") {
		t.Errorf("nothing says the code was kept at 0 on purpose:\n%s", got.err)
	}
}

// --no-fail-on-breach is about a breach, and about nothing else.
func TestNoFailOnBreachDoesNotMakeAnErrorASuccess(t *testing.T) {
	got := wholeRun(t, &ballast{poll: pollsIn(stateWith("errored", "none", "the generator could not start"))},
		func(o *RunOptions, _ *Runner) { o.NoFailOnBreach = true })

	if got.code != ExitError {
		t.Errorf("exit %d, want 2", got.code)
	}
}

// A run that judged nothing cannot say that performance is acceptable, and a
// pipeline that read it as success would go green on a run that measured nothing
// against a rule.
func TestARunThatJudgedNothingIsNotASuccessUnlessTheyAskedForItToBe(t *testing.T) {
	passedWithNoThreshold := &ballast{poll: pollsIn(state("passed", "none"))}

	got := wholeRun(t, passedWithNoThreshold, nil)
	if got.code != ExitError {
		t.Fatalf("exit %d, want 2\nstdout: %s\nstderr: %s", got.code, got.out, got.err)
	}
	if !strings.Contains(got.err, "no threshold") || !strings.Contains(got.err, "--allow-no-threshold") {
		t.Errorf("the message does not say what happened or what to do:\n%s", got.err)
	}

	got = wholeRun(t, &ballast{poll: pollsIn(state("passed", "none"))},
		func(o *RunOptions, _ *Runner) { o.AllowNoThreshold = true })
	if got.code != ExitMet {
		t.Errorf("with --allow-no-threshold: exit %d, want 0", got.code)
	}
	if !strings.Contains(got.err, "no threshold") {
		t.Errorf("a run accepted with no threshold says nothing about it:\n%s", got.err)
	}
}

func TestAllowNoThresholdDoesNotAcceptABreach(t *testing.T) {
	got := wholeRun(t, &ballast{poll: pollsIn(state("failed", "breached", broken))},
		func(o *RunOptions, _ *Runner) { o.AllowNoThreshold = true })

	if got.code != ExitBreached {
		t.Errorf("exit %d, want 1", got.code)
	}
}

func TestARunThatDidNotFinishWellLeavesWithTwoAndSaysWhy(t *testing.T) {
	cases := []struct {
		status, message string
	}{
		{"errored", "the generator's report could not be read"},
		{"timeout", "the run exceeded the environment's maximum duration"},
		{"cancelled", "the run was cancelled"},
		{"rejected", "this environment is closed to runs at this hour"},
		{"skipped", ""},
	}
	for _, c := range cases {
		t.Run(c.status, func(t *testing.T) {
			got := wholeRun(t, &ballast{poll: pollsIn(stateWith(c.status, "none", c.message))}, nil)

			if got.code != ExitError {
				t.Fatalf("exit %d, want 2\nstdout: %s\nstderr: %s", got.code, got.out, got.err)
			}
			if !strings.Contains(got.err, c.status) || !strings.Contains(got.err, c.message) {
				t.Errorf("stderr lacks the status or the reason:\n%s", got.err)
			}
			if !strings.Contains(got.all(), thePageURL) {
				t.Errorf("no link to the run:\n%s", got.all())
			}
		})
	}
}

// A result that contradicts itself, or that this tool cannot read, is not a
// success. The code is read from the server's verdict and checked against its
// status; when they disagree, nobody is to be told the run passed.
func TestAResultThatContradictsItselfOrCannotBeReadLeavesWithTwo(t *testing.T) {
	cases := map[string]string{
		"passed and breached":   state("passed", "breached", broken),
		"failed and met":        state("failed", "met", held),
		"failed and none":       state("failed", "none"),
		"no verdict at all":     state("passed", ""),
		"a verdict from later":  state("passed", "mostly"),
		"met on a cancellation": state("cancelled", "met", held),
	}
	for name, answer := range cases {
		t.Run(name, func(t *testing.T) {
			got := wholeRun(t, &ballast{poll: pollsIn(answer)}, nil)

			if got.code != ExitError {
				t.Errorf("exit %d, want 2\nstdout: %s\nstderr: %s", got.code, got.out, got.err)
			}
		})
	}
}

// --- Launching ------------------------------------------------------------------------------------------------

func TestARefusalToLaunchPrintsTheServersWordsAndLeavesWithTwo(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		header string
		want   []string
	}{
		{"an unverified target", 403, `{"error":"this environment has not been verified"}`, "", []string{"this environment has not been verified"}},
		{"a window that is closed", 403, `{"error":"this environment only accepts runs between 22:00 and 06:00"}`, "", []string{"between 22:00 and 06:00"}},
		{"a bad profile", 422, `{"error":"unknown profile \"burst\""}`, "", []string{`unknown profile "burst"`}},
		{"a limit", 429, `{"error":"you have started 30 runs in the last hour, the most allowed"}`, "120", []string{"30 runs in the last hour", "2m0s"}},
		{"a limit with no wait to give", 429, `{"error":"20 runs are already queued"}`, "", []string{"20 runs are already queued"}},
		{"the switch", 503, `{"error":"Ballast is not accepting new runs"}`, "", []string{"not accepting new runs"}},
		{"a token that is no good", 401, `{"error":"unauthenticated"}`, "", []string{"BALLAST_TOKEN"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := &ballast{launch: func(w http.ResponseWriter) {
				if c.header != "" {
					w.Header().Set("Retry-After", c.header)
				}
				writeJSON(w, c.status, c.body)
			}}

			got := wholeRun(t, b, nil)

			if got.code != ExitError {
				t.Fatalf("exit %d, want 2", got.code)
			}
			for _, want := range c.want {
				if !strings.Contains(got.err, want) {
					t.Errorf("stderr lacks %q:\n%s", want, got.err)
				}
			}
			// Nothing was launched, so nothing is followed or read.
			if b.polls.Load() != 0 || b.streams.Load() != 0 || b.deletes.Load() != 0 {
				t.Errorf("a run that was refused was polled %d, streamed %d, cancelled %d time(s)", b.polls.Load(), b.streams.Load(), b.deletes.Load())
			}
			if got.out != "" {
				t.Errorf("a refusal printed on stdout: %q", got.out)
			}
		})
	}
}

func TestALaunchWhoseOutcomeIsNotKnownSaysSoAndIsNotRepeated(t *testing.T) {
	b := &ballast{launch: func(w http.ResponseWriter) { writeJSON(w, 502, `<html>bad gateway</html>`) }}

	got := wholeRun(t, b, nil)

	if got.code != ExitError {
		t.Errorf("exit %d, want 2", got.code)
	}
	if b.posts.Load() != 1 {
		t.Errorf("the launch was sent %d times, want once", b.posts.Load())
	}
	if !strings.Contains(got.err, "check the project's runs") {
		t.Errorf("nothing says a run may exist:\n%s", got.err)
	}
}

func TestOptionsThatCannotBeRightAreRefusedBeforeAnythingIsSent(t *testing.T) {
	cases := map[string]func(*RunOptions){
		"a scenario that is not an identifier":    func(o *RunOptions) { o.ScenarioID = "hunter2-pasted-by-mistake" },
		"an environment that is not one":          func(o *RunOptions) { o.EnvironmentID = "hunter2-pasted-by-mistake" },
		"no scenario":                             func(o *RunOptions) { o.ScenarioID = "" },
		"no environment":                          func(o *RunOptions) { o.EnvironmentID = "" },
		"no profile":                              func(o *RunOptions) { o.Profile = "" },
		"a profile with something else inside it": func(o *RunOptions) { o.Profile = "hunter2\"}," },
	}
	flags := map[string]string{
		"a scenario that is not an identifier": "--scenario", "an environment that is not one": "--environment",
		"no scenario": "--scenario", "no environment": "--environment", "no profile": "--profile",
		"a profile with something else inside it": "--profile",
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			b := &ballast{}

			got := wholeRun(t, b, func(o *RunOptions, _ *Runner) { change(o) })

			if got.code != ExitError {
				t.Errorf("exit %d, want 2", got.code)
			}
			if !strings.Contains(got.err, flags[name]) {
				t.Errorf("the message does not name %s:\n%s", flags[name], got.err)
			}
			if strings.Contains(got.err, "hunter2") {
				t.Errorf("the message repeats what was typed:\n%s", got.err)
			}
			if b.posts.Load() != 0 {
				t.Error("a request was sent with options that cannot be right")
			}
		})
	}
}

// --- Waiting -----------------------------------------------------------------------------------------------------

func TestTheStatusARunWaitsInIsToldOnceAsItChanges(t *testing.T) {
	got := wholeRun(t, &ballast{poll: pollsIn(
		state("queued", "none"), state("queued", "none"), state("queued", "none"),
		state("running", "none"), state("running", "none"),
		state("passed", "met", held),
	)}, nil)

	if got.code != ExitMet {
		t.Fatalf("exit %d", got.code)
	}
	if n := strings.Count(got.err, "queued"); n != 1 {
		t.Errorf("\"queued\" was said %d times, want once:\n%s", n, got.err)
	}
	if n := strings.Count(got.err, "running"); n != 1 {
		t.Errorf("\"running\" was said %d times, want once:\n%s", n, got.err)
	}
	if !strings.Contains(got.err, theRunID) {
		t.Errorf("the run is never named:\n%s", got.err)
	}
}

func TestAStatusThisVersionDoesNotKnowIsWaitedOutAndNotReadAsAnEnding(t *testing.T) {
	got := wholeRun(t, &ballast{poll: pollsIn(
		state("a_status_from_the_future", "none"),
		state("passed", "met", held),
	)}, nil)

	if got.code != ExitMet {
		t.Errorf("exit %d, want 0: the unknown status was taken as an ending", got.code)
	}
}

// An orphaned run is not over: its generator may still be loading the target.
func TestAnOrphanedRunIsWaitedFor(t *testing.T) {
	b := &ballast{poll: pollsIn(state("orphaned", "none"), state("errored", "none"))}

	got := wholeRun(t, b, nil)

	if got.code != ExitError || b.polls.Load() < 2 {
		t.Errorf("exit %d after %d read(s): the tool stopped at an orphaned run", got.code, b.polls.Load())
	}
}

// --- Following -----------------------------------------------------------------------------------------------------

func TestTheOutputOfTheRunIsPrintedBeforeTheTableThatEndsIt(t *testing.T) {
	b := &ballast{
		poll: pollsIn(state("passed", "met", held)),
		logs: func(w http.ResponseWriter, _ *http.Request) {
			startStream(w)
			sendLine(w, 0, "starting the load")
			sendLine(w, 1, "  ✓ status is 200")
			// The run is already over when the last line is read, and the end event
			// arrives a moment later: nothing may be printed after the table.
			time.Sleep(80 * time.Millisecond)
			sendLine(w, 2, "the very last line")
			sendEnd(w, "passed")
		},
	}

	got := wholeRun(t, b, nil)

	if got.code != ExitMet {
		t.Fatalf("exit %d", got.code)
	}
	last := strings.Index(got.out, "the very last line")
	table := strings.Index(got.out, "http_req_duration")
	if last < 0 || table < 0 || last > table {
		t.Errorf("the last line (%d) must come before the table (%d):\n%s", last, table, got.out)
	}
	if !strings.Contains(got.out, "starting the load\n") || !strings.Contains(got.out, "  ✓ status is 200\n") {
		t.Errorf("the lines were not printed as they are:\n%s", got.out)
	}
}

// A stream that never ends must not hold the verdict hostage.
func TestAStreamThatNeverEndsDoesNotKeepTheVerdictFromBeingGiven(t *testing.T) {
	b := &ballast{
		poll: pollsIn(state("passed", "met", held)),
		logs: func(w http.ResponseWriter, r *http.Request) {
			startStream(w)
			sendLine(w, 0, "line")
			<-r.Context().Done()
		},
	}

	start := time.Now()
	got := wholeRun(t, b, nil)

	if got.code != ExitMet {
		t.Errorf("exit %d", got.code)
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("the verdict was given after %s", took)
	}
}

// The stream is a convenience. When it fails the run goes on, and the verdict is
// still read from the run itself.
func TestAStreamThatFailsNeverFailsTheRun(t *testing.T) {
	for name, logs := range map[string]func(http.ResponseWriter, *http.Request){
		"a server error": func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 500, `{"error":"oops"}`) },
		"a refusal":      func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 404, `{"error":"no such run"}`) },
		"a stop": func(w http.ResponseWriter, _ *http.Request) {
			startStream(w)
			_, _ = fmt.Fprint(w, "event: error\ndata: {\"error\":\"the log stream stopped\"}\n\n")
			w.(http.Flusher).Flush()
		},
	} {
		t.Run(name, func(t *testing.T) {
			b := &ballast{
				poll: pollsIn(state("running", "none"), state("running", "none"), state("passed", "met", held)),
				logs: logs,
			}

			got := wholeRun(t, b, nil)

			if got.code != ExitMet {
				t.Fatalf("exit %d, want 0: a stream that failed failed the run\nstderr: %s", got.code, got.err)
			}
			// Said once, so that the log does not fill with it: one line of the tool's
			// own words, whatever the reason carries.
			n := 0
			for _, line := range strings.Split(got.err, "\n") {
				if strings.Contains(line, "no longer followed") {
					n++
				}
			}
			if n != 1 {
				t.Errorf("the failure of the stream was reported on %d lines, want one:\n%s", n, got.err)
			}
		})
	}
}

// --- Losing contact --------------------------------------------------------------------------------------------------------

func TestAPassingFailureToReadTheRunIsWaitedOut(t *testing.T) {
	b := &ballast{poll: func(n int, w http.ResponseWriter) {
		switch {
		case n <= 2:
			writeJSON(w, 503, `{"error":"authentication is unavailable, try again"}`)
		default:
			writeJSON(w, 200, state("passed", "met", held))
		}
	}}

	got := wholeRun(t, b, nil)

	if got.code != ExitMet {
		t.Errorf("exit %d, want 0\nstderr: %s", got.code, got.err)
	}
	if b.deletes.Load() != 0 {
		t.Error("a run was cancelled for a failure that passed")
	}
}

// A tool that cannot reach Ballast any more leaves the run behind: nobody will
// read its verdict, so it asks for it to be stopped, once, and says so.
func TestAFailureThatDoesNotPassLeavesWithTwoAndAsksForTheRunToBeStopped(t *testing.T) {
	b := &ballast{poll: func(n int, w http.ResponseWriter) {
		if n == 1 {
			writeJSON(w, 200, state("running", "none"))
			return
		}
		writeJSON(w, 502, `<html>bad gateway</html>`)
	}}

	got := wholeRun(t, b, nil)

	if got.code != ExitError {
		t.Errorf("exit %d, want 2", got.code)
	}
	if b.deletes.Load() != 1 {
		t.Errorf("the run was asked to stop %d times, want once", b.deletes.Load())
	}
	// One read that worked, and then exactly as many failures as are tolerated.
	if n := int(b.polls.Load()); n != 1+maxPollFailures {
		t.Errorf("the run was read %d times, want %d: the number of tries is bounded", n, 1+maxPollFailures)
	}
	if !strings.Contains(got.err, thePageURL) && !strings.Contains(got.err, theRunID) {
		t.Errorf("nothing says which run was left:\n%s", got.err)
	}
}

// What is said by a 401 or a 404 does not change by asking again, and a token that
// is refused cannot ask for anything to be stopped either.
func TestAnAnswerThatWillNotChangeEndsTheWaitAtOnce(t *testing.T) {
	for _, status := range []int{401, 404} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			b := &ballast{poll: func(n int, w http.ResponseWriter) {
				if n == 1 {
					writeJSON(w, 200, state("running", "none"))
					return
				}
				writeJSON(w, status, `{"error":"unauthenticated"}`)
			}}

			got := wholeRun(t, b, nil)

			if got.code != ExitError {
				t.Errorf("exit %d, want 2", got.code)
			}
			if n := b.polls.Load(); n != 2 {
				t.Errorf("the run was read %d times, want 2: the answer would not have changed", n)
			}
			if b.deletes.Load() != 0 {
				t.Errorf("a cancellation was attempted with a token that was just refused")
			}
		})
	}
}

// --- What is printed -------------------------------------------------------------------------------------------------------------

func TestTheTokenIsNeverInAnythingPrintedWhateverTheServerSays(t *testing.T) {
	echo := " " + aToken + " and Authorization: Bearer " + aToken
	cases := map[string]*ballast{
		"in the output of the run": {
			poll: pollsIn(state("passed", "met", held)),
			logs: func(w http.ResponseWriter, _ *http.Request) {
				startStream(w)
				sendLine(w, 0, "the token is"+echo)
				sendEnd(w, "passed")
			},
		},
		"in a threshold": {poll: pollsIn(state("failed", "breached",
			threshold("metric"+echo, "expr"+echo, false)))},
		"in the reason a run errored": {poll: pollsIn(stateWith("errored", "none", "failed with"+echo))},
		"in a refusal": {launch: func(w http.ResponseWriter) {
			writeJSON(w, 403, `{"error":"refused`+strings.ReplaceAll(echo, `"`, ``)+`"}`)
		}},
		"in an error on the way": {poll: func(n int, w http.ResponseWriter) {
			if n == 1 {
				writeJSON(w, 200, state("running", "none"))
				return
			}
			writeJSON(w, 500, `{"error":"broke`+strings.ReplaceAll(echo, `"`, ``)+`"}`)
		}},
		"in the stop of the stream": {
			poll: pollsIn(state("running", "none"), state("passed", "met", held)),
			logs: func(w http.ResponseWriter, _ *http.Request) {
				startStream(w)
				_, _ = fmt.Fprintf(w, "event: error\ndata: {\"error\":\"stopped %s\"}\n\n", aToken)
				w.(http.Flusher).Flush()
			},
		},
	}

	for name, b := range cases {
		t.Run(name, func(t *testing.T) {
			got := wholeRun(t, b, nil)

			if strings.Contains(got.all(), aToken) || strings.Contains(got.all(), aToken[4:]) {
				t.Errorf("the token was printed:\nstdout: %s\nstderr: %s", got.out, got.err)
			}
		})
	}
}

// What is printed is cleaned of what would rewrite a terminal or forge a line of
// the log, from the reason a run failed to the name of a metric.
func TestWhatTheServerSaysAboutARunIsMadeSafeToPrint(t *testing.T) {
	hostile := threshold("m\x1b[2Jetric\r\nFORGED: line", "p(95)<500\u202e", false)

	got := wholeRun(t, &ballast{poll: pollsIn(state("failed", "breached", hostile))}, nil)

	for _, forbidden := range []string{"\x1b", "\r", "\u202e"} {
		if strings.Contains(got.all(), forbidden) {
			t.Errorf("%q reached the output", forbidden)
		}
	}
	if strings.Contains(got.out, "\nFORGED:") {
		t.Errorf("a line was forged in the output:\n%s", got.out)
	}
}

// The output is the run's, and the tool's own words go elsewhere: a pipeline that
// reads the output of the run must not find the tool in it.
func TestTheRunsOutputIsOnStdoutAndTheToolsOwnWordsAreNot(t *testing.T) {
	b := &ballast{
		poll: pollsIn(state("running", "none"), state("passed", "met", held)),
		logs: func(w http.ResponseWriter, _ *http.Request) {
			startStream(w)
			sendLine(w, 0, "a line of the run")
			sendEnd(w, "passed")
		},
	}

	got := wholeRun(t, b, nil)

	if !strings.Contains(got.out, "a line of the run") {
		t.Errorf("the output of the run is not on stdout:\n%s", got.out)
	}
	if strings.Contains(got.out, "ballast:") {
		t.Errorf("the tool's own words are on stdout:\n%s", got.out)
	}
	if !strings.Contains(got.err, "ballast:") {
		t.Errorf("the tool's own words are not marked as its own:\n%s", got.err)
	}
}

// What is written is redacted once more on its way out, whatever produced it. The
// pieces that make the text are cleaned where they are read, and this is the one
// place that stands behind all of them.
func TestTheWriterTakesTheTokenOutOfWhateverItIsHanded(t *testing.T) {
	client, err := NewClient("https://ballast.example.test", aToken)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	var sink bytes.Buffer
	writer := &safeWriter{w: &sink, redact: client.Redact}

	// Several lines in one write, as a block of text is: the header pattern runs to
	// the end of its own line, so each line is cleaned alone.
	// And a credential that is not the token, which no value is known for and only
	// the shape of its line gives away.
	n, err := fmt.Fprintf(writer, "first %s\nAuthorization: Bearer %s\nAuthorization: Basic dXNlcjpwYXNz\nlast line, kept\n", aToken, aToken)

	if err != nil || n == 0 {
		t.Fatalf("write: %d, %v", n, err)
	}
	got := sink.String()
	if strings.Contains(got, aToken) || strings.Contains(got, aToken[4:]) {
		t.Errorf("the token went through: %q", got)
	}
	if strings.Contains(got, "dXNlcjpwYXNz") {
		t.Errorf("a credential that is not the token went through: %q", got)
	}
	if !strings.Contains(got, "last line, kept") || strings.Count(got, "\n") != 4 {
		t.Errorf("what was not secret was changed: %q", got)
	}
}
