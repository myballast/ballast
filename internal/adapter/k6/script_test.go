package k6_test

import (
	"strings"
	"testing"

	"github.com/youssefmahersi/ballast/internal/adapter/k6"
	"github.com/youssefmahersi/ballast/internal/domain"
)

// A scenario that does what Ballast asks of it: reads its target from the
// environment, tags its requests, declares nothing of its own.
const goodScript = `
import http from 'k6/http';
import { check, group } from 'k6';

export function setup() {
	return { token: 'from-setup' };
}

export default function () {
	group('login', function () {
		const res = http.post(` + "`${__ENV.BASE_URL}/login`" + `, {}, { tags: { name: 'POST /login' } });
		check(res, { 'ok': (r) => r.status === 200 });
	});

	http.get(` + "`${__ENV.BASE_URL}/health`" + `, { tags: { name: 'GET /health' } });
}
`

func codes(report domain.ScriptReport) map[string]domain.ScriptSeverity {
	found := map[string]domain.ScriptSeverity{}
	for _, finding := range report.Findings {
		found[finding.Code] = finding.Severity
	}
	return found
}

func inspect(script string) domain.ScriptReport {
	return k6.NewScriptInspector().Inspect(script)
}

func TestAGoodScriptPasses(t *testing.T) {
	report := inspect(goodScript)

	if !report.OK() {
		t.Errorf("a well-formed scenario was refused: %v", report.Errors())
	}
	if len(report.Warnings()) != 0 {
		t.Errorf("a well-formed scenario drew warnings: %v", report.Warnings())
	}
	if report.Err() != nil {
		t.Errorf("Err() = %v, want nil", report.Err())
	}
}

// The refusal carried over from the runner: Ballast executes the scenario from a
// module of its own, so a script's own options are never consulted. Accepting
// one silently would let somebody believe their thresholds apply.
func TestOptionsAreRefusedHoweverTheyAreExported(t *testing.T) {
	cases := map[string]string{
		"export const": `
import http from 'k6/http';
export const options = { vus: 500, duration: '1h' };
export default function () { http.get(` + "`${__ENV.BASE_URL}/`" + `); }
`,
		"export let": `
import http from 'k6/http';
export let options = { vus: 500 };
export default function () { http.get(` + "`${__ENV.BASE_URL}/`" + `); }
`,
		"export var": `
import http from 'k6/http';
export var options = { vus: 500 };
export default function () { http.get(` + "`${__ENV.BASE_URL}/`" + `); }
`,
		// The spelling a pattern match would miss.
		"named export of a local": `
import http from 'k6/http';
const options = { vus: 500 };
export { options };
export default function () { http.get(` + "`${__ENV.BASE_URL}/`" + `); }
`,
		"renamed on the way out": `
import http from 'k6/http';
const settings = { vus: 500 };
export { settings as options };
export default function () { http.get(` + "`${__ENV.BASE_URL}/`" + `); }
`,
	}

	for name, script := range cases {
		t.Run(name, func(t *testing.T) {
			report := inspect(script)

			if report.OK() {
				t.Fatalf("the script was accepted: %+v", report.Findings)
			}
			if codes(report)[domain.ScriptDeclaresOptions] != domain.ScriptError {
				t.Errorf("findings: %+v", report.Findings)
			}
		})
	}
}

// A hardcoded host reaches somewhere no environment vouched for, which is the
// whole target-verification control walked around.
func TestAHardcodedURLIsRefused(t *testing.T) {
	cases := map[string]string{
		"a plain string": `
import http from 'k6/http';
export default function () {
	http.get(` + "`${__ENV.BASE_URL}/`" + `);
	http.post('https://somewhere.example.test/login');
}
`,
		"http rather than https": `
import http from 'k6/http';
export default function () {
	http.get(` + "`${__ENV.BASE_URL}/`" + `);
	http.get('http://somewhere.example.test/');
}
`,
		"a template that fixes the host": `
import http from 'k6/http';
export default function () {
	http.get(` + "`${__ENV.BASE_URL}/`" + `);
	http.get(` + "`https://somewhere.example.test/${__ENV.PATH}`" + `);
}
`,
	}

	for name, script := range cases {
		t.Run(name, func(t *testing.T) {
			report := inspect(script)

			if report.OK() {
				t.Fatalf("the script was accepted: %+v", report.Findings)
			}
			if codes(report)[domain.ScriptHardcodedURL] != domain.ScriptError {
				t.Errorf("findings: %+v", report.Findings)
			}
		})
	}
}

// The precision a pattern match cannot have: a comment is not code, and the tree
// does not hold it.
func TestAURLInACommentIsNotAFinding(t *testing.T) {
	script := `
import http from 'k6/http';

// Staging lives at https://staging.example.test — ask the platform team.
/* The old target was http://legacy.example.test/v1 */
export default function () {
	http.get(` + "`${__ENV.BASE_URL}/health`" + `);
}
`

	report := inspect(script)
	if !report.OK() {
		t.Errorf("a URL inside a comment was treated as code: %+v", report.Errors())
	}
}

// A relative path is the point: the host comes from the environment.
func TestARelativePathIsNotAFinding(t *testing.T) {
	script := `
import http from 'k6/http';
export default function () {
	http.get(__ENV.BASE_URL + '/v1/health');
}
`

	report := inspect(script)
	if !report.OK() {
		t.Errorf("a relative path was refused: %+v", report.Errors())
	}
}

func TestAScriptThatNeverReadsTheBaseURLIsRefused(t *testing.T) {
	script := `
import http from 'k6/http';
export default function () { http.get('/health'); }
`

	report := inspect(script)
	if report.OK() {
		t.Fatalf("the script was accepted: %+v", report.Findings)
	}
	if codes(report)[domain.ScriptNoBaseURL] != domain.ScriptError {
		t.Errorf("findings: %+v", report.Findings)
	}
}

// Both spellings reach the same variable.
func TestTheBaseURLCanBeReadEitherWay(t *testing.T) {
	cases := map[string]string{
		"dotted":  "http.get(__ENV.BASE_URL + '/health');",
		"indexed": "http.get(__ENV['BASE_URL'] + '/health');",
	}

	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			script := "import http from 'k6/http';\nexport default function () { " + call + " }\n"

			if report := inspect(script); !report.OK() {
				t.Errorf("findings: %+v", report.Errors())
			}
		})
	}
}

// Flagged, not refused: the criterion says a scenario without name tags is
// signalled, and a run of it is still a run worth having.
func TestMissingNameTagsAreAWarning(t *testing.T) {
	script := `
import http from 'k6/http';
export default function () {
	http.get(` + "`${__ENV.BASE_URL}/one`" + `);
	http.get(` + "`${__ENV.BASE_URL}/two`" + `);
	http.post(` + "`${__ENV.BASE_URL}/three`" + `, {});
}
`

	report := inspect(script)
	if !report.OK() {
		t.Fatalf("the script was refused rather than flagged: %+v", report.Errors())
	}
	if codes(report)[domain.ScriptNoRequestNameTags] != domain.ScriptWarning {
		t.Fatalf("findings: %+v", report.Findings)
	}
	if report.Err() != nil {
		t.Errorf("a warning produced an error: %v", report.Err())
	}

	// The message says how many requests go untagged, because that is what makes
	// it worth acting on.
	if !strings.Contains(report.Warnings()[0].Message, "3 requests") {
		t.Errorf("message: %q", report.Warnings()[0].Message)
	}
}

// One request needs no name: there is nothing to tell it apart from.
func TestASingleRequestNeedsNoName(t *testing.T) {
	script := `
import http from 'k6/http';
export default function () { http.get(` + "`${__ENV.BASE_URL}/health`" + `); }
`

	report := inspect(script)
	if len(report.Warnings()) != 0 {
		t.Errorf("a single request drew a warning: %+v", report.Warnings())
	}
}

func TestAnUnparsableScriptIsRefused(t *testing.T) {
	report := inspect("export default function ( {")

	if report.OK() {
		t.Fatal("broken JavaScript was accepted")
	}
	if codes(report)[domain.ScriptUnparsable] != domain.ScriptError {
		t.Errorf("findings: %+v", report.Findings)
	}
	// And nothing else is reported: there is no point saying what a script that
	// does not parse does or does not read.
	if len(report.Findings) != 1 {
		t.Errorf("an unparsable script produced %d findings", len(report.Findings))
	}
}

// Several faults are all reported, so a user fixes them in one pass rather than
// one request at a time.
func TestEveryFaultIsReportedAtOnce(t *testing.T) {
	script := `
import http from 'k6/http';
export const options = { vus: 500 };
export default function () {
	http.get('https://one.example.test/');
	http.get('https://two.example.test/');
}
`

	report := inspect(script)
	found := codes(report)

	for _, code := range []string{
		domain.ScriptDeclaresOptions,
		domain.ScriptHardcodedURL,
		domain.ScriptNoBaseURL,
	} {
		if found[code] != domain.ScriptError {
			t.Errorf("%s is missing from %+v", code, report.Findings)
		}
	}

	if err := report.Err(); err == nil {
		t.Error("Err() returned nothing")
	} else if !strings.Contains(err.Error(), "more") {
		t.Errorf("the refusal does not say there are others: %v", err)
	}
}
