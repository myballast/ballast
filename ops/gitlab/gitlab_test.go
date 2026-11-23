// Package ballastgitlab holds the tests of the GitLab CI template that sits next to them.
//
// The template is one file, ballast.gitlab-ci.yml, generated from install.sh, run.sh and
// VERSION by generate.sh. It carries both scripts inside it, so a project includes one
// file and trusts one file. What these tests hold in place:
//
//   - the file is what the generator writes from the scripts: nobody edits it by hand;
//   - the scripts do what the Action's do, for the same reasons: the download is checked
//     against the sums of its release (read strictly, since a checksum tool answers
//     "success" to an empty list), the command is only used when it says the version that
//     was asked for, what is typed in a variable is a value and never a second argument,
//     the token is never an argument, and the command is the process of the job so that a
//     cancelled job reaches it;
//   - the job, as GitLab reads it, installs and runs through those very scripts.
package ballastgitlab

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	aVersion = "v1.2.3"
	aSecret  = "blt_" + "AbCdEfGhIjKlMnOpQrStUvWxYz0123456789-_AbCdE"
)

// --- Helpers ------------------------------------------------------------------------------------------

type result struct {
	out  string
	code int
}

func here(t *testing.T, name string) string {
	t.Helper()
	path, err := filepath.Abs(name)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func read(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(here(t, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// cleanEnvironment is this process's environment without anything the scripts read.
func cleanEnvironment() []string {
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "BALLAST_") || strings.HasPrefix(name, "CI_") || name == "TMPDIR" {
			continue
		}
		env = append(env, kv)
	}
	return env
}

func runBash(t *testing.T, dir string, env []string, args ...string) result {
	t.Helper()

	cmd := exec.Command("bash", args...)
	cmd.Dir = dir
	cmd.Env = append(cleanEnvironment(), env...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out

	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return result{out: out.String()}
	case errors.As(err, &exit):
		return result{out: out.String(), code: exit.ExitCode()}
	}
	t.Fatalf("bash: %v", err)
	return result{}
}

// release is a fake page of releases: what it serves, and what it was asked.
type release struct {
	mu       sync.Mutex
	requests []string
	files    map[string][]byte
}

func (r *release) handler(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	r.requests = append(r.requests, req.URL.Path)
	body, ok := r.files[req.URL.Path]
	r.mu.Unlock()
	if !ok {
		http.NotFound(w, req)
		return
	}
	_, _ = w.Write(body)
}

func (r *release) asked() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.requests...)
}

// fakeCommand is a "binary" that is a shell script: it says the version it was given,
// and otherwise records how it was called and leaves with the code in FAKE_EXIT.
func fakeCommand(version string) []byte {
	return []byte(`#!/bin/sh
if [ "$1" = version ]; then echo 'ballast ` + version + ` (test/test, go0)'; exit 0; fi
if [ -n "${FAKE_STARTED:-}" ]; then
  trap 'echo term > "$FAKE_GOT"; exit 2' TERM
  echo $$ > "$FAKE_STARTED"
  while :; do sleep 0.05; done
fi
if [ -n "${FAKE_LOG:-}" ]; then
  { echo "token=${BALLAST_TOKEN-unset}"; echo "url=${BALLAST_URL-unset}"; } > "$FAKE_LOG"
  : > "$FAKE_LOG.args"
  for a in "$@"; do printf '%s\0' "$a" >> "$FAKE_LOG.args"; done
fi
exit "${FAKE_EXIT:-0}"
`)
}

func sumOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

var platforms = []struct{ s, m, file string }{
	{"Linux", "x86_64", "ballast_linux_amd64"},
	{"Linux", "aarch64", "ballast_linux_arm64"},
	{"Linux", "arm64", "ballast_linux_arm64"},
	{"Darwin", "x86_64", "ballast_darwin_amd64"},
	{"Darwin", "arm64", "ballast_darwin_arm64"},
}

func aRelease(version string) *release {
	r := &release{files: map[string][]byte{}}
	var sums strings.Builder
	for _, f := range []string{
		"ballast_linux_amd64", "ballast_linux_arm64", "ballast_darwin_amd64", "ballast_darwin_arm64",
		"ballast_windows_amd64.exe", "ballast_windows_arm64.exe",
	} {
		body := fakeCommand(version)
		r.files["/"+version+"/"+f] = body
		sums.WriteString(sumOf(body) + "  " + f + "\n")
	}
	r.files["/"+version+"/checksums.txt"] = []byte(sums.String())
	return r
}

// fakeUname puts a `uname` first in the PATH, so that the machine the tests run on can
// say it is any other.
func fakeUname(t *testing.T, s, m string) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\ncase \"$1\" in -s) echo '" + s + "';; -m) echo '" + m + "';; *) echo '" + s + "';; esac\n"
	if err := os.WriteFile(filepath.Join(dir, "uname"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return "PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH")
}

type installed struct {
	result
	asked []string
	bin   string
	temp  string
}

func install(t *testing.T, r *release, env ...string) installed {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(r.handler))
	t.Cleanup(server.Close)
	temp := t.TempDir()

	defaults := []string{
		fakeUname(t, "Linux", "x86_64"), "TMPDIR=" + temp,
		"BALLAST_DOWNLOAD_BASE=" + server.URL, "BALLAST_VERSION=" + aVersion,
	}
	got := runBash(t, t.TempDir(), append(defaults, env...), here(t, "install.sh"))
	out := installed{result: got, asked: r.asked(), temp: temp}
	// The path is the one line of stdout; the messages go to stderr, which the helper
	// merges, so it is the line that is a file.
	for _, line := range strings.Split(got.out, "\n") {
		if info, err := os.Stat(line); line != "" && err == nil && !info.IsDir() {
			out.bin = line
		}
	}
	return out
}

func files(t *testing.T, dir string) []string {
	t.Helper()
	var found []string
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			found = append(found, path)
		}
		return nil
	})
	return found
}

// --- The template is what the generator writes ------------------------------------------------------------

func TestTheTemplateIsWhatTheGeneratorWritesFromTheScripts(t *testing.T) {
	cmd := exec.Command("bash", here(t, "generate.sh"))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("generate.sh: %v", err)
	}
	if string(out) != read(t, "ballast.gitlab-ci.yml") {
		t.Error("ballast.gitlab-ci.yml is not what generate.sh writes: run `ops/gitlab/generate.sh > ops/gitlab/ballast.gitlab-ci.yml`")
	}
}

func TestTheVersionTheTemplateIsReleasedWithIsTheActionsAndIsAVersion(t *testing.T) {
	mine := strings.TrimSpace(read(t, "VERSION"))
	theirs := strings.TrimSpace(read(t, "../ballast-action/VERSION"))
	if mine != theirs {
		t.Errorf("the template is released with %s and the Action with %s: they ship together", mine, theirs)
	}
	if !regexp.MustCompile(`^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-[0-9A-Za-z.-]+)?$`).MatchString(mine) {
		t.Errorf("%q is not a version", mine)
	}
}

// --- The file, as GitLab reads it -----------------------------------------------------------------------

type pipeline map[string]job

type job struct {
	Extends   string            `yaml:"extends"`
	Stage     string            `yaml:"stage"`
	Image     string            `yaml:"image"`
	Variables map[string]string `yaml:"variables"`
	Script    []string          `yaml:"script"`
}

func theTemplate(t *testing.T) pipeline {
	t.Helper()
	var p pipeline
	if err := yaml.Unmarshal([]byte(read(t, "ballast.gitlab-ci.yml")), &p); err != nil {
		t.Fatalf("GitLab could not read the template: %v", err)
	}
	return p
}

func TestTheTemplateHasAHiddenJobToExtendAndAJobThatUsesIt(t *testing.T) {
	p := theTemplate(t)

	hidden, ok := p[".ballast-run"]
	if !ok || len(hidden.Script) != 1 {
		t.Fatalf("the hidden job .ballast-run is missing or its script is not one block: %+v", hidden)
	}
	if p["ballast-load-test"].Extends != ".ballast-run" || p["ballast-load-test"].Stage != "test" {
		t.Errorf("ballast-load-test is %+v", p["ballast-load-test"])
	}
	if len(p) != 2 {
		t.Errorf("the template defines %d things, want the two", len(p))
	}
}

func TestTheImageIsPinnedByDigestAndTheVersionByNumber(t *testing.T) {
	hidden := theTemplate(t)[".ballast-run"]

	if !regexp.MustCompile(`@sha256:[0-9a-f]{64}$`).MatchString(hidden.Image) {
		t.Errorf("the image %q is not pinned by digest", hidden.Image)
	}
	if hidden.Variables["BALLAST_VERSION"] != strings.TrimSpace(read(t, "VERSION")) {
		t.Errorf("the default version is %q, not the one of VERSION", hidden.Variables["BALLAST_VERSION"])
	}
	// What the project must give is never given a default here: a default would hide
	// that it is missing, and a variable of the job outranks nothing of the project's
	// but still reads as if it were set.
	for _, name := range []string{"BALLAST_TOKEN", "BALLAST_URL", "BALLAST_SCENARIO_ID", "BALLAST_ENVIRONMENT_ID", "BALLAST_PROFILE"} {
		if _, ok := hidden.Variables[name]; ok {
			t.Errorf("the template gives %s a value", name)
		}
	}
}

func TestNoSecretAndNoValueOfAPipelineIsWrittenInTheTemplate(t *testing.T) {
	text := read(t, "ballast.gitlab-ci.yml")

	if regexp.MustCompile(`blt_[A-Za-z0-9_-]{8,}`).MatchString(text) {
		t.Error("something that looks like a token is in the template")
	}
	// The script is a here-document with a quoted marker: nothing in it is expanded by
	// the job's shell before the script is written, and nothing a variable holds is
	// pasted into the text of a command.
	for _, marker := range []string{"BALLAST_INSTALL_SCRIPT", "BALLAST_RUN_SCRIPT"} {
		if !strings.Contains(text, "<<'"+marker+"'") {
			t.Errorf("the here-document of %s is not quoted", marker)
		}
	}
	// The only $ outside the scripts are those of the job's own lines.
	if strings.Contains(text, "${CI_") || strings.Contains(text, "$CI_") {
		t.Error("the template reads a predefined variable: nothing of the project should be in the script")
	}
}

// --- The job, run as GitLab would run it -----------------------------------------------------------

// theJobScript is the script of the hidden job, as the shell of the job receives it.
func theJobScript(t *testing.T) string {
	t.Helper()
	return theTemplate(t)[".ballast-run"].Script[0]
}

func runTheJob(t *testing.T, r *release, env ...string) (result, string) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(r.handler))
	t.Cleanup(server.Close)

	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	defaults := []string{
		fakeUname(t, "Linux", "x86_64"), "TMPDIR=" + dir,
		"BALLAST_DOWNLOAD_BASE=" + server.URL, "BALLAST_VERSION=" + aVersion,
		"BALLAST_TOKEN=" + aSecret, "BALLAST_URL=https://ballast.example.com",
		"BALLAST_SCENARIO_ID=8f14e45f-ceea-467a-9e2a-3f1f0f5b1f11",
		"BALLAST_ENVIRONMENT_ID=c9f0f895-fb98-4b91-99f4-1bf0c1f0d6a1",
		"BALLAST_PROFILE=smoke", "FAKE_LOG=" + log,
	}
	got := runBash(t, t.TempDir(), append(defaults, env...), "-c", theJobScript(t))
	return got, log
}

func argumentsOf(t *testing.T, log string) []string {
	t.Helper()
	raw, err := os.ReadFile(log + ".args")
	if err != nil {
		t.Fatalf("the command was not run: %v", err)
	}
	if len(raw) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00")
}

func TestTheJobInstallsTheCommandAndRunsItWithTheSettingsOfTheProject(t *testing.T) {
	got, log := runTheJob(t, aRelease(aVersion), "BALLAST_TIMEOUT=45m", "BALLAST_QUEUE_TIMEOUT=20m",
		"BALLAST_ALLOW_NO_THRESHOLD=true", "BALLAST_NO_FAIL_ON_BREACH=false")

	if got.code != 0 {
		t.Fatalf("exit %d:\n%s", got.code, got.out)
	}
	want := []string{
		"run", "--scenario=8f14e45f-ceea-467a-9e2a-3f1f0f5b1f11",
		"--environment=c9f0f895-fb98-4b91-99f4-1bf0c1f0d6a1", "--profile=smoke",
		"--timeout=45m", "--queue-timeout=20m", "--allow-no-threshold",
	}
	if strings.Join(argumentsOf(t, log), "\n") != strings.Join(want, "\n") {
		t.Errorf("arguments %q, want %q", argumentsOf(t, log), want)
	}
	data, _ := os.ReadFile(log)
	if !strings.Contains(string(data), "token="+aSecret) || !strings.Contains(string(data), "url=https://ballast.example.com") {
		t.Errorf("the token and the address did not reach the command in its environment:\n%s", data)
	}
}

func TestTheCodeOfTheCommandIsTheCodeOfTheJob(t *testing.T) {
	// 1 is a breached threshold and 2 is anything else: a job fails on both, and a job
	// that passes is a run whose thresholds held.
	for _, code := range []int{0, 1, 2} {
		got, _ := runTheJob(t, aRelease(aVersion), "FAKE_EXIT="+strconv.Itoa(code))
		if got.code != code {
			t.Errorf("the command left with %d and the job with %d", code, got.code)
		}
	}
}

func TestAJobWhoseDownloadDoesNotMatchItsSumNeverRunsTheCommand(t *testing.T) {
	r := aRelease(aVersion)
	r.files["/"+aVersion+"/ballast_linux_amd64"] = append(fakeCommand(aVersion), '\n')

	got, log := runTheJob(t, r)

	if got.code == 0 {
		t.Fatalf("the job passed:\n%s", got.out)
	}
	if _, err := os.Stat(log + ".args"); err == nil {
		t.Error("a command whose sum is wrong was run")
	}
}

func TestTheTokenIsNeverInTheLogOfAJobThatIsRefused(t *testing.T) {
	got, _ := runTheJob(t, aRelease(aVersion), "BALLAST_PROFILE=smoke", "BALLAST_URL=")
	if got.code == 0 || strings.Contains(got.out, aSecret) {
		t.Errorf("exit %d, and the log has the token: %v", got.code, strings.Contains(got.out, aSecret))
	}
}

// --- install.sh ------------------------------------------------------------------------------------------

func TestTheFileAskedForIsTheOneForTheRunnersSystemAndArchitecture(t *testing.T) {
	for _, p := range platforms {
		t.Run(p.s+"/"+p.m, func(t *testing.T) {
			got := install(t, aRelease(aVersion), fakeUname(t, p.s, p.m))

			if got.code != 0 {
				t.Fatalf("exit %d:\n%s", got.code, got.out)
			}
			want := []string{"/" + aVersion + "/" + p.file, "/" + aVersion + "/checksums.txt"}
			if strings.Join(got.asked, " ") != strings.Join(want, " ") {
				t.Errorf("asked for %v, want %v", got.asked, want)
			}
			if filepath.Base(got.bin) != "ballast" || !strings.HasPrefix(got.bin, got.temp) {
				t.Errorf("the command is at %q", got.bin)
			}
			if info, err := os.Stat(got.bin); err != nil || info.Mode()&0o100 == 0 {
				t.Errorf("the command is not there or not executable: %v", err)
			}
		})
	}
}

func TestASystemThatHasNoFileIsRefusedBeforeAnythingIsDownloaded(t *testing.T) {
	for name, p := range map[string][2]string{
		"windows":      {"MINGW64_NT-10.0", "x86_64"},
		"freebsd":      {"FreeBSD", "amd64"},
		"32 bits":      {"Linux", "i686"},
		"arm 32":       {"Linux", "armv7l"},
		"hostile os":   {"Linux/../../x", "x86_64"},
		"hostile arch": {"Linux", "x86_64 && touch PWNED"},
		"wrong case":   {"linux", "x86_64"},
	} {
		t.Run(name, func(t *testing.T) {
			got := install(t, aRelease(aVersion), fakeUname(t, p[0], p[1]))
			if got.code == 0 || len(got.asked) != 0 || got.bin != "" {
				t.Errorf("exit %d, asked %v, command %q: want a refusal before any download", got.code, got.asked, got.bin)
			}
		})
	}
}

func TestOnlyAVersionIsEverPutIntoAnAddress(t *testing.T) {
	for name, version := range map[string]string{
		"empty":          "",
		"latest":         "latest",
		"no v":           "1.2.3",
		"a path":         "v1.2.3/../../x",
		"a query":        "v1.2.3?x=1",
		"build data":     "v1.2.3+build",
		"a space":        "v1.2.3 ",
		"a newline":      "v1.2.3\nx",
		"a substitution": "v$(touch PWNED).2.3",
		"too long":       "v1.2.3-" + strings.Repeat("a", 60),
		"leading zeros":  "v01.2.3",
	} {
		t.Run(name, func(t *testing.T) {
			got := install(t, aRelease(aVersion), "BALLAST_VERSION="+version)
			if got.code == 0 || len(got.asked) != 0 {
				t.Errorf("exit %d, asked %v: want a refusal before any download", got.code, got.asked)
			}
		})
	}
	for _, version := range []string{"v1.2.3", "v1.2.3-rc.1", "v0.1.0-rc.1"} {
		if got := install(t, aRelease(version), "BALLAST_VERSION="+version); got.code != 0 {
			t.Errorf("%s was refused:\n%s", version, got.out)
		}
	}
}

func TestWhatWasDownloadedIsNotUsedUnlessItsSumIsTheOneOfTheRelease(t *testing.T) {
	file := "ballast_linux_amd64"
	good := fakeCommand(aVersion)
	other := fakeCommand("v0.0.1")
	sums := "/" + aVersion + "/checksums.txt"

	for name, mutate := range map[string]func(*release){
		"another sum":          func(r *release) { r.files[sums] = []byte(sumOf(other) + "  " + file + "\n") },
		"a changed byte":       func(r *release) { r.files["/"+aVersion+"/"+file] = append(append([]byte{}, good...), '\n') },
		"no line for the file": func(r *release) { r.files[sums] = []byte(sumOf(good) + "  ballast_linux_arm64\n") },
		"two lines":            func(r *release) { line := sumOf(good) + "  " + file + "\n"; r.files[sums] = []byte(line + line) },
		"two that disagree": func(r *release) {
			r.files[sums] = []byte(sumOf(good) + "  " + file + "\n" + sumOf(other) + "  " + file + "\n")
		},
		"an empty list": func(r *release) { r.files[sums] = nil },
		"prose":         func(r *release) { r.files[sums] = []byte("Everything is fine, trust me.\n") },
		"upper case":    func(r *release) { r.files[sums] = []byte(strings.ToUpper(sumOf(good)) + "  " + file + "\n") },
		"one space":     func(r *release) { r.files[sums] = []byte(sumOf(good) + " " + file + "\n") },
		"missing file":  func(r *release) { delete(r.files, "/"+aVersion+"/"+file) },
		"missing sums":  func(r *release) { delete(r.files, sums) },
	} {
		t.Run(name, func(t *testing.T) {
			r := aRelease(aVersion)
			mutate(r)

			got := install(t, r)

			if got.code == 0 || got.bin != "" {
				t.Fatalf("exit %d, command %q: a download that does not match was accepted\n%s", got.code, got.bin, got.out)
			}
			if left := files(t, got.temp); len(left) != 0 {
				t.Errorf("a refusal left %v behind", left)
			}
		})
	}
}

func TestACommandThatSaysAnotherVersionOrCannotRunIsNotUsed(t *testing.T) {
	for name, body := range map[string][]byte{
		"another version": fakeCommand("v9.9.9"),
		"not runnable":    []byte("not a program"),
	} {
		t.Run(name, func(t *testing.T) {
			r := &release{files: map[string][]byte{}}
			r.files["/"+aVersion+"/ballast_linux_amd64"] = body
			r.files["/"+aVersion+"/checksums.txt"] = []byte(sumOf(body) + "  ballast_linux_amd64\n")

			got := install(t, r)

			if got.code == 0 || got.bin != "" || len(files(t, got.temp)) != 0 {
				t.Errorf("exit %d, command %q, left %v", got.code, got.bin, files(t, got.temp))
			}
		})
	}
}

func TestSumsForOtherFilesDoNotMatter(t *testing.T) {
	r := aRelease(aVersion)
	r.files["/"+aVersion+"/checksums.txt"] = append([]byte("not a sum at all  ballast_other\n"), r.files["/"+aVersion+"/checksums.txt"]...)
	if got := install(t, r); got.code != 0 {
		t.Errorf("exit %d:\n%s", got.code, got.out)
	}
}

func TestTheDownloadIsFromTheReleasesOfBallastAndFromNowhereElse(t *testing.T) {
	text := read(t, "install.sh")
	if !strings.Contains(text, `default_base="https://github.com/myballast/ballast/releases/download"`) {
		t.Error("the address of the releases is not the one of Ballast")
	}
	for name, base := range map[string]string{
		"another host":     "https://example.com/releases",
		"plain http":       "http://example.com/releases",
		"credentials":      "http://127.0.0.1:1@example.com:80",
		"no port":          "http://127.0.0.1/x",
		"a suffix of host": "http://127.0.0.1:80.example.com",
	} {
		t.Run(name, func(t *testing.T) {
			r := aRelease(aVersion)
			got := install(t, r, "BALLAST_DOWNLOAD_BASE="+base)
			if got.code == 0 || len(got.asked) != 0 {
				t.Errorf("exit %d, asked %v: an address that is not this machine was accepted", got.code, got.asked)
			}
		})
	}
}

// --- run.sh ----------------------------------------------------------------------------------------------

func runScript(t *testing.T, env ...string) (result, string) {
	t.Helper()

	dir := t.TempDir()
	bin := filepath.Join(dir, "ballast")
	if err := os.WriteFile(bin, fakeCommand(aVersion), 0o700); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "log")
	defaults := []string{
		"BALLAST_BIN=" + bin, "BALLAST_TOKEN=" + aSecret, "BALLAST_URL=https://ballast.example.com",
		"BALLAST_SCENARIO_ID=8f14e45f-ceea-467a-9e2a-3f1f0f5b1f11",
		"BALLAST_ENVIRONMENT_ID=c9f0f895-fb98-4b91-99f4-1bf0c1f0d6a1",
		"BALLAST_PROFILE=smoke", "FAKE_LOG=" + log,
	}
	return runBash(t, dir, append(defaults, env...), here(t, "run.sh")), log
}

func TestOptionsArePassedOnlyWhenTheyAreAsked(t *testing.T) {
	for name, tc := range map[string]struct {
		env  []string
		want []string
	}{
		"none":                 {nil, nil},
		"a timeout":            {[]string{"BALLAST_TIMEOUT=45m"}, []string{"--timeout=45m"}},
		"an empty timeout":     {[]string{"BALLAST_TIMEOUT="}, nil},
		"a queue timeout":      {[]string{"BALLAST_QUEUE_TIMEOUT=20m"}, []string{"--queue-timeout=20m"}},
		"a queue timeout of 0": {[]string{"BALLAST_QUEUE_TIMEOUT=0"}, []string{"--queue-timeout=0"}},
		"allow, true":          {[]string{"BALLAST_ALLOW_NO_THRESHOLD=true"}, []string{"--allow-no-threshold"}},
		"allow, false":         {[]string{"BALLAST_ALLOW_NO_THRESHOLD=false"}, nil},
		"no fail, true":        {[]string{"BALLAST_NO_FAIL_ON_BREACH=true"}, []string{"--no-fail-on-breach"}},
		"no fail, false":       {[]string{"BALLAST_NO_FAIL_ON_BREACH=false"}, nil},
		"everything, in order": {[]string{"BALLAST_TIMEOUT=1h", "BALLAST_QUEUE_TIMEOUT=5m", "BALLAST_ALLOW_NO_THRESHOLD=true", "BALLAST_NO_FAIL_ON_BREACH=true"}, []string{"--timeout=1h", "--queue-timeout=5m", "--allow-no-threshold", "--no-fail-on-breach"}},
	} {
		t.Run(name, func(t *testing.T) {
			got, log := runScript(t, tc.env...)
			if got.code != 0 {
				t.Fatalf("exit %d:\n%s", got.code, got.out)
			}
			args := argumentsOf(t, log)
			want := append([]string{
				"run", "--scenario=8f14e45f-ceea-467a-9e2a-3f1f0f5b1f11",
				"--environment=c9f0f895-fb98-4b91-99f4-1bf0c1f0d6a1", "--profile=smoke",
			}, tc.want...)
			if strings.Join(args, "\n") != strings.Join(want, "\n") {
				t.Errorf("arguments %q, want %q", args, want)
			}
		})
	}
}

func TestAVariableThatIsNotTrueOrFalseIsRefusedBeforeAnythingIsLaunched(t *testing.T) {
	for _, name := range []string{"BALLAST_ALLOW_NO_THRESHOLD", "BALLAST_NO_FAIL_ON_BREACH"} {
		got, log := runScript(t, name+"=yes please")
		if got.code == 0 || !strings.Contains(got.out, name) || strings.Contains(got.out, "yes please") {
			t.Errorf("%s: exit %d, output %q: want a refusal that names the variable and not its value", name, got.code, got.out)
		}
		if _, err := os.Stat(log + ".args"); err == nil {
			t.Errorf("%s: the command was run", name)
		}
	}
}

func TestWhatIsTypedInAVariableCanNeverBeAnythingButItsValue(t *testing.T) {
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
		"a variable":     "$BALLAST_TOKEN",
	} {
		t.Run(name, func(t *testing.T) {
			got, log := runScript(t, "BALLAST_PROFILE="+value, "BALLAST_TIMEOUT="+value)
			if got.code != 0 {
				t.Fatalf("exit %d:\n%s", got.code, got.out)
			}
			args := argumentsOf(t, log)
			if len(args) != 5 || args[3] != "--profile="+value || args[4] != "--timeout="+value {
				t.Fatalf("arguments %q: the value became more than one, or something else", args)
			}
			for _, a := range args {
				if strings.Contains(a, aSecret) {
					t.Errorf("the token ended up in an argument: %q", a)
				}
			}
		})
	}
}

func TestWithoutATokenOrAnAddressNothingIsLaunchedAndTheJobIsToldWhy(t *testing.T) {
	got, log := runScript(t, "BALLAST_TOKEN=")
	if got.code == 0 || !strings.Contains(got.out, "BALLAST_TOKEN") || !strings.Contains(got.out, "protected") {
		t.Errorf("exit %d: %s", got.code, got.out)
	}
	if _, err := os.Stat(log + ".args"); err == nil {
		t.Error("the command was run without a token")
	}

	got, _ = runScript(t, "BALLAST_URL=")
	if got.code == 0 || !strings.Contains(got.out, "BALLAST_URL") {
		t.Errorf("exit %d: %s", got.code, got.out)
	}
}

func TestACommandThatIsMissingIsRefused(t *testing.T) {
	got, _ := runScript(t, "BALLAST_BIN=/does/not/exist")
	if got.code == 0 {
		t.Error("a missing command was run")
	}
}

func TestTheCommandIsTheProcessOfTheScriptSoThatItIsToldWhenTheJobIsCancelled(t *testing.T) {
	dir := t.TempDir()
	started, got := filepath.Join(dir, "started"), filepath.Join(dir, "got")
	bin := filepath.Join(dir, "ballast")
	script := "#!/bin/bash\ntrap 'echo term > \"" + got + "\"; exit 2' TERM\necho $$ > \"" + started + "\"\nwhile true; do sleep 0.05; done\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", here(t, "run.sh"))
	cmd.Dir = dir
	cmd.Env = append(cleanEnvironment(),
		"BALLAST_BIN="+bin, "BALLAST_TOKEN="+aSecret, "BALLAST_URL=https://ballast.example.com",
		"BALLAST_SCENARIO_ID=8f14e45f-ceea-467a-9e2a-3f1f0f5b1f11",
		"BALLAST_ENVIRONMENT_ID=c9f0f895-fb98-4b91-99f4-1bf0c1f0d6a1", "BALLAST_PROFILE=smoke")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	deadline := time.Now().Add(10 * time.Second)
	for {
		if data, err := os.ReadFile(started); err == nil && strings.TrimSpace(string(data)) != "" {
			if strings.TrimSpace(string(data)) != strconv.Itoa(cmd.Process.Pid) {
				t.Fatalf("the command is process %s, the script %d: a signal to the script would not reach it", strings.TrimSpace(string(data)), cmd.Process.Pid)
			}
			break
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
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

func TestTheCommandIsTheProcessOfTheJobSoThatItIsToldWhenTheJobIsCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(aRelease(aVersion).handler))
	t.Cleanup(server.Close)

	dir := t.TempDir()
	started, got := filepath.Join(dir, "started"), filepath.Join(dir, "got")
	// GitLab's own lines follow the script in the file the shell reads, so the last
	// command of the script is not the last of the shell: a bare `bash -c` would
	// quietly exec it, and hide a missing exec here.
	cmd := exec.Command("bash", "-c", theJobScript(t)+"\n:")
	cmd.Dir = dir
	cmd.Env = append(cleanEnvironment(),
		fakeUname(t, "Linux", "x86_64"), "TMPDIR="+dir,
		"BALLAST_DOWNLOAD_BASE="+server.URL, "BALLAST_VERSION="+aVersion,
		"BALLAST_TOKEN="+aSecret, "BALLAST_URL=https://ballast.example.com",
		"BALLAST_SCENARIO_ID=8f14e45f-ceea-467a-9e2a-3f1f0f5b1f11",
		"BALLAST_ENVIRONMENT_ID=c9f0f895-fb98-4b91-99f4-1bf0c1f0d6a1", "BALLAST_PROFILE=smoke",
		"FAKE_STARTED="+started, "FAKE_GOT="+got)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	deadline := time.Now().Add(20 * time.Second)
	for {
		if data, err := os.ReadFile(started); err == nil && strings.TrimSpace(string(data)) != "" {
			if strings.TrimSpace(string(data)) != strconv.Itoa(cmd.Process.Pid) {
				t.Fatalf("the command is process %s, the job %d: a signal to the job would not reach it", strings.TrimSpace(string(data)), cmd.Process.Pid)
			}
			break
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
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

// --- The page ---------------------------------------------------------------------------------------------

func TestTheCIPageNamesTheVariablesTheScriptReadsAndTheFileItIncludes(t *testing.T) {
	page := read(t, "../../docs/ci.md")
	start := strings.Index(page, "## GitLab CI")
	end := strings.Index(page, "## Limits")
	if start < 0 || end < start {
		t.Fatal("the CI page has no GitLab section")
	}
	section := page[start:end]

	// Every variable the script reads is on the page, and every BALLAST_ variable the
	// page asks for is one the script reads.
	matches := regexp.MustCompile(`\$\{(BALLAST_[A-Z_]+)[:-]`).FindAllStringSubmatch(read(t, "run.sh"), -1)
	inScript := map[string]bool{}
	for _, m := range matches {
		inScript[m[1]] = true
	}
	delete(inScript, "BALLAST_BIN") // left by the install step, not set by a project
	if len(inScript) < 9 {
		t.Fatalf("the script reads only %v: the pattern that finds them is wrong", inScript)
	}
	onPage := map[string]bool{}
	for _, m := range regexp.MustCompile("`(BALLAST_[A-Z_]+)`").FindAllStringSubmatch(section, -1) {
		onPage[m[1]] = true
	}
	for name := range inScript {
		if !onPage[name] {
			t.Errorf("the page does not say %s", name)
		}
	}
	for name := range onPage {
		if !inScript[name] && name != "BALLAST_VERSION" {
			t.Errorf("the page names %s, which the script does not read", name)
		}
	}

	// The file the example includes is this one, at a path that exists.
	if !strings.Contains(section, "/ops/gitlab/ballast.gitlab-ci.yml") {
		t.Error("the example does not include ops/gitlab/ballast.gitlab-ci.yml")
	}
	// The job it extends is the template's.
	if !strings.Contains(section, "ballast-load-test:") {
		t.Error("the example does not name the template's job")
	}
	for _, want := range []string{"Masked", "Protected", "fork", "resource_group", "exit code", "sums of the release", "digest", "`$$`"} {
		if !strings.Contains(section, want) {
			t.Errorf("the GitLab section does not say %q", want)
		}
	}
	if !strings.Contains(section, "`BALLAST_QUEUE_TIMEOUT`") || !strings.Contains(section, "Ten minutes by default") {
		t.Error("the section does not give the default queue timeout")
	}
}
