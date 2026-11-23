package release

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Publishing is the one step of a release that cannot be taken back: a published release
// is read by pipelines the moment it exists, and the tag it sits on is then fixed. What
// these tests hold in place, against a gh that only writes down what it is asked:
//
//   - nothing is published that was not checked: the directory holds exactly the
//     binaries and the sums of a release, and the sums verify;
//   - a release is built as a draft, looked at, and only then published, so that nobody
//     ever reads one with a binary missing;
//   - an existing release is never touched, a published one is never deleted, and no
//     file is ever overwritten;
//   - when something goes wrong half-way, the draft this run made is removed and
//     nothing else is;
//   - the token is only ever in the environment gh reads it from.
//
// Written before the script, which is why they do not pass yet.

const secretToken = "ghs_THIS_MUST_NEVER_BE_PRINTED_0123456789abcdef"

const fakeGH = `#!/usr/bin/env bash
dir="${FAKE_GH_DIR:?}"
{ printf '%s' "$1"; for a in "${@:2}"; do printf '\t%s' "$a"; done; echo; } >> "$dir/calls.log"

case "$1 $2" in
  "release view")
    case " $* " in
      *" tagName "*)
        if [ -n "${FAKE_GH_BROKEN:-}" ]; then echo "HTTP 502: Bad Gateway" >&2; exit 1; fi
        if [ -n "${FAKE_GH_EXISTS:-}" ]; then echo '{"tagName":"exists"}'; exit 0; fi
        echo "release not found" >&2; exit 1 ;;
      *" isDraft "*) echo "${FAKE_GH_DRAFT:-true}"; exit 0 ;;
      *" assets "*)  echo "${FAKE_GH_ASSET_COUNT:-7}"; exit 0 ;;
    esac ;;
  "release create")
    previous=""
    for a in "$@"; do
      if [ "$previous" = "--notes-file" ]; then cp "$a" "$dir/notes.md"; fi
      previous="$a"
    done
    if [ -n "${FAKE_GH_CREATE_FAILS:-}" ]; then echo "upload failed" >&2; exit 1; fi
    exit 0 ;;
  "release edit"|"release delete") exit 0 ;;
esac
echo "fake gh: a call nobody expected: $*" >&2
exit 99
`

type call []string

func (c call) has(arg string) bool {
	for _, a := range c {
		if a == arg {
			return true
		}
	}
	return false
}

func (c call) String() string { return strings.Join(c, " ") }

type published struct {
	result
	calls []call
	notes string
	dist  string
}

// platformFiles are the files of a release, named by the build script itself.
func platformFiles(t *testing.T) []string {
	t.Helper()

	var names []string
	for _, target := range strings.Fields(runScript(t, t.TempDir(), nil, "--list-targets").out) {
		names = append(names, fileName(target))
	}
	sort.Strings(names)
	return names
}

// aRelease writes what a build leaves: the binaries and their sums. The contents are
// made up, and the sums are computed here, by a tool that is not the script's.
func aRelease(t *testing.T) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "dist")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	var sums strings.Builder
	for _, name := range platformFiles(t) {
		content := []byte("the binary " + name)
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o755); err != nil {
			t.Fatalf("write: %v", err)
		}
		digest := sha256.Sum256(content)
		fmt.Fprintf(&sums, "%s  %s\n", hex.EncodeToString(digest[:]), name)
	}
	if err := os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(sums.String()), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return dir
}

func publishScript(t *testing.T) string {
	t.Helper()

	path, err := filepath.Abs("publish.sh")
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	return path
}

// publish runs the script with a gh that records. dist is the directory to publish;
// the empty string means a good release.
func publish(t *testing.T, dist string, env []string, args ...string) published {
	t.Helper()

	if dist == "" {
		dist = aRelease(t)
	}
	bin := t.TempDir()
	state := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(fakeGH), 0o755); err != nil {
		t.Fatalf("write gh: %v", err)
	}

	if len(args) == 0 {
		args = []string{"v0.1.0"}
	}
	cmd := exec.Command("bash", append([]string{publishScript(t), args[0], dist}, args[1:]...)...)
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FAKE_GH_DIR="+state, "GH_TOKEN="+secretToken)
	cmd.Env = append(cmd.Env, env...)

	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	code := 0
	if err := cmd.Run(); err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run: %v", err)
		}
		code = exit.ExitCode()
	}

	got := published{result: result{out: out.String(), code: code}, dist: dist}
	if raw, err := os.ReadFile(filepath.Join(state, "calls.log")); err == nil {
		for _, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
			got.calls = append(got.calls, call(strings.Split(line, "\t")))
		}
	}
	if raw, err := os.ReadFile(filepath.Join(state, "notes.md")); err == nil {
		got.notes = string(raw)
	}
	assertNothingDangerous(t, got)
	return got
}

// assertNothingDangerous holds in every scenario: whatever else happened, these did not.
func assertNothingDangerous(t *testing.T, got published) {
	t.Helper()

	if strings.Contains(got.out, secretToken) {
		t.Errorf("the token was printed:\n%s", got.out)
	}
	for _, c := range got.calls {
		if strings.Contains(c.String(), secretToken) {
			t.Errorf("the token was passed as an argument: %s", c)
		}
		if c.has("--clobber") {
			t.Errorf("a file was allowed to overwrite another: %s", c)
		}
		// Deleting a release is only ever deleting the draft this run made, and never the
		// tag the maintainer pushed.
		if c.has("--cleanup-tag") {
			t.Errorf("the tag was to be deleted with a release: %s", c)
		}
	}
}

func (p published) kinds() string {
	var kinds []string
	for _, c := range p.calls {
		if len(c) >= 2 {
			kind := c[0] + " " + c[1]
			if kind == "release view" && len(c) > 4 {
				kind += " " + c[4]
			}
			kinds = append(kinds, kind)
		}
	}
	return strings.Join(kinds, " | ")
}

func (p published) find(kind string) []call {
	var found []call
	for _, c := range p.calls {
		if len(c) >= 2 && c[0]+" "+c[1] == kind {
			found = append(found, c)
		}
	}
	return found
}

// --- A release that goes well -----------------------------------------------------------------------

func TestAReleaseIsMadeAsADraftLookedAtAndOnlyThenPublished(t *testing.T) {
	got := publish(t, "", nil)

	if got.code != 0 {
		t.Fatalf("exit %d:\n%s", got.code, got.out)
	}
	want := "release view tagName | release create | release view assets | release edit"
	if got.kinds() != want {
		t.Errorf("the calls were %q, want %q", got.kinds(), want)
	}

	create := got.find("release create")[0]
	for _, flag := range []string{"--draft", "--verify-tag", "--generate-notes", "--notes-file"} {
		if !create.has(flag) {
			t.Errorf("the release is created without %s: %s", flag, create)
		}
	}
	if create.has("--prerelease") {
		t.Errorf("a release of a plain version is marked as a pre-release: %s", create)
	}
	// A title that is the tag, and not whatever the generated notes would call it.
	title := false
	for i, a := range create {
		if a == "--title" && i+1 < len(create) && create[i+1] == "v0.1.0" {
			title = true
		}
	}
	if !title {
		t.Errorf("no title: %s", create)
	}

	// Published last, by the edit that ends the draft, and by nothing else.
	edit := got.find("release edit")[0]
	if !edit.has("--draft=false") || edit[2] != "v0.1.0" {
		t.Errorf("the release is not published by ending its draft: %s", edit)
	}
}

func TestEveryFileOfTheReleaseIsUploadedAndNothingElse(t *testing.T) {
	got := publish(t, "", nil)

	create := got.find("release create")[0]
	var files []string
	for _, a := range create {
		if strings.HasPrefix(a, got.dist) {
			files = append(files, filepath.Base(a))
		}
	}
	sort.Strings(files)

	want := append(platformFiles(t), "checksums.txt")
	sort.Strings(want)
	if strings.Join(files, " ") != strings.Join(want, " ") {
		t.Errorf("uploaded %v, want %v", files, want)
	}
}

func TestAPreReleaseIsMarkedAsOne(t *testing.T) {
	for tag, want := range map[string]bool{
		"v0.1.0-rc.1": true, "v1.0.0-beta": true, "v1.2.3-alpha-1.x": true,
		"v0.1.0": false, "v10.20.30": false,
	} {
		got := publish(t, "", nil, tag)

		if got.code != 0 {
			t.Fatalf("%s: exit %d:\n%s", tag, got.code, got.out)
		}
		if has := got.find("release create")[0].has("--prerelease"); has != want {
			t.Errorf("%s: pre-release %v, want %v", tag, has, want)
		}
	}
}

func TestTheNotesTellHowToVerifyTheDownloadAndFillInTheTag(t *testing.T) {
	got := publish(t, "", nil, "v0.1.0-rc.1")

	for _, want := range []string{
		"checksums.txt", "sha256sum", "shasum -a 256", "Get-FileHash",
		"v0.1.0-rc.1", "releases/download/v0.1.0-rc.1/",
	} {
		if !strings.Contains(got.notes, want) {
			t.Errorf("the notes lack %q:\n%s", want, got.notes)
		}
	}
	if strings.Contains(got.notes, "@") {
		t.Errorf("a placeholder was left in the notes:\n%s", got.notes)
	}
	// What the sums are, and what they are not: honest about the limit of a file that
	// sits next to the thing it vouches for.
	if !strings.Contains(strings.ToLower(got.notes), "not a signature") {
		t.Errorf("the notes do not say the sums are not a signature:\n%s", got.notes)
	}
}

// The commands in the notes are the ones people will type, so they are run: for each
// platform whose tool is installed here, against a real file, and against a changed one.
func TestTheVerificationCommandsInTheNotesWork(t *testing.T) {
	got := publish(t, "", nil)

	checked := 0
	for _, line := range strings.Split(got.notes, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "grep ") || !strings.Contains(line, "checksums.txt") {
			continue
		}
		tool := "sha256sum"
		if strings.Contains(line, "shasum") {
			tool = "shasum"
		}
		if _, err := exec.LookPath(tool); err != nil {
			continue
		}

		// The command is written for one platform's file; it is run on the file of
		// that name, which the release dir holds.
		name := ""
		for _, candidate := range platformFiles(t) {
			if strings.Contains(line, " "+candidate+"$") {
				name = candidate
			}
		}
		if name == "" {
			t.Fatalf("a command that names no file of the release: %s", line)
		}

		// What a person sees: the command either prints "<name>: OK" and succeeds, or it
		// fails. Some implementations of the sum tools succeed on an empty input, so a
		// command that only trusted their status would "verify" a file the sums do not
		// list: success must mean an OK line was seen.
		run := func(dir string) (string, error) {
			cmd := exec.Command("sh", "-c", line)
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			return string(out), err
		}
		if out, err := run(got.dist); err != nil || !strings.Contains(out, name+": OK") {
			t.Errorf("%q does not verify an intact download: %v\n%s", line, err, out)
		}

		tampered := t.TempDir()
		unlisted := t.TempDir()
		for _, f := range append(platformFiles(t), "checksums.txt") {
			content, _ := os.ReadFile(filepath.Join(got.dist, f))
			original := content
			if f == name {
				content = append(append([]byte{}, content...), '!')
			}
			if err := os.WriteFile(filepath.Join(tampered, f), content, 0o755); err != nil {
				t.Fatalf("write: %v", err)
			}
			if f == "checksums.txt" {
				// The same sums, without the line of the file that was downloaded.
				var kept []string
				for _, l := range strings.Split(string(original), "\n") {
					if l != "" && !strings.HasSuffix(l, " "+name) {
						kept = append(kept, l)
					}
				}
				original = []byte(strings.Join(kept, "\n") + "\n")
			}
			if err := os.WriteFile(filepath.Join(unlisted, f), original, 0o755); err != nil {
				t.Fatalf("write: %v", err)
			}
		}
		if out, err := run(tampered); err == nil {
			t.Errorf("%q verified a changed download:\n%s", line, out)
		}
		if out, err := run(unlisted); err == nil {
			t.Errorf("%q verified a file that the sums do not list:\n%s", line, out)
		}
		checked++
	}
	if checked == 0 {
		t.Skip("neither sha256sum nor shasum is installed, or the notes hold no command to run")
	}
}

// --- What is already there --------------------------------------------------------------------------

func TestAnExistingReleaseIsNeverTouched(t *testing.T) {
	got := publish(t, "", []string{"FAKE_GH_EXISTS=1"})

	if got.code == 0 {
		t.Fatal("a release that exists was published again")
	}
	if got.kinds() != "release view tagName" {
		t.Errorf("the calls were %q: nothing but the question may be asked", got.kinds())
	}
	if !strings.Contains(got.out, "already exists") {
		t.Errorf("the refusal does not say why:\n%s", got.out)
	}
}

// Not being able to tell is not the same as there being none.
func TestWhenItCannotBeToldWhetherAReleaseExistsNothingIsCreated(t *testing.T) {
	got := publish(t, "", []string{"FAKE_GH_BROKEN=1"})

	if got.code == 0 {
		t.Fatal("a release was created without knowing whether one existed")
	}
	if got.kinds() != "release view tagName" {
		t.Errorf("the calls were %q", got.kinds())
	}
}

// --- When something goes wrong half-way -------------------------------------------------------------

func TestADraftThatCouldNotBeFilledIsRemovedAndTheTagIsLeftAlone(t *testing.T) {
	got := publish(t, "", []string{"FAKE_GH_CREATE_FAILS=1"})

	if got.code == 0 {
		t.Fatal("a failed upload was reported as a success")
	}
	if len(got.find("release edit")) != 0 {
		t.Error("a release was published after its upload failed")
	}
	deletes := got.find("release delete")
	if len(deletes) != 1 || deletes[0][2] != "v0.1.0" || !deletes[0].has("--yes") {
		t.Errorf("the draft was not removed, once, by name: %v", deletes)
	}
}

// A release that is published is a release somebody may already have read. Whatever
// happens, this script never removes one.
func TestAPublishedReleaseIsNeverDeleted(t *testing.T) {
	got := publish(t, "", []string{"FAKE_GH_CREATE_FAILS=1", "FAKE_GH_DRAFT=false"})

	if got.code == 0 {
		t.Fatal("exit 0")
	}
	if len(got.find("release delete")) != 0 {
		t.Errorf("a release that was not a draft was deleted: %v", got.find("release delete"))
	}
}

func TestADraftWithAFileMissingIsRemovedAndNotPublished(t *testing.T) {
	got := publish(t, "", []string{"FAKE_GH_ASSET_COUNT=6"})

	if got.code == 0 {
		t.Fatal("a release with a file missing was published")
	}
	if len(got.find("release edit")) != 0 {
		t.Error("it was published")
	}
	if len(got.find("release delete")) != 1 {
		t.Errorf("the draft was not removed: %v", got.find("release delete"))
	}
	if !strings.Contains(got.out, "6") || !strings.Contains(got.out, "7") {
		t.Errorf("the message does not say how many files there were and should have been:\n%s", got.out)
	}
}

// --- What is checked before anything is asked of GitHub -----------------------------------------------

func TestWhatIsNotARightReleaseIsRefusedBeforeGitHubIsAsked(t *testing.T) {
	cases := map[string]func(dir string){
		"a platform missing": func(dir string) { _ = os.Remove(filepath.Join(dir, platformFiles(t)[0])) },
		"the sums missing":   func(dir string) { _ = os.Remove(filepath.Join(dir, "checksums.txt")) },
		"a file nobody asked for": func(dir string) {
			_ = os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644)
		},
		"a binary that is not the one summed": func(dir string) {
			_ = os.WriteFile(filepath.Join(dir, platformFiles(t)[1]), []byte("another binary"), 0o755)
		},
		"a sum that is not a sum": func(dir string) {
			_ = os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte("not a checksum file\n"), 0o644)
		},
		"a directory inside": func(dir string) { _ = os.Mkdir(filepath.Join(dir, "sub"), 0o755) },
		// The files and the sums agree, and a platform is missing from both: a release
		// that is consistent and incomplete, which only the list of platforms can tell.
		"a platform missing from the files and from the sums": func(dir string) {
			missing := platformFiles(t)[0]
			_ = os.Remove(filepath.Join(dir, missing))
			raw, _ := os.ReadFile(filepath.Join(dir, "checksums.txt"))
			var kept []string
			for _, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
				if !strings.HasSuffix(line, "  "+missing) {
					kept = append(kept, line)
				}
			}
			_ = os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(strings.Join(kept, "\n")+"\n"), 0o644)
		},
	}
	for name, spoil := range cases {
		t.Run(name, func(t *testing.T) {
			dist := aRelease(t)
			spoil(dist)

			got := publish(t, dist, nil)

			if got.code == 0 {
				t.Errorf("exit 0:\n%s", got.out)
			}
			if len(got.calls) != 0 {
				t.Errorf("GitHub was asked something about a release that is not right: %v", got.calls)
			}
		})
	}
}

func TestADirectoryThatIsNotThereIsRefused(t *testing.T) {
	got := publish(t, filepath.Join(t.TempDir(), "nowhere"), nil)

	if got.code == 0 || len(got.calls) != 0 {
		t.Errorf("exit %d, calls %v", got.code, got.calls)
	}
}

func TestATagThatIsNotAVersionIsRefusedAndNothingTypedIsRun(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "PWNED")

	for _, tag := range []string{"main", "v1", "v1.2.3+build", "$(touch " + marker + ")", "v1.0.0\"; touch " + marker + " #", "`touch " + marker + "`", ""} {
		got := publish(t, "", nil, tag)

		if got.code == 0 || len(got.calls) != 0 {
			t.Errorf("%q: exit %d, calls %v", tag, got.code, got.calls)
		}
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("something typed in a tag was executed")
	}
}

func TestWithoutATokenNothingIsAskedOfGitHub(t *testing.T) {
	got := publish(t, "", []string{"GH_TOKEN="})

	if got.code == 0 || len(got.calls) != 0 {
		t.Errorf("exit %d, calls %v", got.code, got.calls)
	}
	if !strings.Contains(got.out, "GH_TOKEN") {
		t.Errorf("the message does not say what is missing:\n%s", got.out)
	}
}

// Whatever the outcome, the token was in the environment and nowhere else. The checks
// run inside publish for every scenario; this one says out loud that they ran, on a
// failure as well as on a success.
func TestTheTokenIsNowhereButTheEnvironmentWhateverHappens(t *testing.T) {
	for name, env := range map[string][]string{
		"a success": nil, "an existing release": {"FAKE_GH_EXISTS=1"}, "a failed upload": {"FAKE_GH_CREATE_FAILS=1"},
		"a file missing": {"FAKE_GH_ASSET_COUNT=1"}, "a broken GitHub": {"FAKE_GH_BROKEN=1"},
	} {
		got := publish(t, "", env)

		if strings.Contains(got.out, secretToken) || strings.Contains(got.out, "THIS_MUST_NEVER") {
			t.Errorf("%s: the token was printed:\n%s", name, got.out)
		}
	}
}
