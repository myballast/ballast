package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// usage is what "ballast help" prints. It says what a pipeline needs to know at the
// moment it is broken: where each setting comes from, what every code means, and
// what is left to the defaults.
var usage = fmt.Sprintf(`ballast launches a load test run on Ballast, follows its output, waits for it to
end, and leaves with a code a pipeline can act on.

Usage:
  ballast run --scenario ID --environment ID --profile NAME [options]
  ballast version

Settings, read from the environment:
  BALLAST_URL    the address of your Ballast, for example https://ballast.example.com
  BALLAST_TOKEN  an API token of the project, made in its "CI tokens" page. It is read
                 from here and from nowhere else: an argument would show in a process
                 list and in the log of the pipeline that typed it.

Options:
  --scenario ID         the scenario to run (its identifier, a UUID)
  --environment ID      the environment to run against (its identifier, a UUID)
  --profile NAME        the load profile: %s
  --timeout DURATION    how long to give the run, from the launch. When it is up the
                        run is asked to stop and the tool leaves with 2. Default %s.
  --no-fail-on-breach   leave with 0 when a threshold was breached. The breach is
                        still shown; nothing else is excused.
  --allow-no-threshold  accept a run that judged no threshold. Without it such a run
                        is not a success: it measured nothing against a rule.

There is no --project. A token belongs to one project, and a run belongs to the
project of its scenario: --scenario is all that names it.

Exit codes:
  0  the run finished and its thresholds held
  1  the run finished and a threshold was breached
  2  anything else: an error, a refusal, a run that did not finish, a cancellation,
     a result that says nothing about the thresholds

Ctrl+C or SIGTERM asks the run to stop before the tool leaves, and leaves with 2. A
second signal leaves at once, without waiting for the answer; a third leaves now.
`,
	strings.Join([]string{
		string(domain.ProfileSmoke), string(domain.ProfileLoad), string(domain.ProfileStress),
		string(domain.ProfileSpike), string(domain.ProfileSoak),
	}, ", "),
	"2h")

// Main is the command: it reads its arguments and its environment, runs, and
// returns the code to leave with.
//
// ctx is cancelled on the first signal and abort is closed on the second. Nothing
// here reads the process: the environment is a function, the destinations are
// writers, so that all of it is tested without a process.
func Main(
	ctx context.Context, abort <-chan struct{}, args []string,
	env func(string) string, stdout, stderr io.Writer,
) int {
	// What is printed is redacted by the token's own value, from the first byte: a
	// flag that does not parse echoes what it was given, and what it was given may
	// have been the token, typed in the wrong place.
	redact := redactorFor(env("BALLAST_TOKEN"))
	out := &safeWriter{w: stdout, redact: redact}
	errs := &safeWriter{w: stderr, redact: redact}
	say := func(format string, args ...any) {
		_, _ = fmt.Fprintf(errs, "ballast: "+format+"\n", args...)
	}

	if len(args) == 0 {
		say("no command. See \"ballast help\"")
		return ExitError
	}

	switch args[0] {
	case "help", "-h", "--help":
		_, _ = fmt.Fprint(out, usage)
		return 0
	case "version", "--version":
		_, _ = fmt.Fprintln(out, versionLine())
		return 0
	case "run":
		return runCommand(ctx, abort, args[1:], env, out, errs, say)
	}

	// The word is not repeated: it may be something that was typed by mistake.
	say("that is not a command. See \"ballast help\"")
	return ExitError
}

func runCommand(
	ctx context.Context, abort <-chan struct{}, args []string, env func(string) string,
	out, errs io.Writer, say func(string, ...any),
) int {
	// Said before the flags are parsed, so that what is typed in the wrong place is
	// answered with where it goes and not with "flag provided but not defined".
	for _, arg := range args {
		switch flagName(arg) {
		case "project":
			say("there is no --project: a token belongs to one project, and a run belongs to the project of its scenario, so --scenario is all that names it")
			return ExitError
		case "token", "api-token", "apitoken", "password", "secret":
			say("the token is read from BALLAST_TOKEN and never from an argument: an argument shows in a process list and in the log of the pipeline that ran it")
			return ExitError
		}
	}

	var (
		options RunOptions
		timeout time.Duration
	)
	flags := flag.NewFlagSet("ballast run", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.Usage = func() {}
	flags.StringVar(&options.ScenarioID, "scenario", "", "")
	flags.StringVar(&options.EnvironmentID, "environment", "", "")
	flags.StringVar(&options.Profile, "profile", "", "")
	flags.DurationVar(&timeout, "timeout", 0, "")
	flags.BoolVar(&options.NoFailOnBreach, "no-fail-on-breach", false, "")
	flags.BoolVar(&options.AllowNoThreshold, "allow-no-threshold", false, "")

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, _ = fmt.Fprint(out, usage)
			return 0
		}
		say("%v. See \"ballast help\"", err)
		return ExitError
	}
	if flags.NArg() > 0 {
		say("unexpected argument: everything is a flag. See \"ballast help\"")
		return ExitError
	}
	options.Timeout = timeout

	client, err := NewClient(env("BALLAST_URL"), env("BALLAST_TOKEN"))
	if err != nil {
		say("%v", err)
		return ExitError
	}

	runner := &Runner{Client: client, Out: out, Err: errs, Env: env, Abort: abort}
	return runner.Run(ctx, options)
}

// flagName is the name a command-line word would be read as a flag under: the word
// without its dashes and without what follows an equals sign. A word that is not a
// flag has none.
func flagName(arg string) string {
	if !strings.HasPrefix(arg, "-") {
		return ""
	}
	name := strings.TrimLeft(arg, "-")
	name, _, _ = strings.Cut(name, "=")
	return strings.ToLower(name)
}

// redactorFor returns what masks the token in a piece of text. With no token there
// is nothing to mask by value, and the patterns that give a credential away by its
// shape still apply.
func redactorFor(token string) func(string) string {
	redactor, _ := domain.NewRedactor(map[string]string{"BALLAST_TOKEN": strings.TrimSpace(token)})
	return redactor.Redact
}
