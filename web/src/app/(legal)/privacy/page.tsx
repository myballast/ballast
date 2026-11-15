import type { Metadata } from "next";

import { PrivacyPolicy } from "@/features/legal/components/privacy-policy";

export const metadata: Metadata = { title: "Privacy policy · Ballast" };

// Public: the proxy lets it through without a session (proxy-rule.ts).
export default function PrivacyPage() {
  return <PrivacyPolicy />;
}
