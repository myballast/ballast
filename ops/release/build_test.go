// Package release holds the tests of the release scripts that sit next to it.
//
// The scripts are what a release really does — the workflow that runs them is only
// the glue that calls them on a tag — so they are tested here, on this machine, and
// not by pushing a tag and watching.
package release

import (
	"bytes"
	"crypto/sha256"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
)

// What a release ships is six binaries and the sums that go with them, built the
// same way every time. What these tests hold in place:
//
//   - the binaries are the ones a pipeline downloads: named for their platform, built
//     for it, carrying the version they were released under;
//   - the sums are the ones a person can check with the tools their machine already
//     has, and checking them notices a changed byte;
//   - the same source gives the same bytes, from another directory and with another
//     cache, so that a checksum means "this source" and not "this build";
//   - nothing that is not a version is ever built, and nothing is overwritten.
//
// Written before the script, which is why they do not pass yet.

const theVersion = "v0.1.0-rc.1"

func scriptPath(t *testing.T) string {
	t.Helper()

	path, err := filepath.Abs("build.sh")
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	return path
}

type result struct {
	out  string
	code int
}

// runScript runs the build script. dir is where it is run from, which is never the
// repository: a script that only works from the root is one that breaks in a workflow.
func runScript(t *testing.T, dir string, env []string, args ...string) result {
	t.Helper()

	cmd := exec.Command("bash", append([]string{scriptPath(t)}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)

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
	t.Fatalf("run the script: %v", err)
	return result{}
}

// theTargets are what is built for the tests: the platform this machine runs, so that
// a binary can be executed, and windows, so that the .exe is seen. The six of a release
// are listed by the script itself and checked separately.
func theTargets() []string {
	targets := []string{"windows/amd64"}
	host := runtime.GOOS + "/" + runtime.GOARCH
	switch host {
	case "linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64", "windows/arm64":
		targets = append(targets, host)
	default:
		targets = append(targets, "linux/amd64")
	}
	return targets
}

var (
	oneBuild   sync.Once
	builtDir   string
	builtOut   string
	builtCode  int
	builtError error
)

// built is a release built once, for the tests that read it.
func built(t *testing.T) string {
	t.Helper()

	oneBuild.Do(func() {
		dir, err := os.MkdirTemp("", "ballast-release-")
		if err != nil {
			builtError = err
			return
		}
		builtDir = filepath.Join(dir, "dist")
		got := runScript(t, dir, []string{"TARGETS=" + strings.Join(theTargets(), " ")}, theVersion, builtDir)
		builtOut, builtCode = got.out, got.code
	})
	if builtError != nil {
		t.Fatalf("temp dir: %v", builtError)
	}
	if builtCode != 0 {
		t.Fatalf("the build failed (exit %d):\n%s", builtCode, builtOut)
	}
	return builtDir
}

func fileName(target string) string {
	os, arch, _ := strings.Cut(target, "/")
	name := "ballast_" + os + "_" + arch
	if os == "windows" {
		name += ".exe"
	}
	return name
}

// --- What is built ---------------------------------------------------------------------------------------

func TestTheReleaseHasTheSixPlatformsItPromises(t *testing.T) {
	got := runScript(t, t.TempDir(), nil, "--list-targets")

	if got.code != 0 {
		t.Fatalf("exit %d:\n%s", got.code, got.out)
	}
	lines := strings.Fields(got.out)
	sort.Strings(lines)
	want := []string{
		"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64", "windows/amd64", "windows/arm64",
	}
	if strings.Join(lines, " ") != strings.Join(want, " ") {
		t.Errorf("targets %v, want %v", lines, want)
	}
}

func TestTheBuildMakesTheBinariesAndTheirSumsAndNothingElse(t *testing.T) {
	dir := built(t)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read the output: %v", err)
	}
	var got []string
	for _, entry := range entries {
		got = append(got, entry.Name())
	}
	sort.Strings(got)

	want := []string{"checksums.txt"}
	for _, target := range theTargets() {
		want = append(want, fileName(target))
	}
	sort.Strings(want)

	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the output holds %v, want %v: a temporary file was left, or one is missing", got, want)
	}
}

// The binary is built for the platform it is named for, system and architecture. A
// script that forgot to pass either to the compiler would name six copies of the same
// binary six ways; the first bytes of a file cannot tell amd64 from arm64, so the
// executable's own header is read, with the parser of its format.
func TestEachBinaryIsBuiltForThePlatformItIsNamedFor(t *testing.T) {
	dir := built(t)

	for _, target := range theTargets() {
		goos, goarch, _ := strings.Cut(target, "/")
		path := filepath.Join(dir, fileName(target))

		var got string
		switch goos {
		case "linux":
			f, err := elf.Open(path)
			if err != nil {
				t.Fatalf("%s is not an ELF executable: %v", path, err)
			}
			defer func() { _ = f.Close() }()
			got = map[elf.Machine]string{elf.EM_X86_64: "amd64", elf.EM_AARCH64: "arm64"}[f.Machine]
		case "windows":
			f, err := pe.Open(path)
			if err != nil {
				t.Fatalf("%s is not a PE executable: %v", path, err)
			}
			defer func() { _ = f.Close() }()
			got = map[uint16]string{pe.IMAGE_FILE_MACHINE_AMD64: "amd64", pe.IMAGE_FILE_MACHINE_ARM64: "arm64"}[f.Machine]
		case "darwin":
			f, err := macho.Open(path)
			if err != nil {
				t.Fatalf("%s is not a Mach-O executable: %v", path, err)
			}
			defer func() { _ = f.Close() }()
			got = map[macho.Cpu]string{macho.CpuAmd64: "amd64", macho.CpuArm64: "arm64"}[f.Cpu]
		}

		if got != goarch {
			t.Errorf("%s is built for %q, not %q", fileName(target), got, goarch)
		}

		// -s -w: no symbol table and no debugging information. They hold the paths and
		// the names of everything, they are most of what could differ between two
		// machines, and nobody running a pipeline has any use for them.
		if stripped := isStripped(t, path, goos); !stripped {
			t.Errorf("%s still carries symbols or debugging information", fileName(target))
		}

		// -X: the version is in the binary, and it is the one the release is for. This is
		// the only way to see it in a binary that cannot run here.
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if !bytes.Contains(content, []byte(theVersion)) {
			t.Errorf("%s does not carry its version, %s", fileName(target), theVersion)
		}
	}
}

// isStripped says whether an executable has neither a symbol table nor debugging
// information, read with the parser of its format.
func isStripped(t *testing.T, path, goos string) bool {
	t.Helper()

	switch goos {
	case "linux":
		f, err := elf.Open(path)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		defer func() { _ = f.Close() }()
		_, symbolsErr := f.Symbols()
		_, dwarfErr := f.DWARF()
		return errors.Is(symbolsErr, elf.ErrNoSymbols) && dwarfErr != nil
	case "windows":
		f, err := pe.Open(path)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		defer func() { _ = f.Close() }()
		_, dwarfErr := f.DWARF()
		return len(f.Symbols) == 0 && dwarfErr != nil
	default:
		f, err := macho.Open(path)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		defer func() { _ = f.Close() }()
		_, dwarfErr := f.DWARF()
		// A Mach-O keeps the symbols it imports from the system's library, whatever the
		// linker is told. What -s removes is the program's own: Go's, and ours.
		if f.Symtab != nil {
			for _, symbol := range f.Symtab.Syms {
				if strings.HasPrefix(symbol.Name, "runtime.") || strings.HasPrefix(symbol.Name, "main.") ||
					strings.Contains(symbol.Name, "ballast") {
					return false
				}
			}
		}
		return dwarfErr != nil
	}
}

// What was built with is written in the binary, and is the same list on every machine:
// no cgo, the paths trimmed, no stamp of the repository's state, and the version. A
// release whose flags drift is one nobody can reproduce, and what the binary says about
// itself is the only place to look.
func TestTheBinariesSayTheyWereBuiltTheWayTheyShouldBe(t *testing.T) {
	dir := built(t)

	for _, target := range theTargets() {
		out, err := exec.Command("go", "version", "-m", filepath.Join(dir, fileName(target))).Output()
		if err != nil {
			t.Fatalf("go version -m: %v", err)
		}
		info := string(out)

		// -ldflags is not among them: Go leaves it out of the record when the paths are
		// trimmed. What it set is read from the binary itself, in the tests around this one.
		for _, want := range []string{"CGO_ENABLED=0", "-trimpath=true"} {
			if !strings.Contains(info, want) {
				t.Errorf("%s: the build settings lack %q:\n%s", fileName(target), want, info)
			}
		}
		// A stamp of the repository's state changes with it: the same source, clean or
		// with a stray file next to it, would be two different binaries.
		if strings.Contains(info, "vcs") {
			t.Errorf("%s carries the state of the repository:\n%s", fileName(target), info)
		}
		if strings.HasSuffix(target, "/amd64") && !strings.Contains(info, "GOAMD64=v1") {
			t.Errorf("%s does not say which amd64 level it needs:\n%s", fileName(target), info)
		}
	}
}

// No path of the machine that built it is in the binary: not the repository's, not the
// Go installation's, not the temporary directory's, not the home directory. They are
// what would make the same source two different binaries on two machines, and what a
// binary has no business telling anybody.
func TestNoPathOfTheBuildMachineIsInTheBinaries(t *testing.T) {
	dir := built(t)

	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	goroot, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		t.Fatalf("go env: %v", err)
	}
	home, _ := os.UserHomeDir()

	secrets := map[string]string{
		"the repository": root, "the Go installation": strings.TrimSpace(string(goroot)),
		"the output directory": filepath.Dir(dir), "the home directory": home,
	}
	for _, target := range theTargets() {
		content, err := os.ReadFile(filepath.Join(dir, fileName(target)))
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		for what, path := range secrets {
			// The root of the file system is a prefix of everything and says nothing.
			if len(path) > 3 && bytes.Contains(content, []byte(path)) {
				t.Errorf("%s carries the path of %s: %s", fileName(target), what, path)
			}
		}
	}
}

func TestTheBinaryThatCanRunHereSaysTheVersionItWasReleasedUnder(t *testing.T) {
	dir := built(t)
	host := runtime.GOOS + "/" + runtime.GOARCH

	var binary string
	for _, target := range theTargets() {
		if target == host {
			binary = filepath.Join(dir, fileName(target))
		}
	}
	if binary == "" {
		t.Skipf("no binary of this release runs on %s", host)
	}

	cmd := exec.Command(binary, "version")
	cmd.Env = []string{}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if !strings.HasPrefix(string(out), "ballast "+theVersion+" (") {
		t.Errorf("the binary says %q, want it to say %s", out, theVersion)
	}
}

// --- The sums --------------------------------------------------------------------------------------------------

var sumLine = regexp.MustCompile(`^[0-9a-f]{64}  (ballast_[a-z0-9_]+(\.exe)?)$`)

func TestTheSumsAreInTheFormatEveryChecksumToolReads(t *testing.T) {
	dir := built(t)
	raw, err := os.ReadFile(filepath.Join(dir, "checksums.txt"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if !bytes.HasSuffix(raw, []byte("\n")) || bytes.Contains(raw, []byte("\r")) {
		t.Errorf("the file must end with one line break and have no carriage return: %q", raw[len(raw)-5:])
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")

	var names []string
	for _, line := range lines {
		match := sumLine.FindStringSubmatch(line)
		if match == nil {
			t.Fatalf("a line that is not \"<64 hex>  <name>\": %q", line)
		}
		names = append(names, match[1])

		// Computed here, and not by the tool that wrote it: a sum that is checked by the
		// code that made it proves nothing.
		content, err := os.ReadFile(filepath.Join(dir, match[1]))
		if err != nil {
			t.Fatalf("the sum names a file that is not there: %s", match[1])
		}
		digest := sha256.Sum256(content)
		if line[:64] != hex.EncodeToString(digest[:]) {
			t.Errorf("the sum of %s is wrong", match[1])
		}
	}

	if !sort.StringsAreSorted(names) {
		t.Errorf("the lines are not in order: %v", names)
	}
	if len(names) != len(theTargets()) {
		t.Errorf("%d line(s) for %d binaries", len(names), len(theTargets()))
	}
}

// checkCommand is the command a person types to verify a download, with the tool their
// machine has.
func checkCommand(t *testing.T) []string {
	t.Helper()

	if path, err := exec.LookPath("sha256sum"); err == nil {
		return []string{path, "-c", "checksums.txt"}
	}
	if path, err := exec.LookPath("shasum"); err == nil {
		return []string{path, "-a", "256", "-c", "checksums.txt"}
	}
	t.Skip("neither sha256sum nor shasum is installed")
	return nil
}

func TestTheSumsAreVerifiedByTheToolsAPersonHasAndAChangedByteIsNoticed(t *testing.T) {
	dir := built(t)
	command := checkCommand(t)

	verify := func(in string) error {
		cmd := exec.Command(command[0], command[1:]...)
		cmd.Dir = in
		return cmd.Run()
	}

	if err := verify(dir); err != nil {
		t.Fatalf("a release does not verify with %s: %v", command[0], err)
	}

	// The same files, with one byte changed in one of them.
	copyDir := t.TempDir()
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if strings.HasPrefix(entry.Name(), "ballast_") && strings.Contains(entry.Name(), "windows") {
			content[len(content)/2] ^= 0xff
		}
		if err := os.WriteFile(filepath.Join(copyDir, entry.Name()), content, 0o755); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if err := verify(copyDir); err == nil {
		t.Error("a binary with a changed byte verified")
	}
}

// --- Reproducibility ---------------------------------------------------------------------------------------

// The same source, built from another place with another cache, is the same bytes. A
// checksum then says which source this is; it says nothing about when or where it was built.
func TestTheSameSourceGivesTheSameBytesFromAnotherDirectoryAndAnotherCache(t *testing.T) {
	first := built(t)

	elsewhere := t.TempDir()
	second := filepath.Join(elsewhere, "somewhere", "else")
	target := theTargets()[0]
	got := runScript(t, elsewhere, []string{"TARGETS=" + target, "GOCACHE=" + t.TempDir()}, theVersion, second)
	if got.code != 0 {
		t.Fatalf("the second build failed (exit %d):\n%s", got.code, got.out)
	}

	a, err := os.ReadFile(filepath.Join(first, fileName(target)))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(second, fileName(target)))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Errorf("%s differs between two builds of the same source", fileName(target))
	}
}

// Another version is another binary: the version is in it, which is the point of it.
func TestAnotherVersionIsAnotherBinary(t *testing.T) {
	first := built(t)
	target := theTargets()[0]

	other := filepath.Join(t.TempDir(), "dist")
	got := runScript(t, t.TempDir(), []string{"TARGETS=" + target}, "v0.1.0", other)
	if got.code != 0 {
		t.Fatalf("exit %d:\n%s", got.code, got.out)
	}

	a, _ := os.ReadFile(filepath.Join(first, fileName(target)))
	b, _ := os.ReadFile(filepath.Join(other, fileName(target)))
	if bytes.Equal(a, b) {
		t.Error("two versions gave the same binary: the version is not in it")
	}
}

// --- What is refused -----------------------------------------------------------------------------------------

var acceptedVersions = []string{
	"v0.1.0", "v1.2.3", "v10.20.30", "v1.2.3-rc.1", "v1.2.3-alpha", "v1.0.0-0.3.7",
	"v1.0.0-x.7.z.92", "v1.0.0-alpha-a.b-c-somethinglong", "v0.0.0",
}

var refusedVersions = map[string]string{
	"nothing":               "",
	"two numbers":           "v1.2",
	"one number":            "v1",
	"no v":                  "1.2.3",
	"a branch":              "main",
	"build metadata":        "v1.2.3+build",
	"a leading zero":        "v01.2.3",
	"a leading zero, minor": "v1.02.3",
	"a leading zero, patch": "v1.2.03",
	"an empty pre-release":  "v1.2.3-",
	"a numeric pre-release with a leading zero": "v1.2.3-01",
	"an empty identifier":                       "v1.2.3-rc..1",
	"a trailing dot":                            "v1.2.3-rc.",
	"a capital V":                               "V1.2.3",
	"a space before":                            " v1.2.3",
	"a space after":                             "v1.2.3 ",
	"a line break after":                        "v1.2.3\n",
	"a line break inside":                       "v1.2.3\nv1.2.4",
	"a quote and a command":                     "v1.0.0\"; touch PWNED #",
	"a command substitution":                    "$(touch PWNED)",
	"a backtick command":                        "v1.0.0`touch PWNED`",
	"a semicolon":                               "v1.0.0;touch PWNED",
	"a path":                                    "../v1.0.0",
	"an accented letter":                        "v1.2.3-\u00e9",
	"more than sixty-four characters":           "v1.2.3-" + strings.Repeat("a", 60),
}

func TestOnlyAVersionIsAccepted(t *testing.T) {
	dir := t.TempDir()

	for _, version := range acceptedVersions {
		if got := runScript(t, dir, nil, "--check-version", version); got.code != 0 {
			t.Errorf("%q was refused:\n%s", version, got.out)
		}
	}
	for name, version := range refusedVersions {
		got := runScript(t, dir, nil, "--check-version", version)

		if got.code == 0 {
			t.Errorf("%s: %q was accepted", name, version)
		}
		// It says what a version looks like, in one line, and does not repeat what it
		// was given: a tag is text from somebody, and a log is read by others. (A value
		// as harmless as "v1.2" cannot be told from the example the message gives, so
		// what is looked for is what only a hostile one carries.)
		if strings.Count(got.out, "\n") != 1 {
			t.Errorf("%s: the refusal is not one line:\n%s", name, got.out)
		}
		for _, hostile := range []string{"PWNED", "touch", "../", "\u00e9", "\x1b"} {
			if strings.Contains(version, hostile) && strings.Contains(got.out, hostile) {
				t.Errorf("%s: the refusal repeats what it was given (%q):\n%s", name, hostile, got.out)
			}
		}
	}

	// Nothing that was said was run.
	if _, err := os.Stat(filepath.Join(dir, "PWNED")); err == nil {
		t.Error("something typed in a version was executed")
	}
}

func TestAVersionThatIsRefusedBuildsNothing(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "dist")

	got := runScript(t, dir, []string{"TARGETS=linux/amd64"}, "v1.0.0\"; touch PWNED #", out)

	if got.code == 0 {
		t.Fatal("the build went ahead")
	}
	for _, path := range []string{out, filepath.Join(dir, "PWNED")} {
		if _, err := os.Stat(path); err == nil {
			t.Errorf("%s exists after a refused version", path)
		}
	}
}

func TestAPlatformThatIsNotInTheListIsRefused(t *testing.T) {
	dir := t.TempDir()

	for _, targets := range []string{"freebsd/amd64", "linux/386", "linux/amd64; touch PWNED", "linux/amd64 $(touch PWNED)", "../../etc", "linux"} {
		out := filepath.Join(dir, "dist")

		got := runScript(t, dir, []string{"TARGETS=" + targets}, theVersion, out)

		if got.code == 0 {
			t.Errorf("%q was accepted", targets)
		}
		if _, err := os.Stat(out); err == nil {
			t.Errorf("%q: an output was made", targets)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "PWNED")); err == nil {
		t.Error("something typed in a platform was executed")
	}
}

// A release is never written over what is there: whoever runs it again by mistake does
// not find the first one gone, and two builds never mix into one output.
func TestAnOutputThatIsNotEmptyIsNeverWrittenInto(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "dist")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	precious := filepath.Join(out, "ballast_linux_amd64")
	if err := os.WriteFile(precious, []byte("the first release"), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}

	got := runScript(t, dir, []string{"TARGETS=linux/amd64"}, theVersion, out)

	if got.code == 0 {
		t.Error("the build went ahead into a directory that was not empty")
	}
	if content, _ := os.ReadFile(precious); string(content) != "the first release" {
		t.Errorf("what was there was overwritten: %q", content)
	}
	if entries, _ := os.ReadDir(out); len(entries) != 1 {
		t.Errorf("%d entries in the output, want the one that was there", len(entries))
	}
}

func TestAnEmptyOutputIsFine(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "dist")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got := runScript(t, dir, []string{"TARGETS=" + theTargets()[0]}, theVersion, out)

	if got.code != 0 {
		t.Errorf("exit %d:\n%s", got.code, got.out)
	}
}

// The environment of a runner is not the repository's: a variable somebody set for
// another project must not change what is built.
func TestAnEnvironmentThatWouldChangeTheBuildDoesNot(t *testing.T) {
	first := built(t)
	target := theTargets()[0]
	out := filepath.Join(t.TempDir(), "dist")

	got := runScript(t, t.TempDir(), []string{
		"TARGETS=" + target, "GOFLAGS=-ldflags=-X=main.nothing=1 -tags=whatever", "GOAMD64=v3", "GOEXPERIMENT=loopvar", "GOWORK=/nonexistent",
	}, theVersion, out)

	if got.code != 0 {
		t.Fatalf("exit %d:\n%s", got.code, got.out)
	}
	a, _ := os.ReadFile(filepath.Join(first, fileName(target)))
	b, _ := os.ReadFile(filepath.Join(out, fileName(target)))
	if !bytes.Equal(a, b) {
		t.Error("a variable of the environment changed the binary")
	}
}
