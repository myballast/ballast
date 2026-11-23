import { readFile } from "node:fs/promises";
import path from "node:path";

import type { Metadata } from "next";

import { DocPage } from "@/features/docs/components/doc-page";

export const metadata: Metadata = {
  title: "Start a run from a signed request · Ballast",
  description: "How to sign a request so that a deploy script or Jenkins can start a Ballast run.",
};

// Public, like the page about CI: the proxy lets it through without a session
// (proxy-rule.ts). The text is the file docs/webhook.md of the repository, copied here because
// the image is built from web/ alone; a test of the repository keeps the two identical.
export default async function WebhookDocPage() {
  const source = await readFile(path.join(process.cwd(), "src/features/docs/content/webhook.md"), "utf8");
  return <DocPage title="Start a run from a signed request" source={source} />;
}
