package cli

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"unicode"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Result is what a pipeline's next step can read about the run: the outputs of the
// step that ran the tool, and the summary of the job.
//
// Everything in it has been made safe to write into a file that another program
// reads line by line. A value is either one of a closed set of words, an address
// that passed a strict check, or empty: what the server says is never copied as it
// came.
type Result struct {
	// RunID is empty when no run was launched.
	RunID string
	// Status is the run's last known status, or empty when it is not one of the
	// statuses this version knows.
	Status string
	// Verdict is "met", "breached" or "none", and only for a run that finished with
	// an answer that agrees with itself. Empty for anything else: a run that did not
	// finish judged nothing, and an answer that contradicts itself is not passed on.
	Verdict string
	// URL is the run's page, or empty.
	URL string
	// ExitCode is the code the tool leaves with.
	ExitCode int
	// Thresholds are the rules and how they ended, with their text cleaned.
	Thresholds []Threshold
}

// maxSummaryRows bounds the table of thresholds in the summary.
const maxSummaryRows = 50

// knownVerdict reports whether a verdict is one of the three words.
func knownVerdict(verdict string) bool {
	switch verdict {
	case "met", "breached", "none":
		return true
	}
	return false
}

// safeStatus returns the status when it is one this version knows, else nothing.
func safeStatus(status string) string {
	if domain.RunStatus(status).Known() {
		return status
	}
	return ""
}

// safePageURL returns the address when it is a plain https address of a page, and
// nothing otherwise: no credentials, no query, no fragment, printable ASCII only.
// It is written into a line of a file and into a link, and a value that could be
// read as anything else would be written into both.
func safePageURL(address string) string {
	if address == "" || len(address) > 2048 {
		return ""
	}
	for _, r := range address {
		if r <= ' ' || r >= 0x7f {
			return ""
		}
	}
	parsed, err := url.Parse(address)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Hostname() == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" ||
		strings.Contains(address, "#") || strings.Contains(address, "?") {
		return ""
	}
	return address
}

// WriteGitHubOutputs appends the outputs of the step to the file GitHub names in
// GITHUB_OUTPUT. They are always the same five lines in the same order, one value
// each, so that nothing a server says can add a line.
func WriteGitHubOutputs(w io.Writer, r Result) error {
	_, err := fmt.Fprintf(w, "run-id=%s\nstatus=%s\nverdict=%s\nurl=%s\nexit-code=%d\n",
		singleLine(r.RunID), singleLine(r.Status), singleLine(r.Verdict), singleLine(r.URL), r.ExitCode)
	return err
}

// singleLine is the last defence of the outputs file: whatever reaches it, no line
// break does.
func singleLine(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || isInvisible(r) {
			return -1
		}
		return r
	}, s)
}

// WriteGitHubSummary appends a short account of the run to the file GitHub names in
// GITHUB_STEP_SUMMARY, which it shows as markdown on the page of the job.
//
// The metric names and rules come from the scenario's author, and not from this
// tool: none of it is allowed to be read as markdown or as HTML.
func WriteGitHubSummary(w io.Writer, r Result) error {
	var b strings.Builder

	switch {
	case r.Verdict != "":
		fmt.Fprintf(&b, "\n### Ballast run: %s\n\n", r.Verdict)
	case r.Status != "":
		fmt.Fprintf(&b, "\n### Ballast run: %s, no verdict\n\n", r.Status)
	default:
		b.WriteString("\n### Ballast run: no verdict\n\n")
	}

	if r.RunID != "" {
		fmt.Fprintf(&b, "Run `%s`", r.RunID)
		if r.Status != "" {
			fmt.Fprintf(&b, ", status %s", r.Status)
		}
		b.WriteString(".")
		if r.URL != "" {
			fmt.Fprintf(&b, " [Open the run](%s)", r.URL)
		}
		b.WriteString("\n\n")
	}

	if len(r.Thresholds) > 0 {
		b.WriteString("| Result | Metric | Rule |\n|---|---|---|\n")
		shown := r.Thresholds
		if len(shown) > maxSummaryRows {
			shown = shown[:maxSummaryRows]
		}
		for _, t := range shown {
			word := "met"
			if !t.Passed {
				word = "BREACHED"
			}
			fmt.Fprintf(&b, "| %s | %s | %s |\n", word, markdownText(t.Metric), markdownText(t.Expression))
		}
		if left := len(r.Thresholds) - len(shown); left > 0 {
			fmt.Fprintf(&b, "\nand %d more, on the page of the run.\n", left)
		}
	}

	_, err := io.WriteString(w, b.String())
	return err
}

// markdownText makes a piece of text show as itself: each character that markdown
// or HTML could read as something is escaped, and line breaks are gone.
func markdownText(text string) string {
	var b strings.Builder
	for _, r := range text {
		switch {
		case unicode.IsControl(r) || isInvisible(r):
			b.WriteRune(' ')
		case r == '<':
			b.WriteString("&lt;")
		case r == '>':
			b.WriteString("&gt;")
		case r == '&':
			b.WriteString("&amp;")
		case strings.ContainsRune("\\`*_[]()|~#!", r):
			b.WriteRune('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// githubReporter returns what writes the outputs and the summary to the files GitHub
// names, or nil when it names none. A file that cannot be written is a warning: the
// code the tool leaves with is what the pipeline acts on, and a full disk does not
// get to change it.
func githubReporter(env func(string) string, say func(string, ...any)) func(Result) {
	output := strings.TrimSpace(env("GITHUB_OUTPUT"))
	summary := strings.TrimSpace(env("GITHUB_STEP_SUMMARY"))
	if output == "" && summary == "" {
		return nil
	}

	return func(r Result) {
		if output != "" {
			if err := appendTo(output, func(w io.Writer) error { return WriteGitHubOutputs(w, r) }); err != nil {
				// The path is not repeated: it is somebody's, and the variable says enough.
				say("warning: could not write the outputs to the file GITHUB_OUTPUT names")
			}
		}
		if summary != "" {
			if err := appendTo(summary, func(w io.Writer) error { return WriteGitHubSummary(w, r) }); err != nil {
				say("warning: could not write the summary to the file GITHUB_STEP_SUMMARY names")
			}
		}
	}
}

// appendTo adds to a file and never replaces what is in it: other steps wrote there
// before this one, and write after it.
func appendTo(path string, write func(io.Writer) error) error {
	// The path is the one GitHub gives the step in the environment: naming the file is
	// what the variable is for.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600) //nolint:gosec // G304: the runner names the file on purpose
	if err != nil {
		return err
	}
	if err := write(f); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
