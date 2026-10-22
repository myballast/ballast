import assert from "node:assert/strict";
import { test } from "node:test";

import { insertAt, requestSnippet, templatePath } from "./snippet.ts";
import type { CatalogEndpoint } from "./types.ts";

function endpoint(overrides: Partial<CatalogEndpoint>): CatalogEndpoint {
  return {
    id: "1",
    source: "shop.yaml",
    method: "GET",
    path: "/health",
    tag: "",
    summary: "",
    path_params: [],
    headers: {},
    content_type: "",
    body: "",
    body_too_long: false,
    created_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

test("a plain GET reads BASE_URL and carries the name tag", () => {
  const snippet = requestSnippet(endpoint({}));

  assert.equal(
    snippet,
    [
      "// GET /health",
      'http.request("GET", `${__ENV.BASE_URL}/health`, null, {',
      '  tags: { name: "GET /health" },',
      "});",
      "",
    ].join("\n"),
  );
});

test("a path parameter becomes a declared variable, encoded in the address, and the tag keeps the template", () => {
  const snippet = requestSnippet(endpoint({ path: "/users/{id}", path_params: ["id"], summary: "Read one user" }));

  assert.match(snippet, /^\/\/ Read one user\n/);
  assert.match(snippet, /const id = "1"; \/\/ TODO/);
  assert.match(snippet, /`\$\{__ENV\.BASE_URL\}\/users\/\$\{encodeURIComponent\(id\)\}`/);
  assert.match(snippet, /tags: \{ name: "GET \/users\/\{id\}" \}/);
});

test("a name that is not an identifier is rewritten the way the graph import does", () => {
  assert.equal(templatePath("/users/{user-id}/pets/{1st}"), "/users/{user_id}/pets/{_1st}");

  const snippet = requestSnippet(endpoint({ path: "/users/{user-id}", path_params: ["user-id"] }));
  assert.match(snippet, /const user_id = "1"/);
  assert.match(snippet, /encodeURIComponent\(user_id\)/);
});

test("a JSON example is written as an object through JSON.stringify, with its content type", () => {
  const snippet = requestSnippet(
    endpoint({ method: "POST", path: "/orders", content_type: "application/json", body: '{"item":"thing","count":2}' }),
  );

  assert.match(snippet, /JSON\.stringify\(\{\n {2}"item": "thing",\n {2}"count": 2\n\}\)/);
  assert.match(snippet, /"Content-Type": "application\/json"/);
});

test("a body that is not JSON is written as a string literal", () => {
  const snippet = requestSnippet(endpoint({ method: "POST", path: "/x", content_type: "text/plain", body: 'a "b"\nc' }));

  assert.match(snippet, /, "a \\"b\\"\\nc", \{/);
});

test("headers are written sorted, as JSON strings", () => {
  const snippet = requestSnippet(endpoint({ headers: { "X-B": "2", "X-A": 'say "hi"' } }));

  assert.ok(snippet.indexOf('"X-A"') < snippet.indexOf('"X-B"'));
  assert.match(snippet, /"X-A": "say \\"hi\\""/);
});

// The catalog is filled from a file somebody else wrote, so what it holds can be
// hostile. Each of these would break out of the place it is written.
test("a path cannot close the template literal or run code inside it", () => {
  const snippet = requestSnippet(endpoint({ path: "/a`b$c\\d/{x}" }));

  // The backtick, the dollar and the backslash are escaped, so they stay text.
  assert.ok(snippet.includes("/a\\`b\\$c\\\\d/${encodeURIComponent(x)}`"), snippet);
});

test("something that looks like a call between braces becomes a harmless variable name, never a call", () => {
  const snippet = requestSnippet(endpoint({ path: "/a/${evil()}" }));

  const code = snippet.split("\n").slice(1).join("\n");
  assert.ok(!code.includes("evil()"), code);
  assert.ok(code.includes("encodeURIComponent(evil__)"), code);
});

test("a summary cannot end its comment and put code after it", () => {
  const snippet = requestSnippet(endpoint({ summary: "harmless\nfetch('http://evil.example')\u2028more" }));

  const lines = snippet.split("\n");
  assert.equal(lines[0], "// harmless fetch('http://evil.example') more");
  assert.ok(lines.slice(1).every((line) => !line.includes("evil.example")));
});

test("the snippet goes on lines of its own where the cursor is", () => {
  const snippet = "A\nB\n";

  assert.deepEqual(insertAt("", 0, 0, snippet), { text: "A\nB\n", cursor: 4 });
  assert.deepEqual(insertAt("x\n", 2, 2, snippet), { text: "x\nA\nB\n", cursor: 6 });
  assert.deepEqual(insertAt("xy", 1, 1, snippet), { text: "x\nA\nB\ny", cursor: 6 });
});

test("the snippet takes the indentation of the line the cursor is on", () => {
  // Cursor in the blank space of an indented line: that space is written over.
  assert.deepEqual(insertAt("f() {\n  \n}", 8, 8, "A\nB\n"), { text: "f() {\n  A\n  B\n\n}", cursor: 14 });
  // Cursor after some code on an indented line: a new line, same indentation.
  assert.deepEqual(insertAt("  x = 1;", 8, 8, "A\n"), { text: "  x = 1;\n  A\n", cursor: 13 });
});

test("a selection is replaced", () => {
  assert.deepEqual(insertAt("one two three", 4, 7, "S\n"), { text: "one \nS\n three", cursor: 7 });
});
