package ballastaction

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// The Action downloads the command, checks it against the sums of its release, and only
// then hands it to the step that runs it.
//
// What these tests hold in place:
//
//   - the file asked for is the one for the runner's system and architecture, and any
//     other runner is refused before anything is downloaded;
//   - the version is one of the two things that is ever put into an address, and only
//     after it has been checked: what is typed in a workflow is text from somebody;
//   - what was downloaded is not used unless its sum is the one of the release, read
//     strictly — a checksum tool answers "success" to an empty list, so this does not
//     ask one;
//   - the download comes from one place, and the only other place is this machine, for
//     these tests: nothing a workflow sets can send it to another server;
//   - a refusal leaves no command behind to be run by mistake.
//
// Written before the script, which is why they do not pass yet.

const aVersion = "v1.2.3"

type release struct {
	mu       sync.Mutex
	requests []string
	// files is what the fake release holds, by path under the version.
	files map[string][]byte
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

// fakeCommand is a "binary" that is a shell script: it is what runs when the Action
// tries what it installed, and it says the version it was told to.
func fakeCommand(version string) []byte {
	return []byte("#!/bin/sh\nif [ \"$1\" = version ]; then echo 'ballast " + version + " (test/test, go0)'; exit 0; fi\nexit 0\n")
}

func sumOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// platforms: what a runner calls itself, and the file a release has for it.
var platforms = []struct{ os, arch, file string }{
	{"Linux", "X64", "ballast_linux_amd64"},
	{"Linux", "ARM64", "ballast_linux_arm64"},
	{"macOS", "X64", "ballast_darwin_amd64"},
	{"macOS", "ARM64", "ballast_darwin_arm64"},
	{"Windows", "X64", "ballast_windows_amd64.exe"},
	{"Windows", "ARM64", "ballast_windows_arm64.exe"},
}

// aRelease is a release of version that has the six files and honest sums.
func aRelease(version string) *release {
	r := &release{files: map[string][]byte{}}
	var sums strings.Builder
	for _, p := range platforms {
		body := fakeCommand(version)
		r.files["/"+version+"/"+p.file] = body
		sums.WriteString(sumOf(body) + "  " + p.file + "\n")
	}
	r.files["/"+version+"/checksums.txt"] = []byte(sums.String())
	return r
}

type installed struct {
	result
	asked []string
	// bin is the path the step says its command is at, empty when it says none.
	bin string
	// temp is RUNNER_TEMP.
	temp string
	// outputs is what was written to GITHUB_OUTPUT.
	outputs string
}

func install(t *testing.T, r *release, env ...string) installed {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(r.handler))
	t.Cleanup(server.Close)

	temp := t.TempDir()
	outputs := filepath.Join(t.TempDir(), "output")
	if err := os.WriteFile(outputs, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	defaults := []string{
		"RUNNER_OS=Linux", "RUNNER_ARCH=X64", "RUNNER_TEMP=" + temp,
		"GITHUB_OUTPUT=" + outputs, "BALLAST_DOWNLOAD_BASE=" + server.URL,
	}
	got := runScript(t, "install.sh", append(defaults, env...))

	data, _ := os.ReadFile(outputs)
	out := installed{result: got, asked: r.asked(), temp: temp, outputs: string(data)}
	for _, line := range strings.Split(out.outputs, "\n") {
		if path, ok := strings.CutPrefix(line, "bin="); ok {
			out.bin = path
		}
	}
	return out
}

func leftBehind(t *testing.T, dir string) []string {
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

// --- The right file for the runner --------------------------------------------------------------

func TestTheFileAskedForIsTheOneForTheRunnersSystemAndArchitecture(t *testing.T) {
	for _, p := range platforms {
		t.Run(p.os+"/"+p.arch, func(t *testing.T) {
			got := install(t, aRelease(aVersion), "RUNNER_OS="+p.os, "RUNNER_ARCH="+p.arch, "BALLAST_VERSION="+aVersion)

			if got.code != 0 {
				t.Fatalf("exit %d:\n%s", got.code, got.out)
			}
			want := []string{"/" + aVersion + "/" + p.file, "/" + aVersion + "/checksums.txt"}
			if len(got.asked) != 2 || got.asked[0] != want[0] || got.asked[1] != want[1] {
				t.Errorf("asked for %v, want %v", got.asked, want)
			}

			name := "ballast"
			if p.os == "Windows" {
				name = "ballast.exe"
			}
			if filepath.Base(got.bin) != name {
				t.Errorf("the command is %q, want one named %s", got.bin, name)
			}
			if !strings.HasPrefix(got.bin, got.temp) {
				t.Errorf("the command is outside RUNNER_TEMP: %q", got.bin)
			}
			if info, err := os.Stat(got.bin); err != nil || info.Mode()&0o100 == 0 {
				t.Errorf("the command is not there or not executable: %v", err)
			}
		})
	}
}

func TestARunnerThatHasNoFileIsRefusedBeforeAnythingIsDownloaded(t *testing.T) {
	for name, env := range map[string][]string{
		"freebsd":       {"RUNNER_OS=FreeBSD", "RUNNER_ARCH=X64"},
		"32 bits":       {"RUNNER_OS=Linux", "RUNNER_ARCH=X86"},
		"arm 32":        {"RUNNER_OS=Linux", "RUNNER_ARCH=ARM"},
		"no os":         {"RUNNER_OS="},
		"no arch":       {"RUNNER_ARCH="},
		"hostile os":    {"RUNNER_OS=Linux/../../x"},
		"hostile arch":  {"RUNNER_ARCH=X64 && touch PWNED"},
		"wrong case":    {"RUNNER_OS=linux"},
		"windows sparc": {"RUNNER_OS=Windows", "RUNNER_ARCH=SPARC"},
	} {
		t.Run(name, func(t *testing.T) {
			got := install(t, aRelease(aVersion), append([]string{"BALLAST_VERSION=" + aVersion}, env...)...)

			if got.code == 0 || len(got.asked) != 0 || got.bin != "" {
				t.Errorf("exit %d, asked %v, command %q: want a refusal before any download", got.code, got.asked, got.bin)
			}
			if strings.Contains(got.out, "PWNED") && !strings.Contains(got.out, "not supported") {
				t.Errorf("the refusal does not say what it is:\n%s", got.out)
			}
		})
	}
}

// --- The version ----------------------------------------------------------------------------------------

func TestTheVersionTheActionWasReleasedWithIsUsedWhenNoneIsGiven(t *testing.T) {
	pinned, err := os.ReadFile("VERSION")
	if err != nil {
		t.Fatalf("there is no VERSION file: %v", err)
	}
	version := strings.TrimSpace(string(pinned))

	got := install(t, aRelease(version))

	if got.code != 0 {
		t.Fatalf("exit %d:\n%s", got.code, got.out)
	}
	if len(got.asked) == 0 || !strings.HasPrefix(got.asked[0], "/"+version+"/") {
		t.Errorf("asked for %v, want files of %s", got.asked, version)
	}
}

func TestAVersionThatIsGivenWinsOverTheOneTheActionWasReleasedWith(t *testing.T) {
	got := install(t, aRelease("v9.9.9"), "BALLAST_VERSION=v9.9.9")

	if got.code != 0 || len(got.asked) == 0 || !strings.HasPrefix(got.asked[0], "/v9.9.9/") {
		t.Errorf("exit %d, asked %v:\n%s", got.code, got.asked, got.out)
	}
}

// versions that are, and are not, a version: the same list build.sh is asked below.
var (
	accepted = []string{"v0.0.0", "v1.2.3", "v10.20.30", "v1.2.3-rc.1", "v1.2.3-alpha", "v1.2.3-0.3.7", "v1.0.0-x-y-z.1"}
	refused  = map[string]string{
		"no v":            "1.2.3",
		"two parts":       "v1.2",
		"four parts":      "v1.2.3.4",
		"leading zero":    "v01.2.3",
		"build metadata":  "v1.2.3+build",
		"latest":          "latest",
		"a branch":        "main",
		"path":            "v1.2.3/../../x",
		"slash":           "v1.2.3/x",
		"space":           "v1.2.3 x",
		"newline":         "v1.2.3\nx",
		"command":         "v1.2.3$(touch PWNED)",
		"semicolon":       "v1.2.3;touch PWNED",
		"quote":           "v1.2.3'",
		"empty pre":       "v1.2.3-",
		"leading zero id": "v1.2.3-01",
		"unicode":         "v1.2.3-\u00e9",
		"too long":        "v1.2.3-" + strings.Repeat("a", 60),
		"percent":         "v1.2.3%2f",
		"query":           "v1.2.3?x=1",
	}
)

func TestOnlyAVersionIsPutIntoAnAddress(t *testing.T) {
	for _, version := range accepted {
		got := install(t, aRelease(version), "BALLAST_VERSION="+version)
		if got.code != 0 {
			t.Errorf("%q was refused:\n%s", version, got.out)
		}
	}

	dir := t.TempDir()
	for name, version := range refused {
		t.Run(name, func(t *testing.T) {
			got := install(t, aRelease(aVersion), "BALLAST_VERSION="+version)

			if got.code == 0 || len(got.asked) != 0 || got.bin != "" {
				t.Errorf("exit %d, asked %v: %q was not refused before the download", got.code, got.asked, version)
			}
			for _, hostile := range []string{"PWNED", "../", "\u00e9", "\x1b"} {
				if strings.Contains(version, hostile) && strings.Contains(got.out, hostile) {
					t.Errorf("the refusal repeats what it was given (%q):\n%s", hostile, got.out)
				}
			}
			if _, err := os.Stat(filepath.Join(dir, "PWNED")); err == nil {
				t.Error("something typed in a version was executed")
			}
		})
	}
}

// The release script is the one definition of what a version is. The Action cannot call
// it, since it lives elsewhere, so it has a copy; this keeps the copy honest.
func TestTheActionAndTheReleaseScriptAgreeOnWhatAVersionIs(t *testing.T) {
	build, err := filepath.Abs("../release/build.sh")
	if err != nil {
		t.Fatal(err)
	}

	all := append([]string{}, accepted...)
	for _, v := range refused {
		all = append(all, v)
	}
	all = append(all, "v1.2.3-"+strings.Repeat("a", 57), "v1.2.3-"+strings.Repeat("a", 58), "v", "V1.2.3")

	for _, version := range all {
		release := exec.Command("bash", build, "--check-version", version).Run() == nil
		got := install(t, aRelease(version), "BALLAST_VERSION="+version)
		action := got.code == 0 || len(got.asked) != 0

		if release != action {
			t.Errorf("%q: the release script says %v and the Action says %v", version, release, action)
		}
	}
}

func TestTheVersionTheActionIsReleasedWithIsAVersion(t *testing.T) {
	pinned, err := os.ReadFile("VERSION")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(pinned), "\n") > 1 {
		t.Errorf("VERSION has more than one line: %q", pinned)
	}
	build, _ := filepath.Abs("../release/build.sh")
	if err := exec.Command("bash", build, "--check-version", strings.TrimSpace(string(pinned))).Run(); err != nil {
		t.Errorf("VERSION is not a version: %q", pinned)
	}
}

// --- What was downloaded is the release ---------------------------------------------------------

func TestWhatWasDownloadedIsNotUsedUnlessItsSumIsTheOneOfTheRelease(t *testing.T) {
	file := "ballast_linux_amd64"
	good := fakeCommand(aVersion)
	other := fakeCommand("v0.0.1")

	for name, mutate := range map[string]func(*release){
		"another sum": func(r *release) {
			r.files["/"+aVersion+"/checksums.txt"] = []byte(sumOf(other) + "  " + file + "\n")
		},
		"a changed byte": func(r *release) {
			r.files["/"+aVersion+"/"+file] = append(append([]byte{}, good...), '\n')
		},
		"no line for the file": func(r *release) {
			r.files["/"+aVersion+"/checksums.txt"] = []byte(sumOf(good) + "  ballast_linux_arm64\n")
		},
		"two lines for the file": func(r *release) {
			line := sumOf(good) + "  " + file + "\n"
			r.files["/"+aVersion+"/checksums.txt"] = []byte(line + line)
		},
		"two lines that disagree": func(r *release) {
			r.files["/"+aVersion+"/checksums.txt"] = []byte(sumOf(good) + "  " + file + "\n" + sumOf(other) + "  " + file + "\n")
		},
		"an empty list": func(r *release) {
			r.files["/"+aVersion+"/checksums.txt"] = nil
		},
		"prose": func(r *release) {
			r.files["/"+aVersion+"/checksums.txt"] = []byte("Everything is fine, trust me.\n")
		},
		"upper case hex": func(r *release) {
			r.files["/"+aVersion+"/checksums.txt"] = []byte(strings.ToUpper(sumOf(good)) + "  " + file + "\n")
		},
		"one space": func(r *release) {
			r.files["/"+aVersion+"/checksums.txt"] = []byte(sumOf(good) + " " + file + "\n")
		},
		"a short sum": func(r *release) {
			r.files["/"+aVersion+"/checksums.txt"] = []byte(sumOf(good)[:63] + "  " + file + "\n")
		},
		"the name with a path": func(r *release) {
			r.files["/"+aVersion+"/checksums.txt"] = []byte(sumOf(good) + "  ./" + file + "\n")
		},
		"a name that only begins like the file": func(r *release) {
			r.files["/"+aVersion+"/checksums.txt"] = []byte(sumOf(good) + "  " + file + "x\n")
		},
		"the sums are missing": func(r *release) {
			delete(r.files, "/"+aVersion+"/checksums.txt")
		},
		"the command is missing": func(r *release) {
			delete(r.files, "/"+aVersion+"/"+file)
		},
		"an empty command": func(r *release) {
			r.files["/"+aVersion+"/"+file] = nil
			r.files["/"+aVersion+"/checksums.txt"] = []byte(sumOf(nil) + "  " + file + "\n")
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := aRelease(aVersion)
			mutate(r)

			got := install(t, r, "BALLAST_VERSION="+aVersion)

			if got.code == 0 || got.bin != "" {
				t.Errorf("exit %d, command %q: it was used\n%s", got.code, got.bin, got.out)
			}
			if left := leftBehind(t, got.temp); len(left) != 0 {
				t.Errorf("files were left behind: %v", left)
			}
		})
	}
}

func TestACommandThatSaysAnotherVersionIsNotUsed(t *testing.T) {
	r := aRelease(aVersion)
	wrong := fakeCommand("v0.0.1")
	r.files["/"+aVersion+"/ballast_linux_amd64"] = wrong
	r.files["/"+aVersion+"/checksums.txt"] = []byte(sumOf(wrong) + "  ballast_linux_amd64\n")

	got := install(t, r, "BALLAST_VERSION="+aVersion)

	if got.code == 0 || got.bin != "" {
		t.Errorf("exit %d, command %q: a command that is not the version asked for was used", got.code, got.bin)
	}
}

func TestACommandThatCannotRunIsNotUsed(t *testing.T) {
	r := aRelease(aVersion)
	broken := []byte("#!/bin/sh\nexit 3\n")
	r.files["/"+aVersion+"/ballast_linux_amd64"] = broken
	r.files["/"+aVersion+"/checksums.txt"] = []byte(sumOf(broken) + "  ballast_linux_amd64\n")

	got := install(t, r, "BALLAST_VERSION="+aVersion)

	if got.code == 0 || got.bin != "" {
		t.Errorf("exit %d, command %q", got.code, got.bin)
	}
}

func TestWhatIsInTheSumsForOtherFilesDoesNotMatter(t *testing.T) {
	r := aRelease(aVersion)
	good := fakeCommand(aVersion)
	r.files["/"+aVersion+"/checksums.txt"] = []byte("not a line\n" + sumOf(nil) + "  ballast_linux_arm64\n" + sumOf(good) + "  ballast_linux_amd64\n")

	if got := install(t, r, "BALLAST_VERSION="+aVersion); got.code != 0 {
		t.Errorf("exit %d:\n%s", got.code, got.out)
	}
}

// --- Where it downloads from -------------------------------------------------------------------------

func TestTheDownloadIsFromTheReleasesOfBallastAndFromNowhereElse(t *testing.T) {
	// The default is the project's own releases. It cannot be fetched here, so the
	// script is read for it; what is run is the refusal of everything else.
	script, err := os.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(script), "https://github.com/myballast/ballast/releases/download") {
		t.Error("install.sh does not download from the releases of myballast/ballast")
	}

	for name, base := range map[string]string{
		"another host":        "https://evil.test/releases",
		"plain http":          "http://github.com/myballast/ballast/releases/download",
		"credentials":         "http://127.0.0.1:80@evil.test",
		"a lookalike":         "http://127.0.0.1.evil.test:80",
		"localhost lookalike": "http://localhost.evil.test:80",
		"no port":             "http://127.0.0.1",
		"file":                "file:///etc",
		"ftp":                 "ftp://127.0.0.1:21",
		"private address":     "http://10.0.0.1:80",
		"metadata address":    "http://169.254.169.254:80",
		"space":               "http://127.0.0.1:80 --output /x",
	} {
		t.Run(name, func(t *testing.T) {
			got := runScript(t, "install.sh", []string{
				"RUNNER_OS=Linux", "RUNNER_ARCH=X64", "RUNNER_TEMP=" + t.TempDir(),
				"BALLAST_VERSION=" + aVersion, "BALLAST_DOWNLOAD_BASE=" + base,
			})
			if got.code == 0 {
				t.Errorf("%q was accepted:\n%s", base, got.out)
			}
		})
	}
}

func TestRedirectsAreOnlyFollowedWithinTheProtocolsThatWereAllowed(t *testing.T) {
	// A release's files are served from another host after a redirect, and a redirect
	// to plain http must not be followed. The test server cannot be made to look like
	// GitHub, so what is checked is that the restriction is written for the redirect
	// as well as for the first request.
	script, err := os.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"--proto ", "--proto-redir ", "--max-redirs ", "--max-time ", "--max-filesize "} {
		if !strings.Contains(string(script), flag) {
			t.Errorf("install.sh does not use curl's %s", strings.TrimSpace(flag))
		}
	}
}

func TestAnAddressThatRedirectsAwayFromTheAllowedProtocolsIsRefused(t *testing.T) {
	// Served from here, with a redirect to a scheme the download does not allow.
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "ftp://127.0.0.1:1/x", http.StatusFound)
	}))
	t.Cleanup(redirect.Close)

	got := runScript(t, "install.sh", []string{
		"RUNNER_OS=Linux", "RUNNER_ARCH=X64", "RUNNER_TEMP=" + t.TempDir(),
		"BALLAST_VERSION=" + aVersion, "BALLAST_DOWNLOAD_BASE=" + redirect.URL,
	})
	if got.code == 0 {
		t.Errorf("a redirect to ftp was followed:\n%s", got.out)
	}
}

func TestAFailureSaysWhoSpeaksAndNeverRepeatsTheToken(t *testing.T) {
	got := install(t, aRelease(aVersion), "BALLAST_VERSION=v7.7.7", "BALLAST_TOKEN=blt_secret_value_123456")

	if got.code == 0 {
		t.Fatal("a release that does not exist was installed")
	}
	if strings.Contains(got.out, "blt_secret_value_123456") {
		t.Errorf("the token is in the output:\n%s", got.out)
	}
	if !strings.Contains(got.out, "ballast-action:") {
		t.Errorf("the message does not say who speaks:\n%s", got.out)
	}
}

func TestAnAddressWithCredentialsIsNeverReadAsTheHostItNames(t *testing.T) {
	// "http://127.0.0.1:1@host:port" is a request to host:port, with a user called
	// 127.0.0.1. Here host:port is the fake release, and not one request may reach it.
	r := aRelease(aVersion)
	server := httptest.NewServer(http.HandlerFunc(r.handler))
	t.Cleanup(server.Close)
	hostPort := strings.TrimPrefix(server.URL, "http://")

	got := runScript(t, "install.sh", []string{
		"RUNNER_OS=Linux", "RUNNER_ARCH=X64", "RUNNER_TEMP=" + t.TempDir(),
		"BALLAST_VERSION=" + aVersion, "BALLAST_DOWNLOAD_BASE=http://127.0.0.1:1@" + hostPort,
	})

	if got.code == 0 || len(r.asked()) != 0 {
		t.Errorf("exit %d, %d requests reached the server: the address was read as the host after the @", got.code, len(r.asked()))
	}
}
