// Enforces the colour charter (CLAUDE.md, "Charte graphique"):
//
//   - green, amber and red mean the status of a run or a threshold, and
//     nothing else, so the status tokens may only be used by the component
//     that shows a status;
//   - the one exception, the level of a line of a run's output, has tokens of its
//     own (log-error, log-warn) which only the component that draws a line may
//     use;
//   - no raw Tailwind red, green or amber anywhere, which would be a status
//     colour that escaped the tokens;
//   - no colour written in hex in a component: colours are tokens, set in
//     globals.css, or the charter cannot be held in one place;
//   - two font weights, 400 and 500, and no drop shadows (ticket T25).
//
// It runs as part of `npm run lint`. No dependency: it reads files and
// matches lines, and says where.

import { readdirSync, readFileSync } from "node:fs";
import { join, relative } from "node:path";

const root = new URL("..", import.meta.url).pathname;
const src = join(root, "src");

// The only file that may use the status tokens, besides globals.css which
// defines them.
const statusOwners = new Set(["src/components/status-badge.tsx", "src/styles/globals.css"]);

// The only file that may use the log-level tokens, besides globals.css.
const logOwners = new Set(["src/features/runs/components/log-line.tsx", "src/styles/globals.css"]);

// The only file that may use the syntax tokens of a JSON body, besides globals.css.
const syntaxOwners = new Set(["src/features/builder/components/json-editor.tsx", "src/styles/globals.css"]);

const rules = [
  {
    name: "JSON syntax colour outside the JSON editor",
    pattern: /\bsyntax-(key|string|number|variable)\b/,
    applies: (file) => !syntaxOwners.has(file),
    fix: "a JSON body is drawn by <JsonEditor> in features/builder/components/json-editor.tsx",
  },
  {
    name: "log-level colour outside a line of output",
    pattern: /\blog-(error|warn)\b/,
    applies: (file) => !logOwners.has(file),
    fix: "a line of a run's output is drawn by <LogLine> in features/runs/components/log-line.tsx",
  },
  {
    name: "status colour outside a status",
    pattern: /\bstatus-(passed|warning|failed)\b/,
    applies: (file) => !statusOwners.has(file),
    fix: "show a state with <StatusBadge> from components/status-badge.tsx",
  },
  {
    name: "raw Tailwind status hue",
    pattern:
      /\b(?:text|bg|border|ring|fill|stroke|outline|from|via|to|decoration|shadow|accent|caret|divide|placeholder)-(?:red|rose|green|emerald|lime|amber|yellow)-\d{2,3}\b/,
    applies: () => true,
    fix: "use a token from globals.css; a state goes through <StatusBadge>",
  },
  {
    name: "font weight other than 400 and 500",
    pattern: /\bfont-(?:thin|extralight|light|semibold|bold|extrabold|black)\b/,
    applies: () => true,
    fix: "use font-normal (400) or font-medium (500); only those two are loaded",
  },
  {
    name: "drop shadow",
    pattern: /\b(?:shadow|drop-shadow)(?:-(?:2xs|xs|sm|md|lg|xl|2xl|inner))?\b(?![-\w])/,
    applies: (file) => !file.startsWith("src/components/ui/"),
    fix: "the dark theme has no drop shadows; separate surfaces with a border",
  },
  {
    name: "colour written in hex in a component",
    pattern: /#[0-9a-fA-F]{3}(?:[0-9a-fA-F]{3}(?:[0-9a-fA-F]{2})?)?\b/,
    applies: (file) => /\.(tsx?|jsx?)$/.test(file) && file !== "src/styles/theme-colors.ts",
    fix: "add or reuse a token in globals.css",
  },
];

function* files(dir) {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) yield* files(path);
    else if (/\.(tsx?|jsx?|css)$/.test(entry.name)) yield path;
  }
}

const violations = [];
let checked = 0;

for (const path of files(src)) {
  checked++;
  const file = relative(root, path);
  const lines = readFileSync(path, "utf8").split("\n");

  lines.forEach((line, index) => {
    for (const rule of rules) {
      if (rule.applies(file) && rule.pattern.test(line)) {
        violations.push(`${file}:${index + 1}: ${rule.name} — ${rule.fix}\n    ${line.trim()}`);
      }
    }
  });
}

// On an empty tree every rule would hold and prove nothing.
if (checked === 0) {
  console.error("check-palette: no source file found under src/");
  process.exit(1);
}

if (violations.length > 0) {
  console.error(`check-palette: ${violations.length} violation(s) of the colour charter\n`);
  console.error(violations.join("\n"));
  process.exit(1);
}

console.log(`check-palette: ${checked} files follow the colour charter`);
