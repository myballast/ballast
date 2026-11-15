import Link from "next/link";

import { BetaBadge } from "@/components/beta-badge";

import { Logo } from "./logo";

// Brand is the logo and the name, top left, leading home. The link is named
// for what it does; the mark itself is decorative next to the word.
export function Brand() {
  return (
    <Link href="/" aria-label="Ballast, home" className="flex items-center gap-2.5 px-2 group-data-[collapsed=true]/sidebar:justify-center group-data-[collapsed=true]/sidebar:px-0">
      <Logo className="text-brand" />
      <span className="text-[15px] font-medium tracking-wide group-data-[collapsed=true]/sidebar:sr-only">Ballast</span>
      <BetaBadge className="group-data-[collapsed=true]/sidebar:hidden" />
    </Link>
  );
}
