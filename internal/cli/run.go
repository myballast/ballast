package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"
)

// The three ways the tool leaves, which are what a pipeline reads.
const (
	// ExitMet: the run finished and its thresholds held.
	ExitMet = 0
	// ExitBreached: the run finished and at least one threshold was breached.
	ExitBreached = 1
	// ExitError: anything else. A refusal, an error, a run that did not finish, a
	// result that contradicts itself or that this version cannot read. A run that was
	// not a success never leaves with 0.
	ExitError = 2
)

const (
	// maxPollFailures is how many times in a row the run may fail to be read before
	// the tool gives up on it.
	maxPollFailures = 5

	defaultPollInterval = 3 * time.Second
	defaultDrainWait    = 10 * time.Second

	// cancelWait bounds the request that stops a run the tool is leaving behind.
	cancelWait = 10 * time.Second

	// DefaultTimeout is how long the tool waits for a run when it is not told. A
	// run lasts an hour at most, and the queue in front of it takes some of the
	// time too: two hours is the longest anybody has a reason to wait.
	DefaultTimeout = 2 * time.Hour
)

// errTimedOut is the cause of a context that ended because the time given to the
// run was up, as opposed to a signal.
var errTimedOut = errors.New("the timeout was reached")

// profileShape is what a profile's name looks like. The server knows which exist;
// this only keeps what is sent from being something else.
var profileShape = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// RunOptions is what the command is asked to do.
type RunOptions struct {
	ScenarioID    string
	EnvironmentID string
	Profile       string
	// CI is what the pipeline says about itself, or nil.
	CI *CIMetadata

	// NoFailOnBreach leaves with 0 when a threshold was breached. The breach is
	// still shown; nothing else is excused.
	NoFailOnBreach bool
	// AllowNoThreshold accepts a run that judged nothing. Without it such a run is
	// not a success: it measured nothing against a rule.
	AllowNoThreshold bool

	// Timeout is how long the tool gives the run, from the moment it is launched.
	// When it is up the run is asked to stop and the tool leaves with 2. Zero means
	// DefaultTimeout; there is no way to wait for ever, because a pipeline that waits
	// for ever on a run is a pipeline nobody is watching.
	Timeout time.Duration
}

func (o RunOptions) timeout() time.Duration {
	if o.Timeout > 0 {
		return o.Timeout
	}
	return DefaultTimeout
}

// validate refuses what cannot be right before anything is sent. The messages name
// the flag and never repeat its value: a flag is typed by a person, and a person
// pastes the wrong thing.
func (o RunOptions) validate() (RunOptions, error) {
	scenario, err := uuid.Parse(strings.TrimSpace(o.ScenarioID))
	if err != nil {
		return o, errors.New("--scenario must be the identifier of a scenario, a UUID")
	}
	environment, err := uuid.Parse(strings.TrimSpace(o.EnvironmentID))
	if err != nil {
		return o, errors.New("--environment must be the identifier of an environment, a UUID")
	}
	if !profileShape.MatchString(o.Profile) {
		return o, errors.New("--profile must be the name of a load profile, for example smoke")
	}

	if o.Timeout < 0 {
		return o, errors.New("--timeout must be a duration greater than zero, for example 90m")
	}

	o.ScenarioID, o.EnvironmentID = scenario.String(), environment.String()
	return o, nil
}

// Runner launches a run and sees it through.
type Runner struct {
	Client *Client
	// Out receives the output of the run and the result. Err receives the tool's own
	// words, each line beginning "ballast:", so that a pipeline that reads the
	// output of the run does not find the tool in it.
	Out, Err io.Writer

	// PollInterval is how often the run is read. Zero means three seconds.
	PollInterval time.Duration
	// PollBackoff is the pause before the nth try after a failure to read the run.
	PollBackoff func(attempt int) time.Duration
	// DrainWait is how long, once the run is over, the end of its output is waited
	// for. A stream that never ends does not hold the verdict hostage.
	DrainWait time.Duration

	// Env reads the environment, for the CI metadata. Nil means there is none to
	// read, and nothing about the pipeline is sent.
	Env func(name string) string

	// StopWait is how long the request to stop a run is waited for. Zero means ten
	// seconds. Ballast not answering must not keep a stopped pipeline running.
	StopWait time.Duration

	// Abort fires on the second signal. The first one is the context passed to Run
	// being cancelled: the run is asked to stop, and the tool waits for the answer.
	// The second is believed: the tool leaves at once and says what it left. Nil
	// never fires.
	Abort <-chan struct{}
}

// Run launches the run, follows it, and returns the code to leave with.
func (r *Runner) Run(ctx context.Context, options RunOptions) int {
	out := &safeWriter{w: r.Out, redact: r.Client.Redact}
	errs := &safeWriter{w: r.Err, redact: r.Client.Redact}
	say := func(format string, args ...any) {
		_, _ = fmt.Fprintf(errs, "ballast: "+format+"\n", args...)
	}

	options, err := options.validate()
	if err != nil {
		say("%v", err)
		return ExitError
	}

	// What the pipeline says about itself is optional, and nothing that is wrong with
	// it may stop a run: a warning says what was dropped and why.
	if options.CI == nil && r.Env != nil {
		detected, warnings := DetectCI(r.Env)
		for _, warning := range warnings {
			say("warning: %s", warning)
		}
		options.CI = detected
	}

	// The time given to the run covers all of it, the launch as well as the wait.
	ctx, stop := context.WithTimeoutCause(ctx, options.timeout(), errTimedOut)
	defer stop()
	why := func() string {
		if errors.Is(context.Cause(ctx), errTimedOut) {
			return fmt.Sprintf("the timeout of %s was reached", options.timeout())
		}
		return "interrupted"
	}

	queued, err := r.Client.Launch(ctx, LaunchRequest{
		ScenarioID: options.ScenarioID, EnvironmentID: options.EnvironmentID,
		Profile: options.Profile, CI: options.CI,
	})
	if err != nil {
		if ctx.Err() != nil {
			// The request left and nothing says it was answered, and the run has no
			// name yet to ask about: all that can be said is that one may exist.
			say("%s while the run was being launched: the request may have reached Ballast and queued a run, so check the project's runs before launching again", why())
			return ExitError
		}
		say("%s", describeLaunchError(err))
		return ExitError
	}
	say("launched run %s", queued.ID)

	return r.wait(ctx, queued.ID, options, out, say, why)
}

// describeLaunchError says what a refused or failed launch was, in the server's
// own words wherever it has some.
func describeLaunchError(err error) string {
	// An outcome that is not known says so first, whatever it was made of.
	if errors.Is(err, ErrMayHaveQueued) {
		return err.Error()
	}

	var refusal *APIError
	if !errors.As(err, &refusal) {
		return err.Error()
	}
	switch refusal.Status {
	case 401:
		return fmt.Sprintf("Ballast refused the token (%s). Check BALLAST_TOKEN: it may have been revoked or have expired", refusal.Message)
	case 429:
		message := "Ballast is limiting runs: " + refusal.Message
		if refusal.RetryAfter > 0 {
			message += fmt.Sprintf(". Try again in %s", refusal.RetryAfter)
		}
		return message
	case 503:
		return "Ballast is not accepting runs right now: " + refusal.Message
	}
	return fmt.Sprintf("Ballast refused the run (HTTP %d): %s", refusal.Status, refusal.Message)
}

// wait reads the run until it is over, printing its output as it goes.
func (r *Runner) wait(
	ctx context.Context, id string, options RunOptions, out io.Writer,
	say func(string, ...any), why func() string,
) int {
	streamCtx, stopStream := context.WithCancel(ctx)
	defer stopStream()
	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		r.follow(streamCtx, id, out, say)
	}()

	var (
		lastStatus string
		failures   int
		linked     bool
	)
	for {
		state, err := r.Client.Run(ctx, id)
		if err != nil {
			if ctx.Err() != nil {
				return r.leave(id, why(), stopStream, streamDone, say)
			}

			var refusal *APIError
			if errors.As(err, &refusal) && !worthRetrying(refusal.Status) {
				// What a 401 or a 404 says does not change by asking again, and a
				// token that was just refused cannot ask for anything to be stopped.
				say("%s", describeReadError(refusal, id))
				return ExitError
			}

			failures++
			if failures >= maxPollFailures {
				say("lost contact with Ballast after %d tries (%s)", failures, r.Client.clean(err.Error()))
				r.stopWhatIsLeft(id, say)
				return ExitError
			}
			r.sleep(ctx, r.backoff(failures))
			continue
		}
		failures = 0

		if !linked && state.URL != "" {
			say("%s", state.URL)
			linked = true
		}
		if state.Status != lastStatus {
			say("status: %s", state.Status)
			lastStatus = state.Status
		}

		if state.Terminal() {
			// The last lines of a run are written before it is recorded as over, and
			// the end of the stream arrives a moment after the verdict: wait for it,
			// for a while, so that nothing is printed after the table.
			// A signal that arrives now finds a run with nothing left to stop: its
			// result is what was asked for, and the wait is cut short.
			select {
			case <-streamDone:
			case <-time.After(r.drainWait()):
			case <-ctx.Done():
			}
			stopStream()
			<-streamDone

			return r.finish(state, options, out, say)
		}

		if !r.sleep(ctx, r.pollInterval()) {
			return r.leave(id, why(), stopStream, streamDone, say)
		}
	}
}

// leave is the tool being stopped with a run in its hands: by the time it was
// given, or by a signal. Nothing more is read or printed from the run, and the run
// is asked to stop. It is only asked: a run under way is stopped by the worker
// running it, a second or two later, and saying more than that would claim a
// target is free that may still be loaded.
func (r *Runner) leave(id, why string, stopStream context.CancelFunc, streamDone <-chan struct{}, say func(string, ...any)) int {
	stopStream()
	<-streamDone

	say("%s: run %s was not finished", why, id)
	r.stopWhatIsLeft(id, say)
	return ExitError
}

// follow prints the output of the run. It never fails the run: when the stream
// cannot be kept, one line says so and the verdict is read from the run itself.
func (r *Runner) follow(ctx context.Context, id string, out io.Writer, say func(string, ...any)) {
	_, err := r.Client.Follow(ctx, id, func(line LogLine) {
		_, _ = fmt.Fprintln(out, line.Message)
	})
	if err != nil && ctx.Err() == nil {
		say("the output of the run is no longer followed (%s); the run goes on and its verdict will still be read", r.Client.clean(err.Error()))
	}
}

func describeReadError(refusal *APIError, id string) string {
	switch refusal.Status {
	case 401:
		return fmt.Sprintf("the token is no longer accepted (%s): it may have been revoked or have expired. Run %s is still under way and was not stopped", refusal.Message, id)
	case 404:
		return fmt.Sprintf("run %s is no longer visible to this token (%s)", id, refusal.Message)
	}
	return fmt.Sprintf("run %s could not be read (HTTP %d): %s", id, refusal.Status, refusal.Message)
}

// stopWhatIsLeft asks for the run to be stopped when the tool can no longer see it,
// or is leaving it. Nobody is going to read its verdict, and load left running on a
// target that nobody is watching costs somebody something.
//
// The request is its own, with its own time limit, and not the context of the run
// that is being given up. A second signal cuts it short.
func (r *Runner) stopWhatIsLeft(id string, say func(string, ...any)) {
	wait := r.StopWait
	if wait <= 0 {
		wait = cancelWait
	}
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()

	var aborted atomic.Bool
	if r.Abort != nil {
		go func() {
			select {
			case <-r.Abort:
				aborted.Store(true)
				cancel()
			case <-ctx.Done():
			}
		}()
	}

	result, err := r.Client.Cancel(ctx, id)

	var refusal *APIError
	switch {
	case err == nil && result.Outcome == "cancelled":
		say("run %s is cancelled", id)
	case err == nil:
		say("asked Ballast to stop run %s: a run under way is stopped by the worker running it, within a few seconds, and its page shows when it has", id)
	case aborted.Load():
		say("second signal: leaving without waiting for the answer. Run %s may still be running", id)
	case errors.As(err, &refusal) && refusal.Status == 409:
		say("run %s had already finished when it was asked to stop", id)
	case errors.As(err, &refusal):
		say("could not stop run %s (HTTP %d: %s). It may still be running", id, refusal.Status, refusal.Message)
	default:
		say("could not ask Ballast to stop run %s (%s). It may still be running", id, r.Client.clean(err.Error()))
	}
}

// finish turns a run that is over into what is printed and the code to leave with.
//
// The code is read from the server's verdict and checked against its status. They
// agree or nobody is told the run passed: a pipeline that went green on a result
// that contradicts itself would be trusting nothing.
func (r *Runner) finish(state RunState, options RunOptions, out io.Writer, say func(string, ...any)) int {
	switch state.Status {
	case "passed", "failed":
	default:
		reason := ""
		if state.ErrorMessage != "" {
			reason = ": " + state.ErrorMessage
		}
		say("the run %s%s", state.Status, reason)
		if state.URL != "" {
			say("%s", state.URL)
		}
		return ExitError
	}

	switch {
	case state.Status == "passed" && state.Verdict == "met":
		printResult(out, state)
		return ExitMet

	case state.Status == "passed" && state.Verdict == "none":
		printResult(out, state)
		if options.AllowNoThreshold {
			say("warning: no threshold was judged, and --allow-no-threshold accepts the run as it is")
			return ExitMet
		}
		say("no threshold was judged, so this run cannot say whether performance is acceptable. Add a threshold to the scenario, or pass --allow-no-threshold to accept a run that has none")
		return ExitError

	case state.Status == "failed" && state.Verdict == "breached":
		printResult(out, state)
		if options.NoFailOnBreach {
			say("--no-fail-on-breach: a threshold was breached, and the exit code is 0 as asked")
			return ExitMet
		}
		return ExitBreached
	}

	say("the answer about the run is inconsistent (status %q, verdict %q): refusing to call it a success", state.Status, state.Verdict)
	return ExitError
}

// printResult writes the thresholds as a table and the link to the run.
//
// What held and what did not is said in a word and not by position or colour:
// a log has neither.
func printResult(out io.Writer, state RunState) {
	if len(state.Thresholds) > 0 {
		_, _ = fmt.Fprintln(out, "Thresholds:")
		table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		for _, t := range state.Thresholds {
			verdict := "met"
			if !t.Passed {
				verdict = "BREACHED"
			}
			_, _ = fmt.Fprintf(table, "  %s\t%s\t%s\n", verdict, t.Metric, t.Expression)
		}
		_ = table.Flush()
	} else {
		_, _ = fmt.Fprintln(out, "Thresholds: none")
	}
	_, _ = fmt.Fprintf(out, "Verdict: %s\n", state.Verdict)
	if state.URL != "" {
		_, _ = fmt.Fprintf(out, "Run: %s\n", state.URL)
	}
}

func (r *Runner) pollInterval() time.Duration {
	if r.PollInterval > 0 {
		return r.PollInterval
	}
	return defaultPollInterval
}

func (r *Runner) drainWait() time.Duration {
	if r.DrainWait > 0 {
		return r.DrainWait
	}
	return defaultDrainWait
}

func (r *Runner) backoff(attempt int) time.Duration {
	if r.PollBackoff != nil {
		return r.PollBackoff(attempt)
	}
	return defaultStreamBackoff(attempt)
}

// sleep waits, and says whether it waited the whole time.
func (r *Runner) sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}

// safeWriter serialises writes and takes the token out of every line. Two
// goroutines print — the output of the run, and the tool's own words — and one
// credential in either is one too many, so what is written is redacted again here,
// line by line, whatever produced it.
type safeWriter struct {
	mu     sync.Mutex
	w      io.Writer
	redact func(string) string
}

func (s *safeWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// One line at a time: the pattern that blanks a header runs to the end of its
	// line, and it only finds the end of a line it is given alone.
	lines := strings.Split(string(p), "\n")
	for i := range lines {
		lines[i] = s.redact(lines[i])
	}
	_, err := io.WriteString(s.w, strings.Join(lines, "\n"))
	return len(p), err
}
