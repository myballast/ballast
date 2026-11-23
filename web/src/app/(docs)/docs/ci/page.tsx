import { readFile } from "node:fs/promises";
import path from "node:path";

import type { Metadata } from "next";

import { DocPage } from "@/features/docs/components/doc-page";

export const metadata: Metadata = {
  title: "Run Ballast from CI · Ballast",
  description: "From a token to a GitHub job that passes or fails on your thresholds.",
};

// Public: the proxy lets it through without a session (proxy-rule.ts). The text is the
// file docs/ci.md of the repository, copied here because the image is built from web/ alone;
// a test of the repository keeps the two identical.
export default async function CIDocPage() {
  const source = await readFile(path.join(process.cwd(), "src/features/docs/content/ci.md"), "utf8");
  return <DocPage title="Run Ballast from CI" source={source} />;
}
