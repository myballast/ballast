package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// verify.sh is what stands between a directory and a published release, and the sum tools
// a machine has are more forgiving than that: some answer "fine" to a file with no sum in
// it, and to a file that lists nothing. So the script reads the sums itself and asks the
// tool for the arithmetic only. What these tests hold in place is that every way the
// directory can be wrong is refused, including the ones a tool alone would let through.
//
// Written before the script, which is why they do not pass yet.

func verifyScript(t *testing.T) string {
	t.Helper()

	path, err := filepath.Abs("verify.sh")
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	return path
}

func runVerify(t *testing.T, dir string) result {
	t.Helper()

	cmd := exec.Command("bash", verifyScript(t), dir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return result{out: string(out)}
	}
	exit, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("run: %v", err)
	}
	return result{out: string(out), code: exit.ExitCode()}
}

func TestADirectoryThatIsRightVerifies(t *testing.T) {
	if got := runVerify(t, aRelease(t)); got.code != 0 {
		t.Errorf("exit %d:\n%s", got.code, got.out)
	}
}

func TestEveryWayTheDirectoryCanBeWrongIsRefused(t *testing.T) {
	sums := func(dir string) string {
		raw, err := os.ReadFile(filepath.Join(dir, "checksums.txt"))
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		return string(raw)
	}
	write := func(dir, name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	firstLine := func(dir string) string { return strings.SplitN(sums(dir), "\n", 2)[0] }

	cases := map[string]func(dir string){
		"no sums at all":            func(dir string) { _ = os.Remove(filepath.Join(dir, "checksums.txt")) },
		"an empty sums file":        func(dir string) { write(dir, "checksums.txt", "") },
		"a sums file that is prose": func(dir string) { write(dir, "checksums.txt", "not a checksum file\n") },
		"only a blank line":         func(dir string) { write(dir, "checksums.txt", "\n") },
		"a line short of a sum":     func(dir string) { write(dir, "checksums.txt", sums(dir)+"abc123  ballast_linux_amd64\n") },
		"capital letters in a sum": func(dir string) {
			write(dir, "checksums.txt", strings.Replace(sums(dir), firstLine(dir)[:8], strings.ToUpper(firstLine(dir)[:8]), 1))
		},
		"one space between sum and name": func(dir string) {
			write(dir, "checksums.txt", strings.Replace(sums(dir), "  ballast_", " ballast_", 1))
		},
		"a star before the name": func(dir string) {
			write(dir, "checksums.txt", strings.Replace(sums(dir), "  ballast_", " *ballast_", 1))
		},
		"line endings of Windows": func(dir string) {
			write(dir, "checksums.txt", strings.ReplaceAll(sums(dir), "\n", "\r\n"))
		},
		"a name with a path in it": func(dir string) {
			write(dir, "checksums.txt", strings.Replace(sums(dir), "  ballast_", "  ../ballast_", 1))
		},
		"a file that is not listed": func(dir string) {
			lines := strings.Split(strings.TrimSuffix(sums(dir), "\n"), "\n")
			write(dir, "checksums.txt", strings.Join(lines[1:], "\n")+"\n")
		},
		"a line for a file that is not there": func(dir string) {
			write(dir, "checksums.txt", sums(dir)+strings.Repeat("0", 64)+"  ballast_freebsd_amd64\n")
		},
		"the same file listed twice": func(dir string) {
			write(dir, "checksums.txt", sums(dir)+firstLine(dir)+"\n")
		},
		"a file that is not what was summed": func(dir string) {
			write(dir, platformFiles(t)[0], "another binary")
		},
		"a sum that is not the file's": func(dir string) {
			write(dir, "checksums.txt", strings.Replace(sums(dir), firstLine(dir)[:64], strings.Repeat("0", 64), 1))
		},
		"a file nobody asked for": func(dir string) { write(dir, "notes.txt", "x") },
		// Nothing but sums that list nothing: the lists agree, both are empty, and a tool
		// that answers "fine" to an empty file would say so too.
		"a directory of nothing but empty sums": func(dir string) {
			for _, name := range platformFiles(t) {
				_ = os.Remove(filepath.Join(dir, name))
			}
			write(dir, "checksums.txt", "")
		},
	}

	for name, spoil := range cases {
		t.Run(name, func(t *testing.T) {
			dir := aRelease(t)
			spoil(dir)

			if got := runVerify(t, dir); got.code == 0 {
				t.Errorf("a directory with %s verified:\n%s", name, sums(dir))
			}
		})
	}
}

func TestADirectoryThatIsNotThereDoesNotVerify(t *testing.T) {
	if got := runVerify(t, filepath.Join(t.TempDir(), "nowhere")); got.code == 0 {
		t.Error("it verified")
	}
	if got := runVerify(t, ""); got.code == 0 {
		t.Error("it verified with no directory at all")
	}
}

// A sums file whose last line has no line break is still a sums file: it is what a person
// gets from an editor that does not add one, and every tool reads it. The last line is not
// dropped because nothing follows it.
func TestSumsWithNoFinalLineBreakStillVerify(t *testing.T) {
	dir := aRelease(t)
	raw, err := os.ReadFile(filepath.Join(dir, "checksums.txt"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(strings.TrimSuffix(string(raw), "\n")), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if got := runVerify(t, dir); got.code != 0 {
		t.Errorf("exit %d:\n%s", got.code, got.out)
	}
}
