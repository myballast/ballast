import type { CatalogEndpoint } from "./types";

// safeName makes a name between braces usable as a JavaScript identifier, the
// way the API does when it builds a graph: {user-id} becomes user_id.
function safeName(name: string): string {
  const replaced = name.replace(/[^A-Za-z0-9_]/g, "_");
  return /^[0-9]/.test(replaced) ? `_${replaced}` : replaced;
}

// templatePath rewrites the names between braces and leaves the rest as it is.
// It is the text of the request's name tag, so that a threshold can name the
// endpoint without an identifier turning into its own metric.
export function templatePath(path: string): string {
  return path.replace(/\{([^}]*)\}/g, (_, name: string) => `{${safeName(name)}}`);
}

// literalPart escapes text that goes inside a template literal: a backtick or a
// ${ in a path would otherwise end the string or run code.
function literalPart(text: string): string {
  return text.replace(/\\/g, "\\\\").replace(/`/g, "\\`").replace(/\$/g, "\\$");
}

// urlExpression builds the address from BASE_URL and the path, with each
// {name} read from a variable of the same name, encoded as a path segment.
function urlExpression(path: string): string {
  const pieces = templatePath(path).split(/(\{[A-Za-z0-9_]*\})/);

  const inside = pieces
    .map((piece) => {
      const placeholder = /^\{([A-Za-z0-9_]*)\}$/.exec(piece);
      return placeholder ? `\${encodeURIComponent(${placeholder[1]})}` : literalPart(piece);
    })
    .join("");

  return `\`\${__ENV.BASE_URL}${inside}\``;
}

// oneLine keeps text from a specification on a single line, so that a summary
// cannot end the comment it is written in and put code after it.
function oneLine(text: string): string {
  return text.replace(/[\r\n\u2028\u2029]+/g, " ").trim();
}

// bodyExpression writes the body: JSON as an object passed through
// JSON.stringify, which reads like the script a person would write, and anything
// else as a string literal.
function bodyExpression(endpoint: CatalogEndpoint): string {
  if (endpoint.body === "") return "null";

  if (endpoint.content_type.includes("json")) {
    try {
      const parsed: unknown = JSON.parse(endpoint.body);
      return `JSON.stringify(${JSON.stringify(parsed, null, 2)})`;
    } catch {
      // Not JSON after all: fall through to a plain string.
    }
  }
  return JSON.stringify(endpoint.body);
}

// headerLines lists the headers of the request, with the content type among
// them, one per line.
function headerLines(endpoint: CatalogEndpoint): string[] {
  const headers: Record<string, string> = { ...endpoint.headers };
  if (endpoint.content_type !== "") headers["Content-Type"] = endpoint.content_type;

  return Object.keys(headers)
    .sort()
    .map((name) => `    ${JSON.stringify(name)}: ${JSON.stringify(headers[name])},`);
}

// requestSnippet writes the k6 call for an endpoint: the address from BASE_URL,
// the name tag Ballast's checks ask for, and the headers and example body the
// specification gave. A path parameter is declared as a variable with a marked
// placeholder, so that the script runs as soon as it is inserted and the value
// to replace is easy to see.
export function requestSnippet(endpoint: CatalogEndpoint): string {
  const lines: string[] = [];

  const title = oneLine(endpoint.summary) || `${endpoint.method} ${endpoint.path}`;
  lines.push(`// ${title}`);

  for (const name of endpoint.path_params) {
    lines.push(`const ${safeName(name)} = "1"; // TODO: a real value`);
  }

  const name = `${endpoint.method} ${templatePath(endpoint.path)}`;
  lines.push(
    `http.request(${JSON.stringify(endpoint.method)}, ${urlExpression(endpoint.path)}, ${bodyExpression(endpoint)}, {`,
  );

  const headers = headerLines(endpoint);
  if (headers.length > 0) {
    lines.push("  headers: {", ...headers, "  },");
  }
  lines.push(`  tags: { name: ${JSON.stringify(name)} },`, "});");

  return lines.join("\n") + "\n";
}

// insertAt puts the snippet where the cursor is, on lines of their own, indented
// like the line the cursor is on, and says where the cursor should land: just
// after it. A selection is replaced.
export function insertAt(
  text: string,
  selectionStart: number,
  selectionEnd: number,
  snippet: string,
): { text: string; cursor: number } {
  const beforeCursor = text.slice(0, selectionStart);
  const after = text.slice(selectionEnd);

  const lineStart = beforeCursor.lastIndexOf("\n") + 1;
  const onTheLine = beforeCursor.slice(lineStart);
  const indent = /^[ \t]*/.exec(onTheLine)?.[0] ?? "";

  // A cursor in the blank space at the start of a line writes over that space;
  // one in the middle of a line starts a line of its own.
  const startsALine = onTheLine.trim() === "";
  const before = startsALine ? beforeCursor.slice(0, lineStart) : beforeCursor + "\n";

  const indented = snippet
    .split("\n")
    .map((line) => (line === "" ? line : indent + line))
    .join("\n");

  return { text: before + indented + after, cursor: before.length + indented.length };
}
