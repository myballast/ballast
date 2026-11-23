// The landing page is a static file (public/landing.html). These tests hold the rules of its copy that
// a later edit could break without anybody noticing: what it may not claim, and what it must link to.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const page = readFileSync(new URL("../../../public/landing.html", import.meta.url), "utf8");
const visible = page.replace(/<script[\s\S]*?<\/script>/g, "").replace(/<style[\s\S]*?<\/style>/g, "");

test("it names no repository, demo or release tag that does not exist", () => {
  for (const dead of ["ballast-" + "action", "ballast-" + "demo", "v1.2" + ".3", "releases/" + "tag"]) {
    assert.ok(!page.includes(dead), `the landing mentions ${dead}`);
  }
});

test("it never says the project is open source, except in the visitor's own question", () => {
  const hits = visible.match(/open[ -]source/gi) ?? [];
  assert.equal(hits.length, 1, "only the FAQ question may use the words");
  assert.match(visible, /<summary>Is it open source\?<\/summary>/);
});

test("the CLI, the Action and the template are only ever 'coming soon'", () => {
  assert.match(visible, /Coming soon/);
  assert.ok(!/uses:\s*myballast/.test(page), "an Action snippet is shown");
  assert.ok(!/curl[^<]*releases\/download/.test(page), "an install snippet for the CLI is shown");
});

test("it holds no imitation of the app's interface", () => {
  assert.ok(!/class="mock/.test(page), "a hand-built mockup is back");
  assert.ok(!page.includes("staging.example"), "the old mockup's target is back");
});

test("it has one H1, the GitHub link in the places that matter, and the CI and webhook pages", () => {
  assert.equal((page.match(/<h1[\s>]/g) ?? []).length, 1);
  const github = page.match(/href="https:\/\/github\.com\/myballast\/ballast"/g) ?? [];
  assert.ok(github.length >= 5, `GitHub is linked ${github.length} times: nav, hero, self-host, final call, footer`);
  for (const href of ["/docs/ci", "/docs/webhook", "mailto:contact@myballast.pro", "#how", "#features", "#safety", "#ci", "#self-host", "#faq"]) {
    assert.ok(page.includes(`href="${href}"`), `no link to ${href}`);
  }
  assert.ok(!page.includes('href="#compare"'), "the compare link is back");
});

test("it carries no drop effect, no weight beyond 400 and 500, and no glow", () => {
  const dropEffect = new RegExp(["box", "text", "drop"].map((k) => `${k}-sha` + "dow").join("|"));
  assert.ok(!dropEffect.test(page), "a drop effect");
  assert.ok(!/font-weight:\s*(?:(?:[1-3]|[6-9])00|bold)/.test(page), "a weight other than 400 and 500");
  assert.ok(!/glow/.test(page.replace(/texture without glow/, "")), "a glow is used");
});

test("it states the plan of the beta without a price", () => {
  assert.match(visible, /Free during the beta/);
  assert.ok(!/[$€£]\s?\d/.test(visible), "a price is written");
});
