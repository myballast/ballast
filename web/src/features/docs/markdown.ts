// A small reader for the markdown of the documentation pages.
//
// The pages are files of the repository, shown on GitHub and on the site, and this reads
// exactly what they use: headings, paragraphs, lists, tables, code, and inline code, bold
// and links. It makes a tree and nothing else. Nothing is turned into HTML: the tree is
// drawn by React, which writes text as text, and a link is followed only where safeHref
// says it may be.
//
// What it does not understand is shown as the text it is, never dropped.

export type Inline =
  | { kind: "text"; text: string }
  | { kind: "code"; text: string }
  | { kind: "bold"; children: Inline[] }
  | { kind: "link"; href: string; children: Inline[] };

export type Block =
  | { kind: "heading"; level: 1 | 2 | 3; id: string; children: Inline[] }
  | { kind: "paragraph"; children: Inline[] }
  | { kind: "code"; lang: string; text: string }
  | { kind: "list"; ordered: boolean; items: Inline[][] }
  | { kind: "table"; header: Inline[][]; rows: Inline[][][] };

// --- Inline ------------------------------------------------------------------------------------------

export function parseInline(source: string): Inline[] {
  const found: Inline[] = [];
  let plain = "";
  const flush = () => {
    if (plain !== "") found.push({ kind: "text", text: plain });
    plain = "";
  };

  let i = 0;
  while (i < source.length) {
    const c = source[i];

    if (c === "`") {
      const end = source.indexOf("`", i + 1);
      if (end > i + 1) {
        flush();
        found.push({ kind: "code", text: source.slice(i + 1, end) });
        i = end + 1;
        continue;
      }
    }

    if (c === "*" && source[i + 1] === "*") {
      const end = source.indexOf("**", i + 2);
      if (end > i + 2) {
        flush();
        found.push({ kind: "bold", children: parseInline(source.slice(i + 2, end)) });
        i = end + 2;
        continue;
      }
    }

    if (c === "[") {
      const middle = source.indexOf("](", i + 1);
      const end = middle < 0 ? -1 : source.indexOf(")", middle + 2);
      if (middle > i + 1 && end > middle + 2 && !source.slice(i + 1, middle).includes("[")) {
        flush();
        found.push({
          kind: "link",
          href: source.slice(middle + 2, end),
          children: parseInline(source.slice(i + 1, middle)),
        });
        i = end + 1;
        continue;
      }
    }

    plain += c;
    i++;
  }
  flush();
  return found;
}

function textOf(inlines: Inline[]): string {
  return inlines
    .map((inline) => (inline.kind === "bold" || inline.kind === "link" ? textOf(inline.children) : inline.text))
    .join("");
}

// headingId is the anchor GitHub gives a heading: lower case, only letters, digits,
// spaces and hyphens kept, spaces made hyphens.
export function headingId(text: string): string {
  return text
    .toLowerCase()
    .replace(/[^a-z0-9 -]/g, "")
    .replace(/ /g, "-");
}

// --- Where a link may go -------------------------------------------------------------------------------

const repositoryPages = "https://github.com/myballast/ballast/blob/main/docs/";

// safeHref returns where a link of a page may be followed to, or null when it may not: a
// place on the same page, another page of the documentation (which lives on GitHub), or an
// https address with no credentials in it. Anything else is shown as the text it is.
export function safeHref(href: string): string | null {
  if (/^#[a-z0-9-]+$/.test(href)) return href;

  const page = /^([a-z][a-z-]*\.md)(#[a-z0-9-]+)?$/.exec(href);
  if (page) return repositoryPages + page[1] + (page[2] ?? "");

  // Printable ASCII only, and no space: nothing that reads differently from what it is.
  if (!/^https:\/\/[\x21-\x7e]+$/.test(href)) return null;
  try {
    const url = new URL(href);
    if (url.protocol !== "https:" || url.hostname === "" || url.username !== "" || url.password !== "") return null;
  } catch {
    return null;
  }
  return href;
}

// --- Blocks --------------------------------------------------------------------------------------------

const fence = /^```([A-Za-z0-9_-]*)\s*$/;
const heading = /^(#{1,3}) +(.*?)\s*$/;
const bullet = /^- +(.*)$/;
const numbered = /^\d+\. +(.*)$/;
const separator = /^\|[\s:|-]*-[\s:|-]*\|$/;

function startsTable(lines: string[], i: number): boolean {
  return lines[i].startsWith("|") && i + 1 < lines.length && separator.test(lines[i + 1].trim());
}

function startsBlock(lines: string[], i: number): boolean {
  const line = lines[i];
  return fence.test(line) || heading.test(line) || bullet.test(line) || /^1\. +/.test(line) || startsTable(lines, i);
}

// cells splits a row of a table on its bars, except those inside code.
function cells(row: string): string[] {
  const found: string[] = [];
  let current = "";
  let inCode = false;
  for (const c of row.trim()) {
    if (c === "`") inCode = !inCode;
    if (c === "|" && !inCode) {
      found.push(current);
      current = "";
      continue;
    }
    current += c;
  }
  found.push(current);
  // A row begins and ends with a bar: what is outside them is nothing.
  return found.slice(1, -1).map((cell) => cell.trim());
}

export function parseMarkdown(source: string): Block[] {
  const lines = source.replace(/\r\n?/g, "\n").split("\n");
  const blocks: Block[] = [];

  let i = 0;
  while (i < lines.length) {
    const line = lines[i];

    if (line.trim() === "") {
      i++;
      continue;
    }

    const opening = fence.exec(line);
    if (opening) {
      const body: string[] = [];
      i++;
      while (i < lines.length && !/^```\s*$/.test(lines[i])) body.push(lines[i++]);
      i++; // the closing fence, or the end
      blocks.push({ kind: "code", lang: opening[1], text: body.join("\n") });
      continue;
    }

    const title = heading.exec(line);
    if (title) {
      const children = parseInline(title[2]);
      blocks.push({ kind: "heading", level: title[1].length as 1 | 2 | 3, id: headingId(textOf(children)), children });
      i++;
      continue;
    }

    if (startsTable(lines, i)) {
      const header = cells(lines[i]).map(parseInline);
      i += 2;
      const rows: Inline[][][] = [];
      while (i < lines.length && lines[i].startsWith("|")) {
        // Every row is as wide as the header: a table that is ragged is not a different table.
        const row = cells(lines[i]).map(parseInline);
        while (row.length < header.length) row.push([]);
        rows.push(row.slice(0, header.length));
        i++;
      }
      blocks.push({ kind: "table", header, rows });
      continue;
    }

    const first = bullet.exec(line) ?? numbered.exec(line);
    if (first) {
      const ordered = numbered.test(line);
      const marker = ordered ? numbered : bullet;
      const items: string[] = [];
      while (i < lines.length) {
        const item = marker.exec(lines[i]);
        if (item) {
          items.push(item[1]);
        } else if (/^\s+\S/.test(lines[i]) && items.length > 0) {
          items[items.length - 1] += " " + lines[i].trim();
        } else {
          break;
        }
        i++;
      }
      blocks.push({ kind: "list", ordered, items: items.map(parseInline) });
      continue;
    }

    if (line.startsWith("    ")) {
      const body: string[] = [];
      while (i < lines.length && (lines[i].startsWith("    ") || (lines[i].trim() === "" && (lines[i + 1] ?? "").startsWith("    ")))) {
        body.push(lines[i].slice(4));
        i++;
      }
      blocks.push({ kind: "code", lang: "", text: body.join("\n") });
      continue;
    }

    const paragraph = [line.trim()];
    i++;
    while (i < lines.length && lines[i].trim() !== "" && !startsBlock(lines, i)) paragraph.push(lines[i++].trim());
    blocks.push({ kind: "paragraph", children: parseInline(paragraph.join(" ")) });
  }
  return blocks;
}
