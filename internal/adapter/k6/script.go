package k6

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/js"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// ScriptInspector reads a scenario without running it.
//
// The script is parsed, not searched. A pattern match reports a URL written in a
// comment and misses one built by joining two strings; the syntax tree holds
// neither comments nor the illusion that a concatenation is a literal. It also
// makes the refusal of an exported options block exact, which matters: that
// refusal is the one thing standing between a user believing their thresholds
// apply and finding out after a run that they never did.
//
// Nothing is executed. A k6 script is code somebody uploaded.
type ScriptInspector struct{}

// NewScriptInspector returns an inspector. It holds no state.
func NewScriptInspector() ScriptInspector {
	return ScriptInspector{}
}

// absoluteURL matches a string that addresses a host of its own choosing.
var absoluteURL = regexp.MustCompile(`^(?i)https?://`)

// The k6 module that makes requests, and the names it answers to.
const httpModule = "http"

var requestMethods = map[string]bool{
	"get": true, "post": true, "put": true, "patch": true, "head": true,
	"del": true, "options": true, "request": true, "asyncRequest": true, "batch": true,
}

// Inspect reports what is wrong with a script, and what is only worth saying.
func (ScriptInspector) Inspect(script string) domain.ScriptReport {
	input := parse.NewInputString(script)

	tree, err := js.Parse(input, js.Options{})
	if err != nil {
		// k6 would not load it either, so there is nothing further to say about
		// what it does or does not read.
		return domain.ScriptReport{Findings: []domain.ScriptFinding{{
			Severity: domain.ScriptError,
			Code:     domain.ScriptUnparsable,
			Message:  "the script is not valid JavaScript",
			Detail:   err.Error(),
			Line:     lineOf(err),
		}}}
	}

	reader := &scriptReader{script: script}
	js.Walk(reader, tree)

	return reader.report()
}

// scriptReader collects what matters while walking the tree once.
type scriptReader struct {
	script string

	optionsExport bool
	readsBaseURL  bool
	requests      int
	nameTags      int
	hardcoded     []string
}

func (r *scriptReader) Exit(js.INode) {}

func (r *scriptReader) Enter(node js.INode) js.IVisitor {
	switch n := node.(type) {
	case *js.ExportStmt:
		r.readExport(n)

	case *js.LiteralExpr:
		if n.TokenType == js.StringToken {
			r.readString(unquote(n.Data))
		}

	case *js.TemplateExpr:
		// Only the part before the first substitution can fix the host. A
		// template that starts with ${__ENV.BASE_URL} begins with nothing.
		if len(n.List) > 0 {
			r.readString(templateText(n.List[0].Value))
		} else {
			r.readString(templateText(n.Tail))
		}

	case *js.DotExpr:
		if name(n.X) == "__ENV" && name(n.Y) == "BASE_URL" {
			r.readsBaseURL = true
		}
		if name(n.X) == httpModule && requestMethods[name(n.Y)] {
			r.requests++
		}

	case *js.IndexExpr:
		// __ENV['BASE_URL'] reads the same thing by another spelling.
		if name(n.X) == "__ENV" {
			if literal, ok := n.Y.(*js.LiteralExpr); ok && unquote(literal.Data) == "BASE_URL" {
				r.readsBaseURL = true
			}
		}

	case *js.Property:
		if n.Name != nil && propertyName(n.Name) == "name" {
			r.nameTags++
		}
	}

	return r
}

func (r *scriptReader) readExport(stmt *js.ExportStmt) {
	// export { options } and export { settings as options }. Binding is the
	// name the module exports; Name is what it was called inside. It is the
	// exported one that k6 would read, so it is the one that matters.
	for _, alias := range stmt.List {
		if strings.TrimSpace(string(alias.Binding)) == "options" {
			r.optionsExport = true
		}
	}

	// export const options = {...} and export let/var.
	if stmt.Decl != nil && bytes.Contains([]byte(stmt.Decl.String()), []byte("Binding(options")) {
		r.optionsExport = true
	}
}

func (r *scriptReader) readString(value string) {
	if absoluteURL.MatchString(value) {
		r.hardcoded = append(r.hardcoded, value)
	}
}

func (r *scriptReader) report() domain.ScriptReport {
	var report domain.ScriptReport

	if r.optionsExport {
		report.Findings = append(report.Findings, domain.ScriptFinding{
			Severity: domain.ScriptError,
			Code:     domain.ScriptDeclaresOptions,
			Message: "the script exports its own options; Ballast decides the load, " +
				"so these would be ignored rather than applied",
			Line: r.lineOf("options"),
		})
	}

	for _, url := range r.hardcoded {
		report.Findings = append(report.Findings, domain.ScriptFinding{
			Severity: domain.ScriptError,
			Code:     domain.ScriptHardcodedURL,
			Message:  "the script addresses a host of its own instead of reading __ENV.BASE_URL",
			Detail:   url,
			Line:     r.lineOf(url),
		})
	}

	if !r.readsBaseURL {
		report.Findings = append(report.Findings, domain.ScriptFinding{
			Severity: domain.ScriptError,
			Code:     domain.ScriptNoBaseURL,
			Message: "the script never reads __ENV.BASE_URL, so it does not target " +
				"the environment it is pointed at",
		})
	}

	// Only worth saying when there is something to tag. A script that makes one
	// request does not need to name it.
	if r.requests > 1 && r.nameTags == 0 {
		report.Findings = append(report.Findings, domain.ScriptFinding{
			Severity: domain.ScriptWarning,
			Code:     domain.ScriptNoRequestNameTags,
			Message: fmt.Sprintf(
				"the script makes %d requests and tags none of them with a name, "+
					"so every endpoint will be measured as one", r.requests),
		})
	}

	return report
}

// lineOf finds where a piece of text sits, counting from one. It is for the
// message only: a finding without a line is still a finding.
func (r *scriptReader) lineOf(needle string) int {
	index := strings.Index(r.script, needle)
	if index < 0 {
		return 0
	}
	return strings.Count(r.script[:index], "\n") + 1
}

// name reads an identifier.
//
// The tree wraps expressions in ways that vary with their position, so the
// printed form is used as a fallback and stripped of the parentheses and quotes
// that wrapping adds. What is being asked is only whether a name is this name.
func name(expr js.IExpr) string {
	switch n := expr.(type) {
	case nil:
		return ""
	case *js.Var:
		return string(n.Name())
	case *js.LiteralExpr:
		return strings.Trim(string(n.Data), `'"`)
	}
	return strings.Trim(expr.String(), `()'"`)
}

func propertyName(n *js.PropertyName) string {
	if n == nil {
		return ""
	}
	if !n.IsComputed() {
		return strings.Trim(n.String(), `'"`)
	}
	return ""
}

// unquote strips the quotes a string literal carries in the source.
func unquote(data []byte) string {
	return strings.Trim(string(data), `'"`+"`")
}

func templateText(data []byte) string {
	// A template part arrives with its backtick or ${ delimiters.
	return strings.TrimLeft(strings.Trim(string(data), "`"), "}")
}

// lineOf pulls the line out of a parse error, which reports it in its message.
func lineOf(err error) int {
	var parseErr *parse.Error
	if ok := asParseError(err, &parseErr); ok {
		_, line, _ := parseErr.Position()
		return line
	}
	return 0
}

func asParseError(err error, target **parse.Error) bool {
	if parseErr, ok := err.(*parse.Error); ok {
		*target = parseErr
		return true
	}
	return false
}
