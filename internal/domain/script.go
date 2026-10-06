package domain

import (
	"errors"
	"fmt"
)

// ErrInvalidScript reports a scenario Ballast refuses to store. The report says
// why; callers match on the sentinel.
var ErrInvalidScript = errors.New("invalid scenario script")

// ScriptSeverity says whether a finding stops an import or only warns about it.
type ScriptSeverity string

const (
	// ScriptError refuses the script.
	ScriptError ScriptSeverity = "error"
	// ScriptWarning lets it through and says so.
	ScriptWarning ScriptSeverity = "warning"
)

// The codes a finding can carry. They are stable: a front end and a command
// line both key on them rather than on the wording.
const (
	// ScriptUnparsable is JavaScript k6 would not load either.
	ScriptUnparsable = "unparsable"
	// ScriptDeclaresOptions is a script exporting its own options. They would
	// not take effect — Ballast runs the scenario from a module of its own,
	// which is what makes the environment's ceilings binding — so accepting one
	// silently would let somebody believe their thresholds apply.
	ScriptDeclaresOptions = "declares_options"
	// ScriptNoBaseURL is a script that never reads __ENV.BASE_URL. A scenario
	// that does not read its target has chosen one itself.
	ScriptNoBaseURL = "no_base_url"
	// ScriptHardcodedURL is an absolute URL written into the script. It would
	// reach a host no environment ever vouched for, which is the control that
	// keeps Ballast from being aimed at somebody else's servers.
	ScriptHardcodedURL = "hardcoded_url"
	// ScriptNoRequestNameTags is a script whose requests carry no name tag.
	// Without them every endpoint lands in one bucket, and a threshold cannot
	// single one out.
	ScriptNoRequestNameTags = "no_request_name_tags"
)

// ScriptFinding is one thing an inspection noticed.
type ScriptFinding struct {
	Severity ScriptSeverity
	Code     string
	Message  string
	// Detail is what was found, when quoting it helps: the URL, the export.
	Detail string
	// Line is where, counting from one. Zero when the finding is about the
	// script as a whole rather than a place in it.
	Line int
}

func (f ScriptFinding) String() string {
	if f.Detail == "" {
		return f.Message
	}
	return fmt.Sprintf("%s: %s", f.Message, f.Detail)
}

// ScriptReport is everything an inspection noticed about one script.
type ScriptReport struct {
	Findings []ScriptFinding
}

// Errors are the findings that refuse the script.
func (r ScriptReport) Errors() []ScriptFinding {
	return r.bySeverity(ScriptError)
}

// Warnings are the findings that let it through.
func (r ScriptReport) Warnings() []ScriptFinding {
	return r.bySeverity(ScriptWarning)
}

// OK reports whether the script may be stored.
func (r ScriptReport) OK() bool {
	return len(r.Errors()) == 0
}

// Err is the refusal to return to a caller, or nil.
func (r ScriptReport) Err() error {
	refusals := r.Errors()
	if len(refusals) == 0 {
		return nil
	}

	message := refusals[0].String()
	if len(refusals) > 1 {
		message = fmt.Sprintf("%s (and %d more)", message, len(refusals)-1)
	}
	return fmt.Errorf("%w: %s", ErrInvalidScript, message)
}

func (r ScriptReport) bySeverity(severity ScriptSeverity) []ScriptFinding {
	var found []ScriptFinding
	for _, finding := range r.Findings {
		if finding.Severity == severity {
			found = append(found, finding)
		}
	}
	return found
}
