import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { headingId, parseMarkdown, safeHref, type Block, type Inline } from "./markdown.ts";

// The documentation pages are markdown files that the repository shows on GitHub and the
// site shows at /docs. The site reads them with this small reader, which understands
// exactly what those pages use, and nothing else.
//
// What these tests hold in place:
//   - what a page uses is understood: every heading, table, list and code block of the
//     real page comes out as one, and no marker of markdown is left in the text;
//   - the page is text from a file in the repository, but it is still never trusted to be
//     markup: nothing is turned into HTML, and a link goes where a link may go;
//   - the anchors of the headings are GitHub's, so the links inside a page keep working.

function text(inlines: Inline[]): string {
  return inlines
    .map((i) => (i.kind === "link" || i.kind === "bold" ? text(i.children) : i.text))
    .join("");
}

test("a heading has the level it was written with and the anchor GitHub gives it", () => {
  const [h1, h2, h3] = parseMarkdown("# One\n\n## 5. Read the verdict\n\n### Fine, `code` & more\n");

  assert.deepEqual([h1.kind, h2.kind, h3.kind], ["heading", "heading", "heading"]);
  assert.equal((h2 as Extract<Block, { kind: "heading" }>).level, 2);
  assert.equal((h2 as Extract<Block, { kind: "heading" }>).id, "5-read-the-verdict");
  assert.equal((h3 as Extract<Block, { kind: "heading" }>).id, "fine-code--more");
});

test("the anchor is what GitHub makes of a heading", () => {
  assert.equal(headingId("Hosting Ballast yourself"), "hosting-ballast-yourself");
  assert.equal(headingId("1. A scenario with a threshold"), "1-a-scenario-with-a-threshold");
  assert.equal(headingId("What, exactly?"), "what-exactly");
});

test("lines of a paragraph are one paragraph", () => {
  const blocks = parseMarkdown("first line\nsecond line\n\nanother\n");

  assert.equal(blocks.length, 2);
  assert.equal(blocks[0].kind, "paragraph");
  assert.equal(text((blocks[0] as Extract<Block, { kind: "paragraph" }>).children), "first line second line");
});

test("inline code, bold and links are read, and what is inside code is left alone", () => {
  const [p] = parseMarkdown("Use `a*b` and **bold `x`** and [the page](https://example.com/x) now, and `[no](link)`.\n");
  const inlines = (p as Extract<Block, { kind: "paragraph" }>).children;

  assert.deepEqual(
    inlines.map((i) => i.kind),
    ["text", "code", "text", "bold", "text", "link", "text", "code", "text"],
  );
  assert.equal((inlines[1] as { text: string }).text, "a*b");
  assert.equal((inlines[7] as { text: string }).text, "[no](link)");
});

test("a marker that never closes is only text", () => {
  const [p] = parseMarkdown("2 ** 3 and a ` tick and [not a link] and (x)\n");

  assert.equal(text((p as Extract<Block, { kind: "paragraph" }>).children), "2 ** 3 and a ` tick and [not a link] and (x)");
});

test("an indented block and a fenced block are code, and are kept as they were written", () => {
  const blocks = parseMarkdown("text\n\n    printf '%s' '<x>'\n      indented more\n\n```yaml\nname: x\n  on: y\n```\n");
  const [, indented, fenced] = blocks as Extract<Block, { kind: "code" }>[];

  assert.equal(indented.kind, "code");
  assert.equal(indented.text, "printf '%s' '<x>'\n  indented more");
  assert.equal(fenced.lang, "yaml");
  assert.equal(fenced.text, "name: x\n  on: y");
});

test("a list keeps its items, and a continuation line is part of its item", () => {
  const [list] = parseMarkdown("- one\n  more of one\n- two with `code`\n\n1. first\n2. second\n") as Extract<Block, { kind: "list" }>[];

  assert.equal(list.ordered, false);
  assert.deepEqual(list.items.map(text), ["one more of one", "two with code"]);

  const second = parseMarkdown("1. first\n2. second\n")[0] as Extract<Block, { kind: "list" }>;
  assert.equal(second.ordered, true);
  assert.equal(second.items.length, 2);
});

test("a table has a header and rows of the same width, and a bar inside code is not a cell", () => {
  const [table] = parseMarkdown("| A | B |\n|---|---|\n| `x|y` | two |\n| three | four |\n") as Extract<Block, { kind: "table" }>[];

  assert.equal(table.kind, "table");
  assert.deepEqual(table.header.map(text), ["A", "B"]);
  assert.equal(table.rows.length, 2);
  assert.deepEqual(table.rows[0].map(text), ["x|y", "two"]);
});

test("a row that is the wrong width is padded or cut to the header, never a different table", () => {
  const [table] = parseMarkdown("| A | B |\n|---|---|\n| only |\n| a | b | c |\n") as Extract<Block, { kind: "table" }>[];

  assert.deepEqual(table.rows.map((r) => r.length), [2, 2]);
});

test("html in the text is text", () => {
  const blocks = parseMarkdown("<script>alert(1)</script> and <img src=x onerror=y>\n");

  assert.equal(blocks[0].kind, "paragraph");
  assert.equal(text((blocks[0] as Extract<Block, { kind: "paragraph" }>).children), "<script>alert(1)</script> and <img src=x onerror=y>");
});

// --- Where a link may go ----------------------------------------------------------------------------

test("a link goes to a page of the page, to a page of the repository, or to https", () => {
  assert.equal(safeHref("#hosting-ballast-yourself"), "#hosting-ballast-yourself");
  assert.equal(safeHref("cli.md"), "https://github.com/myballast/ballast/blob/main/docs/cli.md");
  assert.equal(safeHref("cli.md#install"), "https://github.com/myballast/ballast/blob/main/docs/cli.md#install");
  assert.equal(safeHref("https://github.com/myballast/ballast-action#outputs"), "https://github.com/myballast/ballast-action#outputs");
});

test("anything else is not a link", () => {
  for (const href of [
    "javascript:alert(1)", "JaVaScRiPt:alert(1)", "data:text/html,x", "http://example.com", "//example.com",
    "https://user:pw@example.com/", "https://example.com/a b", "../secrets.md", "/etc/passwd", "cli.md/../x",
    "x.md?y=1", "", " https://example.com", "https://exa\nmple.com", "file:///etc/passwd", "vbscript:x",
  ]) {
    assert.equal(safeHref(href), null, `${JSON.stringify(href)} was accepted`);
  }
});

// --- The real page -----------------------------------------------------------------------------------------

const page = readFileSync(new URL("./content/ci.md", import.meta.url), "utf8");

test("every heading of the real page is a heading, in order", () => {
  const written = page.split("\n").filter((l) => /^#{1,3} /.test(l)).map((l) => l.replace(/^#+ /, ""));
  const read = parseMarkdown(page).filter((b) => b.kind === "heading").map((b) => text((b as Extract<Block, { kind: "heading" }>).children));

  assert.deepEqual(read, written);
  assert.ok(written.length >= 10, "the page has the headings it should");
});

test("every table, fenced block and list of the real page is one", () => {
  const blocks = parseMarkdown(page);
  const count = (kind: string) => blocks.filter((b) => b.kind === kind).length;

  assert.equal(count("table"), (page.match(/^\|[-| ]*-[-| ]*\|$/gm) ?? []).length);
  assert.equal(blocks.filter((b) => b.kind === "code" && b.lang === "yaml").length, (page.match(/^```yaml$/gm) ?? []).length);
  assert.ok(count("list") >= 3);
});

test("no marker of markdown is left in what the real page shows", () => {
  const shown: string[] = [];
  for (const block of parseMarkdown(page)) {
    // A heading may begin with its number, "1. A scenario": that is its text.
    if (block.kind === "heading") shown.push(text(block.children).replace(/^\d+\. /, ""));
    if (block.kind === "paragraph") shown.push(text(block.children));
    if (block.kind === "list") block.items.forEach((i) => shown.push(text(i)));
    if (block.kind === "table") [block.header, ...block.rows].forEach((r) => r.forEach((c) => shown.push(text(c))));
  }

  for (const line of shown) {
    assert.doesNotMatch(line, /\*\*|`|\]\(|^#+ |^- |^\d+\. |^\|/, `a marker is left in: ${line}`);
  }
});

test("every link of the real page is one that may be followed, and every anchor is a heading", () => {
  const ids = new Set(parseMarkdown(page).filter((b) => b.kind === "heading").map((b) => (b as Extract<Block, { kind: "heading" }>).id));

  for (const [, href] of page.matchAll(/\]\(([^)]+)\)/g)) {
    assert.notEqual(safeHref(href), null, `${href} is not a link that may be followed`);
    if (href.startsWith("#")) assert.ok(ids.has(href.slice(1)), `${href} points at no heading`);
  }
});

test("the real page shows nothing as code that is not code", () => {
  const code = parseMarkdown(page).filter((b) => b.kind === "code");

  assert.ok(code.length >= 3, "the printf line, the workflow and the log are code");
  for (const block of code) {
    assert.doesNotMatch((block as { text: string }).text, /^```/m);
  }
});

test("an anchor is a lower-case id and nothing else", () => {
  for (const href of ["#", "#A", "#a b", '#x"', "#x/y", "#x?y", "#é", "##x"]) {
    assert.equal(safeHref(href), null, `${JSON.stringify(href)} was accepted`);
  }
});

test("a hyphen stays in an anchor", () => {
  assert.equal(headingId("Pre-release notes"), "pre-release-notes");
});

test("empty bold is only text, and a bracket inside a label starts a link of its own", () => {
  const [empty] = parseMarkdown("a **** b\n");
  assert.equal(text((empty as Extract<Block, { kind: "paragraph" }>).children), "a **** b");

  const [nested] = parseMarkdown("[a [b](https://example.com/x)\n");
  const kinds = (nested as Extract<Block, { kind: "paragraph" }>).children.map((i) => i.kind);
  assert.deepEqual(kinds, ["text", "link"]);
});
