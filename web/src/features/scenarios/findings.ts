import type { ScriptFinding } from "./types";

// What to do about each finding, in the words of whoever has to fix the script.
// Keyed on the code, which is stable; the API's own message is shown with it.
const advice: Record<string, string> = {
  hardcoded_url:
    "Build every address from `${__ENV.BASE_URL}` instead of writing a host. Ballast sends load only to the environment the run targets, and a host written in the script is one nobody verified.",
  no_base_url:
    "Read the target from `__ENV.BASE_URL`, for example `${__ENV.BASE_URL}/health` as the address of a request. Ballast sets it to the environment the run targets.",
  declares_options:
    "Remove `export const options`. Ballast writes the options from the scenario's profile and thresholds, and the script's own would not apply.",
  unparsable: "k6 could not read this script. Fix the JavaScript at the line shown.",
  no_request_name_tags:
    'Tag each request with a name, for example `{ tags: { name: "POST /login" } }`. Without a tag, k6 groups the metrics by URL: every distinct URL becomes its own series, the per-endpoint metrics become unusable, and a threshold cannot single out an endpoint.',
};

export function adviceFor(finding: ScriptFinding): string | undefined {
  return advice[finding.code];
}

// The lines a set of findings points at, for the editor's gutter.
export function linesOf(findings: ScriptFinding[]): Set<number> {
  return new Set(findings.flatMap((f) => (f.line ? [f.line] : [])));
}
