import type { RunCI } from "./types";

// What a pipeline said about itself, as a run's page shows it.
//
// It is declared by whoever launched the run and proved to Ballast by nobody, so
// this file draws nothing from it that could be followed or interpreted: the text
// comes out as it was sent, to be rendered as text, and the one thing that can be
// clicked — the link to the job — is followed only if it is an https address.

const providers: Record<string, string> = {
  github_actions: "GitHub Actions",
  gitlab_ci: "GitLab CI",
};

// providerLabel names a provider. One this screen does not know — the API may be
// newer than it — is still a pipeline, and is not drawn as a blank.
export function providerLabel(provider: string): string {
  return providers[provider] ?? "CI";
}

// shortCommit is a commit the way a person says one: seven characters.
export function shortCommit(commit: string | undefined): string | undefined {
  if (!commit) return undefined;
  return commit.slice(0, 7);
}

export type CIFact = { key: "ci" | "repository" | "branch" | "commit"; value: string };

// ciFacts are the facts that were said, in the order a person asks them: which CI,
// which repository, which branch, which commit. A field the pipeline left out is
// left out here, and not drawn empty.
export function ciFacts(ci: RunCI): CIFact[] {
  const facts: CIFact[] = [{ key: "ci", value: providerLabel(ci.provider) }];

  if (ci.repository) facts.push({ key: "repository", value: ci.repository });
  if (ci.branch) facts.push({ key: "branch", value: ci.branch });
  const commit = shortCommit(ci.commit);
  if (commit) facts.push({ key: "commit", value: commit });

  return facts;
}

export type JobLink = { href: string; host: string };

// jobLink is the link to the job, or null if what was kept is not one that may be
// followed: only an https address, with no credentials, and plain ASCII with no
// space. The API refuses anything else before it is stored and so does the table;
// this is the third line, for a row written some other way. The query and the
// fragment are not followed either: they may carry a token, and say nothing about
// which job it is.
export function jobLink(raw: string | undefined): JobLink | null {
  if (!raw || /[\s\\]/.test(raw) || !/^[\x21-\x7e]+$/.test(raw)) return null;

  let url: URL;
  try {
    url = new URL(raw);
  } catch {
    return null;
  }
  if (url.protocol !== "https:" || !url.host || url.username || url.password) return null;

  return { href: `${url.origin}${url.pathname}`, host: url.host };
}
