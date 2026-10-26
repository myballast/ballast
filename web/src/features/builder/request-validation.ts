import type { GraphNode } from "../scenarios/types.ts";

// What the panel can say about a request before the API does. The API is the
// judge — these are the same rules, said while somebody is still typing, next
// to the field they are about.

export const maxText = 2000;

export type Row = { k: string; v: string };

// A problem in the body, placed: the line and column of the first thing that is
// not JSON, so that it can be marked where it is.
export type JsonProblem = { message: string; line: number; column: number };

export type RequestProblems = {
  path?: string;
  headers: string[];
  query: string[];
  body?: JsonProblem;
  // A message about the body that has no place in it (too long).
  bodyText?: string;
};

const credentialHeaders = ["authorization", "proxy-authorization", "cookie", "set-cookie"];

export function isCredentialHeader(name: string): boolean {
  return credentialHeaders.includes(name.trim().toLowerCase());
}

const variable = /\{\{([^{}]*)\}\}/g;
const variableName = /^[A-Za-z_][A-Za-z0-9_]*$/;

// readsAVariable: a value with at least one well-formed {{name}} in it.
export function readsAVariable(value: string): boolean {
  for (const match of value.matchAll(variable)) if (variableName.test(match[1])) return true;
  return false;
}

// templateProblem checks the {{name}} syntax of a header value, a query value or a
// body: every {{ is closed and holds a usable name. Single braces are text.
export function templateProblem(text: string): string | undefined {
  let rest = text;
  for (;;) {
    const open = rest.indexOf("{{");
    if (open < 0) return undefined;
    const close = rest.indexOf("}}", open);
    if (close < 0) return "A {{ is never closed.";
    const name = rest.slice(open + 2, close);
    if (!variableName.test(name)) return `“${name}” is not a usable variable name.`;
    rest = rest.slice(close + 2);
  }
}

// pathProblem: the path starts with a slash, is not too long, and every {name} is
// a usable variable name.
export function pathProblem(path: string): string | undefined {
  if (!path.startsWith("/")) return "The path starts with a slash, as in /users/{id}.";
  if (path.length > maxText) return `The path is longer than ${maxText} characters.`;

  let rest = path;
  for (;;) {
    const open = rest.indexOf("{");
    if (open < 0) return undefined;
    const close = rest.indexOf("}", open);
    if (close < 0) return "A { is never closed.";
    const name = rest.slice(open + 1, close);
    if (!variableName.test(name)) return `“${name}” is not a usable variable name between braces.`;
    rest = rest.slice(close + 1);
  }
}

// rowProblems returns one message per row, in the order of the rows, empty where
// the row is fine. Names that are blank or repeated are the usual faults: a
// repeated header would silently keep one of its two values.
export function headerProblems(rows: Row[]): string[] {
  const seen = new Map<string, number>();

  return rows.map((row, index) => {
    const name = row.k.trim();
    if (name === "") return row.v === "" ? "" : "A header needs a name.";

    const key = name.toLowerCase();
    if (seen.has(key)) return `Already set by header ${(seen.get(key) as number) + 1}.`;
    seen.set(key, index);

    if (row.v.length > maxText) return `The value is longer than ${maxText} characters.`;
    const template = templateProblem(row.v);
    if (template) return template;
    if (isCredentialHeader(name) && !readsAVariable(row.v)) {
      return "A credential is read from a secret, never typed: use a variable, as in Bearer {{token}}.";
    }
    return "";
  });
}

export function queryProblems(rows: Row[]): string[] {
  return rows.map((row) => {
    if (row.k.trim() === "") return row.v === "" ? "" : "A parameter needs a name.";
    if (row.k.length > maxText || row.v.length > maxText) return `Longer than ${maxText} characters.`;
    return templateProblem(row.v) ?? "";
  });
}

// jsonProblem finds where a body stops being JSON. A {{name}} stands for a value
// the run fills in, so it is replaced, character for character, by digits: that
// keeps it valid wherever a value may stand, and every column where it was.
//
// The position comes from a reader written here and not from the engine's own
// error message, which says different things in different browsers and, in some,
// gives no position at all.
export function jsonProblem(text: string): JsonProblem | undefined {
  const neutral = text.replace(variable, (found) => "1".repeat(found.length));
  const found = scanJson(neutral);
  if (!found) return undefined;

  const at = Math.min(found.offset, text.length);
  const before = text.slice(0, at);
  return {
    message: found.message,
    line: before.split("\n").length,
    column: at - (before.lastIndexOf("\n") + 1) + 1,
  };
}

type Found = { message: string; offset: number };

class Stop extends Error {
  found: Found;

  constructor(found: Found) {
    super(found.message);
    this.found = found;
  }
}

// scanJson reads text as one JSON value, and says what is wrong and where, or
// nothing when it is JSON. Nesting is bounded so a body of brackets cannot
// exhaust the stack.
export function scanJson(text: string): Found | undefined {
  let i = 0;
  const fail = (message: string, at = i): never => {
    throw new Stop({ message, offset: at });
  };
  const skip = () => {
    while (i < text.length && " \t\r\n".includes(text[i])) i++;
  };
  const describe = () => (i >= text.length ? "the end of the text" : `“${text[i]}”`);

  function value(depth: number): void {
    if (depth > 100) fail("Nested too deeply.");
    skip();
    const c = text[i];

    if (c === "{") {
      i++;
      skip();
      if (text[i] === "}") return void i++;
      for (;;) {
        skip();
        if (text[i] !== '"') fail(`Expected a quoted name, found ${describe()}.`);
        string();
        skip();
        if (text[i] !== ":") fail(`Expected “:” after the name, found ${describe()}.`);
        i++;
        value(depth + 1);
        skip();
        if (text[i] === ",") {
          i++;
          continue;
        }
        if (text[i] === "}") return void i++;
        fail(`Expected “,” or “}” after the value, found ${describe()}.`);
      }
    }

    if (c === "[") {
      i++;
      skip();
      if (text[i] === "]") return void i++;
      for (;;) {
        value(depth + 1);
        skip();
        if (text[i] === ",") {
          i++;
          continue;
        }
        if (text[i] === "]") return void i++;
        fail(`Expected “,” or “]” after the value, found ${describe()}.`);
      }
    }

    if (c === '"') return string();

    const literal = /^(true|false|null)/.exec(text.slice(i));
    if (literal) return void (i += literal[0].length);

    const number = /^-?(0|[1-9]\d*)(\.\d+)?([eE][+-]?\d+)?/.exec(text.slice(i));
    if (number) return void (i += number[0].length);

    fail(i >= text.length ? "The text ends before the value does." : `Expected a value, found ${describe()}.`);
  }

  function string(): void {
    const start = i;
    i++;
    while (i < text.length) {
      const c = text[i];
      if (c === '"') return void i++;
      if (c === "\n") fail("A string cannot continue on the next line.", i);
      if (c === "\\") {
        i++;
        if (!/["\\/bfnrt]/.test(text[i] ?? "") && !/^u[0-9a-fA-F]{4}/.test(text.slice(i, i + 5))) {
          fail("Not a valid escape in a string.", i - 1);
        }
        if (text[i] === "u") i += 4;
      }
      i++;
    }
    fail("This string is never closed.", start);
  }

  try {
    value(0);
    skip();
    if (i < text.length) fail(`Unexpected ${describe()} after the end of the value.`);
    return undefined;
  } catch (error) {
    if (error instanceof Stop) return error.found;
    throw error;
  }
}

// isJson says whether the body is meant to be JSON: the Content-Type says so, or
// it looks like it. A form body or plain text is not held to the format.
export function isJson(body: string, headers: Row[]): boolean {
  const type = headers.find((row) => row.k.trim().toLowerCase() === "content-type")?.v.toLowerCase() ?? "";
  if (type.includes("json")) return true;
  if (type !== "") return false;
  const first = body.trimStart()[0];
  return first === "{" || first === "[";
}

// requestProblems is everything the panel flags on a request node.
export function requestProblems(node: GraphNode): RequestProblems {
  const headers: Row[] = Object.entries(node.headers ?? {}).map(([k, v]) => ({ k, v }));
  const query: Row[] = (node.query ?? []).map((q) => ({ k: q.name, v: q.value }));
  const body = node.body ?? "";

  const problems: RequestProblems = {
    path: pathProblem(node.path ?? ""),
    headers: headerProblems(headers),
    query: queryProblems(query),
  };

  if (body.length > maxText) problems.bodyText = `The body is longer than ${maxText} characters.`;
  else if (body !== "" && isJson(body, headers)) problems.body = jsonProblem(body);
  else if (body !== "") problems.bodyText = templateProblem(body);
  return problems;
}

export function hasProblems(problems: RequestProblems): boolean {
  return Boolean(
    problems.path || problems.body || problems.bodyText || problems.headers.some(Boolean) || problems.query.some(Boolean),
  );
}

// summary: the first thing wrong, in a few words, for the node on the canvas.
export function summaryOfProblems(problems: RequestProblems): string | undefined {
  if (problems.path) return "The path needs attention";
  if (problems.headers.some(Boolean)) return "A header needs attention";
  if (problems.query.some(Boolean)) return "A query parameter needs attention";
  if (problems.body) return `The body is not valid JSON (line ${problems.body.line})`;
  if (problems.bodyText) return "The body needs attention";
  return undefined;
}

const headerToken = /^[A-Za-z0-9!#$%&'*+.^_`|~-]+$/;

// extractProblem is what is wrong with an extraction, in a sentence, or nothing:
// the variable has a usable name, and the source has what it needs — a path in the
// body, the name of a header, and for the status nothing at all.
export function extractProblem(node: GraphNode): string | undefined {
  if (!variableName.test(node.variable ?? "")) return "The variable needs a name made of letters, digits and _, not starting with a digit.";
  if (node.variable === "response_status" || node.variable === "response_time_ms") {
    return `${node.variable} reads the last response, so no step may write it.`;
  }

  const from = (node.from ?? "").trim();
  switch (node.source ?? "body") {
    case "body":
      if (from.replace(/^\$\./, "") === "" || from === "$") return "Give a path in the body, such as data.token.";
      if (from.length > maxText) return `The path is longer than ${maxText} characters.`;
      return undefined;
    case "header":
      if (from === "") return "Give the name of a header, such as X-Request-Id.";
      if (!headerToken.test(from)) return "That is not a header name.";
      return undefined;
    default:
      return from === "" ? undefined : "The status takes no path.";
  }
}
