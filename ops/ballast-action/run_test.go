package ballastaction

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The step that runs the command passes the inputs of the workflow to it, and leaves with
// what it leaves with.
//
// What these tests hold in place:
//
//   - the inputs reach the command as arguments of their own, in the form --name=value:
//     a value is text from somebody, and neither a space nor a quote nor "$(…)" nor a
//     leading dash in it can make a second argument, a second command or a flag;
//   - the token is read from the environment and given to the command in the environment:
//     it is never an argument, which a process list and a log would show;
//   - the code of the command is the code of the step, and the command is the process of
//     the step, so that the signal a runner sends to stop a job reaches it;
//   - what is wrong with the inputs is said before anything is launched, and in the
//     words of the workflow, not of the command line.
//
// Written before the script, which is why they do not pass yet.

const aSecret = "blt_" + "AbCdEfGhIjKlMnOpQrStUvWxYz0123456789-_AbCdE"

// fakeBallast is a command that records how it was called and leaves with the code it is
// told to.
type fakeBallast struct {
	path string
	log  string
}

func newFakeBallast(t *testing.T, code int) fakeBallast {
	t.Helper()

	dir := t.TempDir()
	f := fakeBallast{path: filepath.Join(dir, "ballast"), log: filepath.Join(dir, "log")}
	script := `#!/bin/bash
{
  echo "pid=$$"
  echo "token=${BALLAST_TOKEN-unset}"
  echo "url=${BALLAST_URL-unset}"
} > "` + f.log + `"
printf '%s\0' "$@" > "` + f.log + `.args"
exit ` + itoa(code) + `
`
	if err := os.WriteFile(f.path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return f
}

func itoa(n int) string { return strconv.Itoa(n) }

// called reads what the command recorded: its arguments, in order, and the rest by name.
func (f fakeBallast) called(t *testing.T) (args []string, fields map[string]string, ran bool) {
	t.Helper()

	data, err := os.ReadFile(f.log)
	if err != nil {
		return nil, nil, false
	}
	fields = map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		name, value, _ := strings.Cut(line, "=")
		fields[name] = value
	}

	// The arguments are written with a NUL after each, so that one that holds a line
	// break stays one.
	raw, err := os.ReadFile(f.log + ".args")
	if err != nil {
		return nil, nil, false
	}
	if len(raw) > 0 {
		args = strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00")
	}
	return args, fields, true
}

func inputs(f fakeBallast, extra ...string) []string {
	return append([]string{
		"BALLAST_BIN=" + f.path,
		"BALLAST_TOKEN=" + aSecret,
		"BALLAST_URL=https://ballast.example.com",
		"BALLAST_ARG_SCENARIO=" + "8f14e45f-ceea-467a-9e2a-3f1f0f5b1f11",
		"BALLAST_ARG_ENVIRONMENT=" + "c9f0f895-fb98-4b91-99f4-1bf0c1f0d6a1",
		"BALLAST_ARG_PROFILE=smoke",
	}, extra...)
}

func TestTheInputsReachTheCommandAsArgumentsOfTheirOwn(t *testing.T) {
	f := newFakeBallast(t, 0)

	got := runScript(t, "run.sh", inputs(f))
	args, _, ran := f.called(t)

	if got.code != 0 || !ran {
		t.Fatalf("exit %d, ran %v:\n%s", got.code, ran, got.out)
	}
	want := []string{
		"run", "--scenario=8f14e45f-ceea-467a-9e2a-3f1f0f5b1f11",
		"--environment=c9f0f895-fb98-4b91-99f4-1bf0c1f0d6a1", "--profile=smoke",
	}
	if strings.Join(args, "\n") != strings.Join(want, "\n") {
		t.Errorf("arguments %q, want %q", args, want)
	}
}

func TestTheOptionsAreOnlyPassedWhenTheyAreAsked(t *testing.T) {
	for name, tc := range map[string]struct {
		env  []string
		want []string
	}{
		"a timeout":      {[]string{"BALLAST_ARG_TIMEOUT=45m"}, []string{"--timeout=45m"}},
		"no timeout":     {[]string{"BALLAST_ARG_TIMEOUT="}, nil},
		"allow, true":    {[]string{"BALLAST_ARG_ALLOW_NO_THRESHOLD=true"}, []string{"--allow-no-threshold"}},
		"allow, false":   {[]string{"BALLAST_ARG_ALLOW_NO_THRESHOLD=false"}, nil},
		"allow, empty":   {[]string{"BALLAST_ARG_ALLOW_NO_THRESHOLD="}, nil},
		"no fail, true":  {[]string{"BALLAST_ARG_NO_FAIL_ON_BREACH=true"}, []string{"--no-fail-on-breach"}},
		"no fail, false": {[]string{"BALLAST_ARG_NO_FAIL_ON_BREACH=false"}, nil},
		"both, and a timeout": {[]string{"BALLAST_ARG_TIMEOUT=1h", "BALLAST_ARG_ALLOW_NO_THRESHOLD=true", "BALLAST_ARG_NO_FAIL_ON_BREACH=true"},
			[]string{"--timeout=1h", "--allow-no-threshold", "--no-fail-on-breach"}},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeBallast(t, 0)

			got := runScript(t, "run.sh", inputs(f, tc.env...))
			args, _, _ := f.called(t)

			if got.code != 0 {
				t.Fatalf("exit %d:\n%s", got.code, got.out)
			}
			base := 4
			if len(args) != base+len(tc.want) {
				t.Fatalf("arguments %q, want the four of a run and %q", args, tc.want)
			}
			for i, w := range tc.want {
				if args[base+i] != w {
					t.Errorf("argument %d is %q, want %q", base+i, args[base+i], w)
				}
			}
		})
	}
}

func TestAnInputThatIsNotTrueOrFalseIsRefusedBeforeAnythingIsLaunched(t *testing.T) {
	for _, name := range []string{"BALLAST_ARG_ALLOW_NO_THRESHOLD", "BALLAST_ARG_NO_FAIL_ON_BREACH"} {
		for _, value := range []string{"yes", "1", "True", "TRUE", "true ", "--x", "$(touch PWNED)"} {
			f := newFakeBallast(t, 0)

			got := runScript(t, "run.sh", inputs(f, name+"="+value))

			if _, _, ran := f.called(t); got.code == 0 || ran {
				t.Errorf("%s=%q: exit %d, ran %v", name, value, got.code, ran)
			}
			input := strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(name, "BALLAST_ARG_"), "_", "-"))
			if !strings.Contains(got.out, input) {
				t.Errorf("%s=%q: the refusal does not name the input %s:\n%s", name, value, input, got.out)
			}
			if strings.Contains(got.out, "PWNED") {
				t.Errorf("the refusal repeats what it was given:\n%s", got.out)
			}
		}
	}
}

func TestWhatIsTypedInAnInputCanNeverBeAnythingButItsValue(t *testing.T) {
	for name, value := range map[string]string{
		"a space":        "a b",
		"a flag":         "--token=x",
		"a dash":         "-x",
		"a substitution": "$(touch PWNED)",
		"a backtick":     "`touch PWNED`",
		"a semicolon":    "x; touch PWNED",
		"a quote":        `x' --token=y '`,
		"a glob":         "*",
		"a newline":      "x\n--token=y",
		"empty":          "",
		"a variable":     "$BALLAST_TOKEN",
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeBallast(t, 0)

			got := runScript(t, "run.sh", inputs(f, "BALLAST_ARG_PROFILE="+value, "BALLAST_ARG_TIMEOUT="+value))
			args, _, ran := f.called(t)

			if !ran || got.code != 0 {
				t.Fatalf("exit %d, ran %v:\n%s", got.code, ran, got.out)
			}
			// Exactly one argument holds each, and it begins with its own flag.
			wantCount := 4
			if value != "" {
				wantCount = 5
			}
			if len(args) != wantCount {
				t.Fatalf("%d arguments %q, want %d: the value became more than one", len(args), args, wantCount)
			}
			if args[3] != "--profile="+value {
				t.Errorf("profile argument is %q", args[3])
			}
			if wantCount == 5 && args[4] != "--timeout="+value {
				t.Errorf("timeout argument is %q", args[4])
			}
			for _, a := range args {
				if strings.Contains(a, aSecret) {
					t.Errorf("the token ended up in an argument: %q", a)
				}
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(f.path), "PWNED")); err == nil {
				t.Error("something typed in an input was executed")
			}
		})
	}
}

func TestTheTokenAndTheAddressGoInTheEnvironmentAndNeverInTheArguments(t *testing.T) {
	f := newFakeBallast(t, 0)

	runScript(t, "run.sh", inputs(f))
	args, fields, _ := f.called(t)

	if fields["token"] != aSecret || fields["url"] != "https://ballast.example.com" {
		t.Errorf("the command got token %q and url %q", fields["token"], fields["url"])
	}
	for _, a := range args {
		if strings.Contains(a, aSecret) || strings.Contains(a, "ballast.example.com") {
			t.Errorf("an argument holds the token or the address: %q", a)
		}
	}
}

func TestTheCodeOfTheCommandIsTheCodeOfTheStep(t *testing.T) {
	for _, code := range []int{0, 1, 2} {
		f := newFakeBallast(t, code)

		if got := runScript(t, "run.sh", inputs(f)); got.code != code {
			t.Errorf("the command left with %d and the step with %d", code, got.code)
		}
	}
}

func TestTheCommandIsTheProcessOfTheStepSoThatItIsToldWhenTheJobIsStopped(t *testing.T) {
	dir := t.TempDir()
	started, got := filepath.Join(dir, "started"), filepath.Join(dir, "got")
	bin := filepath.Join(dir, "ballast")
	script := "#!/bin/bash\ntrap 'echo term > \"" + got + "\"; exit 2' TERM\necho $$ > \"" + started + "\"\nwhile true; do sleep 0.05; done\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", here(t, "run.sh"))
	cmd.Dir = dir
	cmd.Env = append(cleanEnvironment(), inputs(fakeBallast{path: bin})...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	deadline := time.Now().Add(10 * time.Second)
	for {
		if data, err := os.ReadFile(started); err == nil && strings.TrimSpace(string(data)) != "" {
			if strings.TrimSpace(string(data)) != strconv.Itoa(cmd.Process.Pid) {
				t.Fatalf("the command is process %s, the step is %d: a signal to the step would not reach it", strings.TrimSpace(string(data)), cmd.Process.Pid)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the command did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the command did not leave on SIGTERM")
	}
	if _, err := os.Stat(got); err != nil {
		t.Error("the command was not told about the signal")
	}
}

// --- What is wrong is said in the words of the workflow ---------------------------------

func TestWithoutATokenNothingIsLaunchedAndTheWorkflowIsToldWhyItMayBeEmpty(t *testing.T) {
	f := newFakeBallast(t, 0)

	got := runScript(t, "run.sh", inputs(f, "BALLAST_TOKEN="))

	if _, _, ran := f.called(t); got.code == 0 || ran {
		t.Errorf("exit %d, ran %v", got.code, ran)
	}
	for _, want := range []string{"token", "secret", "fork"} {
		if !strings.Contains(strings.ToLower(got.out), want) {
			t.Errorf("the message does not mention %q:\n%s", want, got.out)
		}
	}
}

func TestWithoutAnAddressNothingIsLaunched(t *testing.T) {
	f := newFakeBallast(t, 0)

	got := runScript(t, "run.sh", inputs(f, "BALLAST_URL="))

	if _, _, ran := f.called(t); got.code == 0 || ran || !strings.Contains(got.out, "url") {
		t.Errorf("exit %d, ran %v:\n%s", got.code, ran, got.out)
	}
}

func TestAMissingOrUnrunnableCommandIsRefused(t *testing.T) {
	for name, bin := range map[string]string{
		"not given":    "",
		"not there":    filepath.Join(t.TempDir(), "nope"),
		"a directory":  t.TempDir(),
		"not runnable": notRunnable(t),
	} {
		t.Run(name, func(t *testing.T) {
			env := inputs(fakeBallast{path: bin})
			if got := runScript(t, "run.sh", env); got.code == 0 {
				t.Errorf("exit 0:\n%s", got.out)
			}
		})
	}
}

func notRunnable(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ballast")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTheMessagesNeverCarryTheTokenOrWhatWasTyped(t *testing.T) {
	f := newFakeBallast(t, 0)

	got := runScript(t, "run.sh", inputs(f, "BALLAST_ARG_ALLOW_NO_THRESHOLD="+aSecret))

	if strings.Contains(got.out, aSecret) {
		t.Errorf("the output holds the token:\n%s", got.out)
	}
}
