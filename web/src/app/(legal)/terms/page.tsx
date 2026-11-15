import type { Metadata } from "next";

import { Terms } from "@/features/legal/components/terms";

export const metadata: Metadata = { title: "Terms of use · Ballast" };

// Public: the proxy lets it through without a session (proxy-rule.ts).
export default function TermsPage() {
  return <Terms />;
}
