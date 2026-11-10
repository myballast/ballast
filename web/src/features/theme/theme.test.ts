import assert from "node:assert/strict";
import test from "node:test";

import { isDark, parseTheme, systemThemeScript } from "./theme.ts";

test("only the three themes are read, and the rest is dark", () => {
  assert.equal(parseTheme("light"), "light");
  assert.equal(parseTheme("dark"), "dark");
  assert.equal(parseTheme("system"), "system");
  for (const value of [undefined, "", "Light", "blue", "<script>"]) assert.equal(parseTheme(value), "dark", String(value));
});

test("a theme is dark by itself, or by the system's word", () => {
  assert.equal(isDark("dark", false), true);
  assert.equal(isDark("light", true), false);
  assert.equal(isDark("system", true), true);
  assert.equal(isDark("system", false), false);
});

test("the script of the head holds no input of anybody's", () => {
  assert.ok(!/\$\{/.test(systemThemeScript));
  assert.ok(systemThemeScript.includes("prefers-color-scheme: dark"));
});
