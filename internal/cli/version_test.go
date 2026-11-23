package cli

import (
	"context"
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A tool that runs in somebody's pipeline has to be able to say which version it is:
// to the person reading the log of a job that failed, and to the server, whose own log
// is where a misbehaving client is found.
//
// The version is the one the release put in at link time. What these tests hold in
// place is that it is said the same way everywhere, that a build with nothing injected
// says so rather than inventing one, and that whatever is injected can never break a
// request or the output: it is text from a build, and a build is not always right.
//
// Written before the code, which is why they do not compile yet.

// withVersion sets what a release would have linked in, and puts it back.
func withVersion(t *testing.T, value string) {
	t.Helper()

	previous := Version
	Version = value
	t.Cleanup(func() { Version = previous })
}

func TestABuildWithNothingInjectedSaysItIsADevelopmentBuild(t *testing.T) {
	if Version != "dev" {
		t.Errorf("the default version is %q, want \"dev\": a build nobody released must not look like one", Version)
	}
}

func TestTheVersionCommandSaysWhatBuildThisIs(t *testing.T) {
	withVersion(t, "v1.2.3")

	for _, args := range [][]string{{"version"}, {"--version"}} {
		got := invokeWith(t, envOf(), args...)

		want := "ballast v1.2.3 (" + runtime.GOOS + "/" + runtime.GOARCH + ", " + runtime.Version() + ")\n"
		if got.code != 0 || got.out != want {
			t.Errorf("%v: exit %d, stdout %q, want %q", args, got.code, got.out, want)
		}
		if got.err != "" {
			t.Errorf("%v: stderr %q", args, got.err)
		}
	}
}

// Asking which version this is needs no settings: it is what somebody runs first,
// to see whether the tool is there at all.
func TestTheVersionNeedsNeitherAnAddressNorAToken(t *testing.T) {
	got := invokeWith(t, envOf(), "version")

	if got.code != 0 || !strings.HasPrefix(got.out, "ballast ") {
		t.Errorf("exit %d, stdout %q, stderr %q", got.code, got.out, got.err)
	}
}

func TestTheHelpMentionsTheVersionCommand(t *testing.T) {
	got := invokeWith(t, envOf(), "help")

	if !strings.Contains(got.out, "ballast version") {
		t.Errorf("the help does not mention it:\n%s", got.out)
	}
}

func TestTheServerIsToldWhichVersionIsAskingWithoutAnythingElseAboutTheMachine(t *testing.T) {
	withVersion(t, "v1.2.3")
	var agent string
	client, _ := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		agent = r.Header.Get("User-Agent")
		writeJSON(w, http.StatusOK, `{"id":"r1","status":"queued","thresholds":[]}`)
	})

	if _, err := client.Run(context.Background(), "r1"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if agent != "ballast-cli/v1.2.3" {
		t.Errorf("User-Agent %q, want ballast-cli/v1.2.3", agent)
	}
}

func TestTheStreamSaysTheSameThing(t *testing.T) {
	withVersion(t, "v1.2.3")
	var agent string
	client, _ := streamer(t, func(_ int, w http.ResponseWriter, r *http.Request) {
		agent = r.Header.Get("User-Agent")
		startStream(w)
		sendEnd(w, "passed")
	})

	if _, _, err := collect(context.Background(), client); err != nil {
		t.Fatalf("Follow: %v", err)
	}

	if agent != "ballast-cli/v1.2.3" {
		t.Errorf("User-Agent %q", agent)
	}
}

// What a build puts in is text, and the tool is not to break on a build that got it
// wrong: a header with a line break in it is refused by the HTTP client, and every
// request of the tool would fail with it. Anything that is not a version is a
// development build.
func TestAVersionThatIsNotOneNeverBreaksARequestOrTheOutput(t *testing.T) {
	for name, hostile := range map[string]string{
		"a line break":     "v1.0.0\r\nX-Injected: 1",
		"a space":          "v1.0.0 beta",
		"an escape":        "v1.0.0\x1b[2J",
		"an empty value":   "",
		"a very long one":  "v" + strings.Repeat("1", 500),
		"a quote":          "v1.0.0\"",
		"a direction mark": "v1\u202e.0.0",
	} {
		t.Run(name, func(t *testing.T) {
			withVersion(t, hostile)
			var agent string
			client, _ := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
				agent = r.Header.Get("User-Agent")
				writeJSON(w, http.StatusOK, `{"id":"r1","status":"queued","thresholds":[]}`)
			})

			if _, err := client.Run(context.Background(), "r1"); err != nil {
				t.Fatalf("a bad version broke the request: %v", err)
			}
			if agent != "ballast-cli/dev" {
				t.Errorf("User-Agent %q, want ballast-cli/dev", agent)
			}

			got := invokeWith(t, envOf(), "version")
			if !strings.HasPrefix(got.out, "ballast dev (") || strings.ContainsAny(got.out[:len(got.out)-1], "\r\n\x1b\u202e") {
				t.Errorf("the output carries what was injected: %q", got.out)
			}
		})
	}
}

// A real build, with what the release passes the linker, and the binary it makes.
// Everything above is the package; this is that the injection reaches the binary at
// all, which a typo in the path of the variable would silently defeat.
func TestTheVersionTheLinkerPutsInIsTheOneTheBinarySays(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "ballast")

	build := exec.Command("go", "build", "-trimpath",
		"-ldflags", "-X github.com/youssefmahersi/ballast/internal/cli.Version=v9.8.7", "-o", binary, "../../cmd/ballast")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	// An environment of its own: nothing of this machine's is read.
	run := exec.Command(binary, "version")
	run.Env = []string{}
	out, err := run.Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if !strings.HasPrefix(string(out), "ballast v9.8.7 (") {
		t.Errorf("the binary says %q", out)
	}
}

// And with nothing injected, a binary that says so.
func TestABinaryBuiltWithoutTheLinkerSaysDev(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "ballast")

	if out, err := exec.Command("go", "build", "-o", binary, "../../cmd/ballast").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	out, err := exec.Command(binary, "version").Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if !strings.HasPrefix(string(out), "ballast dev (") {
		t.Errorf("the binary says %q", out)
	}
}
