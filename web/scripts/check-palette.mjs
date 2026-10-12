// Enforces the colour charter (CLAUDE.md, "Charte graphique"):
//
//   - green, amber and red mean the status of a run or a threshold, and
//     nothing else, so the status tokens may only be used by the component
//     that shows a status;
//   - no raw Tailwind red, green or amber anywhere, which would be a status
//     colour that escaped the tokens;
//   - no colour written in hex in a component: colours are tokens, set in
//     globals.css, or the charter cannot be held in one place.
//
// It runs as part of `npm run lint`. No dependency: it reads files and
// matches lines, and says where.

import { readdirSync, readFileSync } from "node:fs";
import { join, relative } from "node:path";

const root = new URL("..", import.meta.url).pathname;
const src = join(root, "src");

// The only file that may use the status tokens, besides globals.css which
// defines them.
const statusOwners = new Set(["src/components/status-badge.tsx", "src/app/globals.css"]);

const rules = [
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
    name: "colour written in hex in a component",
    pattern: /#[0-9a-fA-F]{3}(?:[0-9a-fA-F]{3}(?:[0-9a-fA-F]{2})?)?\b/,
    applies: (file) => /\.(tsx?|jsx?)$/.test(file),
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
