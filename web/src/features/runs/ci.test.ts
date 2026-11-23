import assert from "node:assert/strict";
import { test } from "node:test";

import { ciFacts, jobLink, providerLabel, shortCommit } from "./ci.ts";
import type { RunCI } from "./types.ts";

// What a pipeline said about itself, as a run's page shows it. Declared by the
// caller and proved by nobody, so this file is about what is drawn from it and
// what is not: the link is followed only if it is an https address, and the rest
// is text.

const full: RunCI = {
  provider: "github_actions",
  repository: "myballast/ballast",
  branch: "feature/ci-metadata",
  commit: "3f9c2a1b7d4e5f60718293a4b5c6d7e8f9012345",
  job_url: "https://github.com/myballast/ballast/actions/runs/123456789",
};

test("each provider is called by its name, and one nobody knows by what it is", () => {
  assert.equal(providerLabel("github_actions"), "GitHub Actions");
  assert.equal(providerLabel("gitlab_ci"), "GitLab CI");
  assert.equal(providerLabel("other"), "CI");
  // An API newer than this screen may say a provider it does not know: it is
  // still a pipeline, and not a blank.
  assert.equal(providerLabel("circleci"), "CI");
  assert.equal(providerLabel(""), "CI");
});

test("a commit is shown as seven characters, the way a person says one", () => {
  assert.equal(shortCommit(full.commit!), "3f9c2a1");
  assert.equal(shortCommit("abcdef1"), "abcdef1");
  assert.equal(shortCommit(undefined), undefined);
  assert.equal(shortCommit(""), undefined);
});

test("the facts are the ones that were said, in the order a person asks them", () => {
  assert.deepEqual(ciFacts(full), [
    { key: "ci", value: "GitHub Actions" },
    { key: "repository", value: "myballast/ballast" },
    { key: "branch", value: "feature/ci-metadata" },
    { key: "commit", value: "3f9c2a1" },
  ]);
});

test("a field the pipeline left out is left out of the facts, not shown empty", () => {
  assert.deepEqual(ciFacts({ provider: "gitlab_ci", commit: "abcdef1" }), [
    { key: "ci", value: "GitLab CI" },
    { key: "commit", value: "abcdef1" },
  ]);
  assert.deepEqual(ciFacts({ provider: "other" }), [{ key: "ci", value: "CI" }]);
});

test("the link is followed only if it is an https address with no credentials", () => {
  assert.deepEqual(jobLink("https://github.com/myballast/ballast/actions/runs/42"), {
    href: "https://github.com/myballast/ballast/actions/runs/42",
    host: "github.com",
  });
  assert.deepEqual(jobLink("https://ci.example.test:8443/job/7"), {
    href: "https://ci.example.test:8443/job/7",
    host: "ci.example.test:8443",
  });
});

// The API refuses these before they are stored, and the table refuses them too.
// This is the third line, for a row written some other way: an address that is not
// https is text, never something a click can follow.
test("anything else is not a link", () => {
  for (const bad of [
    undefined,
    "",
    "http://ci.example.test/job",
    "javascript:alert(1)",
    "data:text/html,<script>alert(1)</script>",
    "https://user:hunter2@ci.example.test/job",
    "https://hunter2@ci.example.test/job",
    "//ci.example.test/job",
    "ci.example.test/job",
    "https://",
    "not a url",
    "https://ci.example.test/ job",
  ]) {
    assert.equal(jobLink(bad), null, `${String(bad)} was taken for a link`);
  }
});

test("a link that has been kept with a query is shown without it", () => {
  // Never stored that way, but what is drawn must not depend on that.
  assert.deepEqual(jobLink("https://ci.example.test/job/7?token=abc#frag"), {
    href: "https://ci.example.test/job/7",
    host: "ci.example.test",
  });
});

test("text a pipeline sent is passed through as it is: drawing it is React's, as text", () => {
  const hostile: RunCI = {
    provider: "other",
    repository: "<img src=x onerror=alert(1)>",
    branch: "main\u202egnp.exe",
    commit: "abcdef1",
  };

  const facts = ciFacts(hostile);

  // Nothing here interprets it, strips it or trusts it: the values come out as
  // sent, and are rendered by a component that never writes markup.
  assert.equal(facts.find((f) => f.key === "repository")?.value, "<img src=x onerror=alert(1)>");
  assert.equal(facts.find((f) => f.key === "branch")?.value, "main\u202egnp.exe");
});
