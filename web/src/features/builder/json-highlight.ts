// What a line of JSON is made of, for colouring it. It reads one line at a time
// and keeps no state across them, so a string that runs over two lines is left
// plain: the editor says that is a fault anyway.
export type Token = { text: string; kind: "key" | "string" | "number" | "literal" | "variable" | "punctuation" | "plain" };

const pattern =
  /(\{\{[^{}]*\}\})|("(?:[^"\\]|\\.)*"?)(\s*:)?|(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)|\b(true|false|null)\b|([{}[\],:])/g;

export function tokenizeJsonLine(line: string): Token[] {
  const tokens: Token[] = [];
  let last = 0;

  for (const match of line.matchAll(pattern)) {
    const at = match.index ?? 0;
    if (at > last) tokens.push({ text: line.slice(last, at), kind: "plain" });

    if (match[1] !== undefined) tokens.push({ text: match[1], kind: "variable" });
    else if (match[2] !== undefined) {
      // A string followed by a colon is a name.
      tokens.push({ text: match[2], kind: match[3] !== undefined ? "key" : "string" });
      if (match[3] !== undefined) tokens.push({ text: match[3], kind: "punctuation" });
    } else if (match[4] !== undefined) tokens.push({ text: match[4], kind: "number" });
    else if (match[5] !== undefined) tokens.push({ text: match[5], kind: "literal" });
    else tokens.push({ text: match[6], kind: "punctuation" });

    last = at + match[0].length;
  }

  if (last < line.length) tokens.push({ text: line.slice(last), kind: "plain" });
  return tokens;
}
