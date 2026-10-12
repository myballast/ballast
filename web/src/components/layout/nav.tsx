"use client";

import { FolderKanban, type LucideIcon } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";

import { cn } from "@/lib/utils";

// A client component for one reason: knowing which page is current.
const items: { href: string; label: string; icon: LucideIcon }[] = [
  { href: "/projects", label: "Projects", icon: FolderKanban },
];

export function Nav() {
  const pathname = usePathname();

  return (
    <nav aria-label="Main" className="flex flex-col gap-0.5">
      <p className="px-2 pb-2 text-[11px] tracking-wider text-muted-foreground uppercase">Workspace</p>
      {items.map(({ href, label, icon: Icon }) => {
        const current = href === "/" ? pathname === "/" : pathname.startsWith(href);
        return (
          <Link
            key={href}
            href={href}
            aria-current={current ? "page" : undefined}
            className={cn(
              "relative flex items-center gap-2.5 rounded-md px-2 py-1.5 text-sm text-muted-foreground hover:bg-sidebar-accent hover:text-foreground",
              // The current page is marked in the brand's violet: it says where
              // you are, which is not a status.
              current &&
                "bg-sidebar-accent text-foreground before:absolute before:inset-y-1.5 before:-left-2 before:w-0.5 before:rounded-full before:bg-brand [&>svg]:text-brand",
            )}
          >
            <Icon aria-hidden className="size-4" />
            {label}
          </Link>
        );
      })}
    </nav>
  );
}
