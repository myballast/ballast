// The grammar of a condition, as the API reads it (internal/domain/condition.go).
//
// A copy, and a deliberate one: the panel has to say that an expression is wrong
// while somebody is still typing it, not at the end when the save is refused. Two
// copies of one grammar drift apart unless they are asked the same questions, so
// both are tested against the same list (internal/domain/testdata/conditions.json).
// The API is still the judge; this only says it sooner.

export const responseStatus = "response_status";
export const responseTimeMs = "response_time_ms";

// The two names the last response owns: readable anywhere, writable nowhere.
export function isReserved(name: string): boolean {
  return name === responseStatus || name === responseTimeMs;
}

const maxLength = 500;
const comparisons = ["==", "!=", "<=", ">=", "<", ">"];
const operators = ["==", "!=", "<=", ">=", "&&", "||", "<", ">"];
const variableName = /^[A-Za-z_][A-Za-z0-9_]*$/;
const number = /^-?(\d+\.?\d*|\.\d+)$/;

export type Tree =
  | { kind: "or" | "and"; left: Tree; right: Tree }
  | { kind: "compare"; op: string; left: Tree; right: Tree }
  | { kind: "variable"; name: string }
  | { kind: "number"; value: number; text: string }
  | { kind: "string"; text: string };

export type Problem = { message: string; offset: number };

export type Parsed = { ok: true; tree: Tree; variables: string[] } | { ok: false; problem: Problem };

type Token = { text: string; at: number };

class Refusal extends Error {
  problem: Problem;

  constructor(message: string, offset: number) {
    super(message);
    this.problem = { message, offset };
  }
}

const isLetter = (c: string) => /\p{L}/u.test(c);
const isDigit = (c: string) => /\p{Nd}/u.test(c);
const isSpace = (c: string) => /\s/u.test(c);

function tokenize(text: string): Token[] {
  if (text.length > maxLength) throw new Refusal(`It is longer than ${maxLength} characters.`, maxLength);

  const chars = [...text];
  const tokens: Token[] = [];
  let at = 0;

  // Offsets are in characters of the text as typed, which is what a caret counts.
  const take = (keep: (c: string) => boolean) => {
    const start = at;
    while (at < chars.length && keep(chars[at])) at++;
    return chars.slice(start, at).join("");
  };

  while (at < chars.length) {
    const c = chars[at];
    const start = at;

    if (isSpace(c)) {
      at++;
    } else if (c === "(" || c === ")") {
      tokens.push({ text: c, at: start });
      at++;
    } else if (c === '"') {
      at++;
      let closed = false;
      while (at < chars.length) {
        const d = chars[at];
        if (d === "\\" || d === "\n" || d === "\r") {
          throw new Refusal("A text value may not contain a backslash or a line break.", at);
        }
        at++;
        if (d === '"') {
          closed = true;
          break;
        }
      }
      if (!closed) throw new Refusal("A text value is not closed.", start);
      tokens.push({ text: chars.slice(start, at).join(""), at: start });
    } else if (isDigit(c) || c === "-") {
      tokens.push({ text: take((d) => isDigit(d) || d === "." || d === "-"), at: start });
    } else if (isLetter(c) || c === "_") {
      tokens.push({ text: take((d) => isLetter(d) || isDigit(d) || d === "_"), at: start });
    } else {
      const rest = chars.slice(at).join("");
      const op = operators.find((o) => rest.startsWith(o));
      if (!op) throw new Refusal(`“${c}” is not part of the grammar.`, at);
      tokens.push({ text: op, at: start });
      at += op.length;
    }
  }
  return tokens;
}

class Parser {
  tokens: Token[];
  position = 0;
  end: number;

  constructor(tokens: Token[], end: number) {
    this.tokens = tokens;
    this.end = end;
  }

  next(): Token | undefined {
    return this.tokens[this.position];
  }

  takeIf(text: string): boolean {
    if (this.next()?.text === text) {
      this.position++;
      return true;
    }
    return false;
  }

  or(): Tree {
    let left = this.and();
    while (this.takeIf("||")) left = { kind: "or", left, right: this.and() };
    return left;
  }

  and(): Tree {
    let left = this.comparison();
    while (this.takeIf("&&")) left = { kind: "and", left, right: this.comparison() };
    return left;
  }

  comparison(): Tree {
    const left = this.operand();
    const current = this.next();
    if (!current || !comparisons.includes(current.text)) return left;
    this.position++;
    return { kind: "compare", op: current.text, left, right: this.operand() };
  }

  operand(): Tree {
    const token = this.next();
    if (!token) throw new Refusal("It stops in the middle.", this.end);
    this.position++;

    if (token.text === "(") {
      const inside = this.or();
      if (!this.takeIf(")")) throw new Refusal("A group is not closed.", this.next()?.at ?? this.end);
      return inside;
    }
    if (token.text.startsWith('"')) return { kind: "string", text: token.text.slice(1, -1) };
    if (number.test(token.text)) return { kind: "number", value: Number(token.text), text: token.text };
    if (variableName.test(token.text)) return { kind: "variable", name: token.text };
    throw new Refusal(`“${token.text}” is neither a variable, a number nor a text value.`, token.at);
  }
}

// parseCondition reads an expression, or says where it stops making sense.
export function parseCondition(text: string): Parsed {
  try {
    const tokens = tokenize(text);
    if (tokens.length === 0) return { ok: false, problem: { message: "It is empty.", offset: 0 } };

    const parser = new Parser(tokens, [...text].length);
    const tree = parser.or();
    const left = parser.next();
    if (left) throw new Refusal(`“${left.text}” was not expected here.`, left.at);

    return { ok: true, tree, variables: variablesOf(tree) };
  } catch (error) {
    if (error instanceof Refusal) return { ok: false, problem: error.problem };
    throw error;
  }
}

// variablesOf lists the names an expression reads, once each. The response's own
// two names are not among them: they need no step to have written them.
export function variablesOf(tree: Tree): string[] {
  const found: string[] = [];
  const walk = (node: Tree) => {
    if (node.kind === "variable") {
      if (!isReserved(node.name) && !found.includes(node.name)) found.push(node.name);
    } else if (node.kind === "or" || node.kind === "and" || node.kind === "compare") {
      walk(node.left);
      walk(node.right);
    }
  };
  walk(tree);
  return found;
}

// --- The guided form -------------------------------------------------------

export type Operator = "==" | "!=" | "<" | "<=" | ">" | ">=";

export const operatorLabels: Record<Operator, string> = {
  "==": "equals",
  "!=": "does not equal",
  "<": "is less than",
  "<=": "is at most",
  ">": "is more than",
  ">=": "is at least",
};

// A clause is one comparison: a variable, how it is compared, and to what. A bare
// variable (`ready`) is a clause with no operator — true when the variable is.
export type Clause = { variable: string; op: Operator | ""; value: string; text: boolean };

export type Guided = { join: "all" | "any"; clauses: Clause[] };

// toGuided reads an expression back into the form's rows, when it is made of
// comparisons all joined the same way. Mixed && and ||, or a group, stay as text:
// the priority of the two is a thing to read carefully, not to click through.
export function toGuided(text: string): Guided | null {
  const parsed = parseCondition(text);
  if (!parsed.ok) return null;

  const clauses: Clause[] = [];
  let join: "all" | "any" | null = null;

  const walk = (node: Tree): boolean => {
    if (node.kind === "and" || node.kind === "or") {
      const way = node.kind === "and" ? "all" : "any";
      if (join && join !== way) return false;
      join = way;
      return walk(node.left) && walk(node.right);
    }
    const clause = toClause(node);
    if (!clause) return false;
    clauses.push(clause);
    return true;
  };

  if (!walk(parsed.tree)) return null;
  return { join: join ?? "all", clauses };
}

function toClause(node: Tree): Clause | null {
  if (node.kind === "variable") return { variable: node.name, op: "", value: "", text: false };
  if (node.kind !== "compare" || node.left.kind !== "variable") return null;

  if (node.right.kind === "number") {
    return { variable: node.left.name, op: node.op as Operator, value: node.right.text, text: false };
  }
  if (node.right.kind === "string") {
    return { variable: node.left.name, op: node.op as Operator, value: node.right.text, text: true };
  }
  return null;
}

// fromGuided writes the rows as an expression.
export function fromGuided(guided: Guided): string {
  const joiner = guided.join === "all" ? " && " : " || ";
  return guided.clauses
    .map((c) => (c.op === "" ? c.variable : `${c.variable} ${c.op} ${c.text ? `"${c.value}"` : c.value}`))
    .join(joiner);
}
