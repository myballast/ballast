import Link from "next/link";

import { Logo } from "./logo";

// Brand is the logo and the name, top left, leading home. The link is named
// for what it does; the mark itself is decorative next to the word.
export function Brand() {
  return (
    <Link href="/" aria-label="Ballast, home" className="flex items-center gap-2.5 px-2">
      <Logo className="text-brand" />
      <span className="text-[15px] font-medium tracking-wide">Ballast</span>
    </Link>
  );
}
