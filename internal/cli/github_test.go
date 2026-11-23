package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// What the next step of a GitHub workflow can read: the outputs of the step that
// ran the tool, and the summary of the job.
//
// What these tests hold in place:
//
//   - the outputs are a closed set of five names, one line each, and what the
//     server says never becomes a sixth line: a line break in a value is how a step
//     output is forged, and a later step may act on it;
//   - what the server says about the verdict is passed on only when it is one of the
//     words this version knows, and a run whose answer contradicts itself has no
//     verdict at all;
//   - the summary shows text written by somebody else (a metric is named by the
//     scenario's author), so none of it is read as markdown or as HTML;
//   - the token is in neither file;
//   - a file that cannot be written is a warning: it never changes the code the
//     tool leaves with, because the code is what the pipeline acts on.
//
// Written before the code, which is why they do not compile yet.

// reported runs the command with the given report hook and returns what it reported.
func reported(t *testing.T, b *ballast, mutate func(*RunOptions, *Runner)) ([]Result, outcome) {
	t.Helper()

	var results []Result
	got := wholeRun(t, b, func(o *RunOptions, r *Runner) {
		r.Report = func(result Result) { results = append(results, result) }
		if mutate != nil {
			mutate(o, r)
		}
	})
	return results, got
}

func only(t *testing.T, results []Result) Result {
	t.Helper()
	if len(results) != 1 {
		t.Fatalf("the result was reported %d times, want exactly once", len(results))
	}
	return results[0]
}

// --- What is reported ---------------------------------------------------------------------------------

func TestARunThatPassedIsReportedWithItsVerdictAndItsPage(t *testing.T) {
	results, got := reported(t, &ballast{}, nil)

	r := only(t, results)
	if r.RunID != theRunID || r.Status != "passed" || r.Verdict != "met" || r.URL != thePageURL || r.ExitCode != 0 {
		t.Errorf("reported %+v, want the passed run, met, with its page and 0 (exit %d)", r, got.code)
	}
	if len(r.Thresholds) != 1 || r.Thresholds[0].Metric != "http_req_duration" || !r.Thresholds[0].Passed {
		t.Errorf("thresholds: %+v", r.Thresholds)
	}
}

func TestABreachIsReportedAsABreachEvenWhenTheCodeIsExcused(t *testing.T) {
	for name, excuse := range map[string]bool{"fails": false, "excused": true} {
		t.Run(name, func(t *testing.T) {
			results, _ := reported(t, &ballast{poll: pollsIn(state("failed", "breached", broken))},
				func(o *RunOptions, _ *Runner) { o.NoFailOnBreach = excuse })

			r := only(t, results)
			want := 1
			if excuse {
				want = 0
			}
			if r.Verdict != "breached" || r.Status != "failed" || r.ExitCode != want {
				t.Errorf("reported %+v, want breached, failed, exit %d", r, want)
			}
		})
	}
}

func TestARunThatJudgedNothingIsReportedAsNoneWhateverTheCode(t *testing.T) {
	for name, allow := range map[string]bool{"refused": false, "accepted": true} {
		t.Run(name, func(t *testing.T) {
			results, _ := reported(t, &ballast{poll: pollsIn(state("passed", "none"))},
				func(o *RunOptions, _ *Runner) { o.AllowNoThreshold = allow })

			r := only(t, results)
			want := 2
			if allow {
				want = 0
			}
			if r.Verdict != "none" || r.ExitCode != want {
				t.Errorf("reported %+v, want none and exit %d", r, want)
			}
		})
	}
}

func TestARunThatDidNotFinishWellHasNoVerdict(t *testing.T) {
	for _, status := range []string{"errored", "timeout", "cancelled", "rejected"} {
		t.Run(status, func(t *testing.T) {
			// The server may well send a verdict with it. It is not passed on:
			// nothing was judged by a run that did not finish.
			results, _ := reported(t, &ballast{poll: pollsIn(state(status, "met", held))}, nil)

			r := only(t, results)
			if r.Status != status || r.Verdict != "" || r.ExitCode != 2 || r.RunID != theRunID {
				t.Errorf("reported %+v, want %s, no verdict, exit 2", r, status)
			}
		})
	}
}

func TestAnAnswerThatContradictsItselfHasNoVerdict(t *testing.T) {
	for name, answer := range map[string]string{
		"passed and breached": state("passed", "breached", broken),
		"failed and met":      state("failed", "met", held),
		"passed, no verdict":  state("passed", ""),
		"unknown verdict":     state("passed", "great"),
	} {
		t.Run(name, func(t *testing.T) {
			results, got := reported(t, &ballast{poll: pollsIn(answer)}, nil)

			r := only(t, results)
			if r.Verdict != "" || r.ExitCode != 2 || got.code != 2 {
				t.Errorf("reported %+v (exit %d), want no verdict and 2", r, got.code)
			}
		})
	}
}

func TestARunTheToolGaveUpOnIsReportedWithItsIdAndNoVerdict(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var results []Result
	b := &ballast{poll: func(n int, w http.ResponseWriter) {
		if n == 2 {
			cancel()
		}
		writeJSON(w, 200, state("running", "none"))
	}}
	wholeRunCtx(ctx, t, b, func(_ *RunOptions, r *Runner) {
		r.Report = func(result Result) { results = append(results, result) }
	})

	r := only(t, results)
	if r.RunID != theRunID || r.Status != "running" || r.Verdict != "" || r.ExitCode != 2 {
		t.Errorf("reported %+v, want the run, still running, no verdict, exit 2", r)
	}
}

func TestALaunchThatWasRefusedIsReportedWithoutARun(t *testing.T) {
	results, _ := reported(t, &ballast{launch: func(w http.ResponseWriter) {
		writeJSON(w, 403, `{"error":"environment not verified"}`)
	}}, nil)

	r := only(t, results)
	if r.RunID != "" || r.Status != "" || r.Verdict != "" || r.ExitCode != 2 {
		t.Errorf("reported %+v, want no run and exit 2", r)
	}
}

func TestOptionsThatCannotBeRightAreReportedToo(t *testing.T) {
	results, _ := reported(t, &ballast{}, func(o *RunOptions, _ *Runner) { o.Profile = "not a profile" })

	if r := only(t, results); r.ExitCode != 2 || r.RunID != "" {
		t.Errorf("reported %+v", r)
	}
}

func TestWhatTheServerSaysIsMadeSafeBeforeItIsReported(t *testing.T) {
	hostile := rawState(map[string]any{
		"id": theRunID, "status": "passed", "verdict": "met",
		"url": "https://app.ballast.test/runs/x\nexit-code=0",
		"thresholds": []map[string]any{
			{"metric": "a\nb\x1b[31m" + aToken, "expression": "p(95)<500", "passed": true},
		},
	})
	results, _ := reported(t, &ballast{poll: func(_ int, w http.ResponseWriter) { writeJSON(w, 200, hostile) }}, nil)

	r := only(t, results)
	if r.URL != "" {
		t.Errorf("an address with a line break in it was passed on: %q", r.URL)
	}
	if len(r.Thresholds) != 1 {
		t.Fatalf("thresholds: %+v", r.Thresholds)
	}
	for _, text := range []string{r.Thresholds[0].Metric, r.Thresholds[0].Expression} {
		if strings.ContainsAny(text, "\n\r\x1b") || strings.Contains(text, aToken) {
			t.Errorf("a threshold was passed on unclean: %q", text)
		}
	}
}

func TestAStatusThatIsNotAKnownWordIsNotPassedOn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var results []Result
	hostile := rawState(map[string]any{"id": theRunID, "status": "running\nverdict=met", "verdict": "none", "url": thePageURL, "thresholds": []any{}})
	b := &ballast{poll: func(n int, w http.ResponseWriter) {
		if n == 2 {
			cancel()
		}
		writeJSON(w, 200, hostile)
	}}
	wholeRunCtx(ctx, t, b, func(_ *RunOptions, r *Runner) {
		r.Report = func(result Result) { results = append(results, result) }
	})

	if r := only(t, results); r.Status != "" {
		t.Errorf("a status that is not one of the known words was passed on: %q", r.Status)
	}
}

func TestOnlyAPlainAddressOfThePageIsPassedOn(t *testing.T) {
	for name, address := range map[string]string{
		"http":        "http://app.ballast.test/runs/x",
		"credentials": "https://user:pw@app.ballast.test/runs/x",
		"javascript":  "javascript:alert(1)",
		"query":       "https://app.ballast.test/runs/x?token=abc",
		"fragment":    "https://app.ballast.test/runs/x#a",
		"space":       "https://app.ballast.test/runs/x y",
		"no host":     "https:///runs/x",
		"non ascii":   "https://app.ballast.test/runs/é",
		"bidi":        "https://app.ballast.test/runs/\u202ex",
		"empty":       "",
	} {
		t.Run(name, func(t *testing.T) {
			body := rawState(map[string]any{"id": theRunID, "status": "passed", "verdict": "met", "url": address, "thresholds": []any{}})
			results, _ := reported(t, &ballast{poll: func(_ int, w http.ResponseWriter) { writeJSON(w, 200, body) }}, nil)

			if r := only(t, results); r.URL != "" {
				t.Errorf("%q was passed on as %q", address, r.URL)
			}
		})
	}
}

func rawState(fields map[string]any) string {
	b, _ := json.Marshal(fields)
	return string(b)
}

// --- GITHUB_OUTPUT ---------------------------------------------------------------------------------------

func outputsOf(t *testing.T, b *ballast, extra ...string) (path string, got invocation) {
	t.Helper()

	path = filepath.Join(t.TempDir(), "output")
	got = invoke(t, b, append([]string{"GITHUB_OUTPUT", path}, extra...), runArgs()...)
	return path, got
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestTheOutputsAreFiveLinesAndInThatOrder(t *testing.T) {
	path, got := outputsOf(t, &ballast{})

	want := "run-id=" + theRunID + "\n" +
		"status=passed\n" +
		"verdict=met\n" +
		"url=" + thePageURL + "\n" +
		"exit-code=0\n"
	if content := readFile(t, path); content != want {
		t.Errorf("GITHUB_OUTPUT is\n%q\nwant\n%q\n(exit %d: %s)", content, want, got.code, got.err)
	}
}

func TestTheOutputsAreAddedToWhatIsThereAndNotInsteadOfIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "output")
	if err := os.WriteFile(path, []byte("earlier=step\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	invoke(t, &ballast{}, []string{"GITHUB_OUTPUT", path}, runArgs()...)

	if content := readFile(t, path); !strings.HasPrefix(content, "earlier=step\n") || !strings.Contains(content, "verdict=met\n") {
		t.Errorf("GITHUB_OUTPUT is %q: the earlier line must stay, and ours follow it", content)
	}
}

func TestTheOutputsOfABreachCarryTheBreachAndTheCode(t *testing.T) {
	path, got := outputsOf(t, &ballast{poll: pollsIn(state("failed", "breached", broken))})

	content := readFile(t, path)
	if !strings.Contains(content, "verdict=breached\n") || !strings.Contains(content, "exit-code=1\n") || got.code != 1 {
		t.Errorf("exit %d, GITHUB_OUTPUT is %q", got.code, content)
	}
}

func TestTheOutputsOfARunThatDidNotFinishAreEmptyWhereNothingIsKnown(t *testing.T) {
	path, _ := outputsOf(t, &ballast{launch: func(w http.ResponseWriter) {
		writeJSON(w, 403, `{"error":"no"}`)
	}})

	want := "run-id=\nstatus=\nverdict=\nurl=\nexit-code=2\n"
	if content := readFile(t, path); content != want {
		t.Errorf("GITHUB_OUTPUT is %q, want %q", content, want)
	}
}

func TestWhatTheServerSaysCannotAddALineToTheOutputs(t *testing.T) {
	hostile := rawState(map[string]any{
		"id": theRunID, "status": "passed", "verdict": "breached\nexit-code=0",
		"url":        "https://app.ballast.test/runs/x\nexit-code=0",
		"thresholds": []any{},
	})
	path, _ := outputsOf(t, &ballast{poll: func(_ int, w http.ResponseWriter) { writeJSON(w, 200, hostile) }})

	lines := strings.Split(strings.TrimSuffix(readFile(t, path), "\n"), "\n")
	names := []string{"run-id", "status", "verdict", "url", "exit-code"}
	if len(lines) != len(names) {
		t.Fatalf("%d lines, want %d:\n%s", len(lines), len(names), strings.Join(lines, "\n"))
	}
	for i, line := range lines {
		if !strings.HasPrefix(line, names[i]+"=") {
			t.Errorf("line %d is %q, want it to begin %q", i, line, names[i]+"=")
		}
	}
	if !strings.Contains(strings.Join(lines, "\n"), "exit-code=2") {
		t.Errorf("the exit code is not the tool's own: %v", lines)
	}
}

func TestNothingIsWrittenWhenNoFileIsNamed(t *testing.T) {
	dir := t.TempDir()
	before, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(before) })

	got := invoke(t, &ballast{}, nil, runArgs()...)

	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 || got.code != 0 || strings.Contains(got.err, "GITHUB_") {
		t.Errorf("exit %d, files %v, stderr %q: with nothing named, nothing is written and nothing is said", got.code, entries, got.err)
	}
}

func TestAFileThatCannotBeWrittenIsAWarningAndNeverChangesTheCode(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no", "such", "dir", "out")
	got := invoke(t, &ballast{}, []string{"GITHUB_OUTPUT", missing, "GITHUB_STEP_SUMMARY", missing}, runArgs()...)

	if got.code != 0 {
		t.Errorf("exit %d: an output that cannot be written must not change the code", got.code)
	}
	for _, name := range []string{"GITHUB_OUTPUT", "GITHUB_STEP_SUMMARY"} {
		if !strings.Contains(got.err, name) {
			t.Errorf("stderr does not name %s:\n%s", name, got.err)
		}
	}
	if strings.Contains(got.err, missing) {
		t.Errorf("the path was repeated: %s", got.err)
	}
}

func TestAFileThatCannotBeWrittenDoesNotTurnABreachIntoSomethingElse(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no", "dir", "out")
	got := invoke(t, &ballast{poll: pollsIn(state("failed", "breached", broken))}, []string{"GITHUB_OUTPUT", missing}, runArgs()...)

	if got.code != 1 {
		t.Errorf("exit %d, want 1", got.code)
	}
}

func TestTheTokenIsInNeitherFile(t *testing.T) {
	body := rawState(map[string]any{
		"id": theRunID, "status": "passed", "verdict": "met", "url": thePageURL,
		"thresholds": []map[string]any{{"metric": "m " + aToken, "expression": "Authorization: Bearer " + aToken, "passed": true}},
	})
	dir := t.TempDir()
	output, summary := filepath.Join(dir, "output"), filepath.Join(dir, "summary")

	invoke(t, &ballast{poll: func(_ int, w http.ResponseWriter) { writeJSON(w, 200, body) }},
		[]string{"GITHUB_OUTPUT", output, "GITHUB_STEP_SUMMARY", summary}, runArgs()...)

	for _, path := range []string{output, summary} {
		if strings.Contains(readFile(t, path), aToken) {
			t.Errorf("%s holds the token", filepath.Base(path))
		}
	}
}

// --- GITHUB_STEP_SUMMARY ---------------------------------------------------------------------------------

func summaryOf(t *testing.T, b *ballast) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "summary")
	invoke(t, b, []string{"GITHUB_STEP_SUMMARY", path}, runArgs()...)
	return readFile(t, path)
}

func TestTheSummarySaysHowTheRunWentInWordsAndLinksToIt(t *testing.T) {
	got := summaryOf(t, &ballast{poll: pollsIn(state("failed", "breached", held, broken))})

	for _, want := range []string{"BREACHED", "met", markdownText("http_req_failed"), markdownText("http_req_duration"), thePageURL, theRunID} {
		if !strings.Contains(got, want) {
			t.Errorf("the summary lacks %q:\n%s", want, got)
		}
	}
}

func TestTheSummaryIsAddedToWhatIsThere(t *testing.T) {
	path := filepath.Join(t.TempDir(), "summary")
	if err := os.WriteFile(path, []byte("an earlier step\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	invoke(t, &ballast{}, []string{"GITHUB_STEP_SUMMARY", path}, runArgs()...)

	if content := readFile(t, path); !strings.HasPrefix(content, "an earlier step\n") || !strings.Contains(content, markdownText("http_req_duration")) {
		t.Errorf("the summary is %q", content)
	}
}

func TestTheSummaryOfARunWithoutAVerdictSaysWhy(t *testing.T) {
	got := summaryOf(t, &ballast{poll: pollsIn(stateWith("errored", "none", "the worker lost the container"))})

	if !strings.Contains(got, "errored") || !strings.Contains(got, "no verdict") {
		t.Errorf("the summary does not say the run ended without a verdict:\n%s", got)
	}
}

func TestTextWrittenByAnotherIsNeverReadAsMarkdownOrHTML(t *testing.T) {
	for name, text := range map[string]string{
		"link":      "[click](http://evil.test)",
		"image":     "![x](http://evil.test/p.png)",
		"html":      "<script>alert(1)</script>",
		"table":     "a | b |\n|---|",
		"heading":   "\n# big",
		"code":      "`rm -rf`",
		"emphasis":  "**bold** _it_",
		"autolink":  "<http://evil.test>",
		"entity":    "&lt;b&gt;",
		"backslash": `\|`,
	} {
		t.Run(name, func(t *testing.T) {
			body := rawState(map[string]any{
				"id": theRunID, "status": "passed", "verdict": "met", "url": thePageURL,
				"thresholds": []map[string]any{{"metric": text, "expression": text, "passed": true}},
			})
			got := summaryOf(t, &ballast{poll: func(_ int, w http.ResponseWriter) { writeJSON(w, 200, body) }})

			// The table is the part the text goes into; the link above it is ours.
			var rows []string
			for _, line := range strings.Split(got, "\n") {
				if strings.HasPrefix(line, "| met ") || strings.HasPrefix(line, "| BREACHED ") {
					rows = append(rows, line)
				}
			}
			if len(rows) != 1 {
				t.Fatalf("%d rows, want exactly 1 (a break in the text must not make another):\n%s", len(rows), got)
			}

			// What the text can do is checked on what it would be rendered from.
			for _, bad := range []string{"<script", "<http", "](http", "![", "<", ">"} {
				if strings.Contains(rows[0], bad) {
					t.Errorf("the row contains %q, which would be read as markup: %s", bad, rows[0])
				}
			}
			for i, r := range rows[0] {
				if r == '`' && (i == 0 || rows[0][i-1] != '\\') {
					t.Errorf("the row has a backtick that is not escaped: %s", rows[0])
				}
			}
			if cells(rows[0]) != 3 {
				t.Errorf("the row has %d cells, want 3: %q", cells(rows[0]), rows[0])
			}
		})
	}
}

// cells counts the cells of a table row, not counting an escaped bar.
func cells(row string) int {
	n := 0
	escaped := false
	for _, r := range row {
		switch {
		case escaped:
			escaped = false
		case r == '\\':
			escaped = true
		case r == '|':
			n++
		}
	}
	return n - 1
}

func TestAVeryLongListOfThresholdsIsCutAndSaysSo(t *testing.T) {
	var list []map[string]any
	for i := 0; i < 200; i++ {
		list = append(list, map[string]any{"metric": "metric_" + strings.Repeat("x", 3) + string(rune('a'+i%26)), "expression": "p(95)<500", "passed": true})
	}
	body := rawState(map[string]any{"id": theRunID, "status": "passed", "verdict": "met", "url": thePageURL, "thresholds": list})

	got := summaryOf(t, &ballast{poll: func(_ int, w http.ResponseWriter) { writeJSON(w, 200, body) }})

	if rows := strings.Count(got, "\n| met "); rows != maxSummaryRows {
		t.Errorf("%d rows, want %d", rows, maxSummaryRows)
	}
	if !strings.Contains(got, "150 more") {
		t.Errorf("the summary does not say how many were left out:\n%s", got)
	}
}
