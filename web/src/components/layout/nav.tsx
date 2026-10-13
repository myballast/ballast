"use client";

import { FolderKanban, Globe, type LucideIcon } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";

import { cn } from "@/lib/utils";

// A client component for one reason: knowing which page is current.
type Item = { href: string; label: string; icon: LucideIcon; nested?: boolean };

const items: Item[] = [{ href: "/projects", label: "Projects", icon: FolderKanban }];

// The entries of the project the current page belongs to, when its address
// names one. A run's page does not: its address carries no project.
function projectItems(pathname: string): Item[] {
  const project = /^\/projects\/([^/]+)/.exec(pathname)?.[1];
  if (!project) return [];
  return [{ href: `/projects/${project}/environments`, label: "Environments", icon: Globe, nested: true }];
}

export function Nav() {
  const pathname = usePathname();
  const all = [...items, ...projectItems(pathname)];

  // The current entry is the most specific one the address falls under: on a
  // project's environments, Environments rather than Projects.
  const current = all
    .filter(({ href }) => pathname === href || pathname.startsWith(`${href}/`))
    .reduce<string | undefined>((best, { href }) => (!best || href.length > best.length ? href : best), undefined);

  return (
    <nav aria-label="Main" className="flex flex-col gap-0.5">
      <p className="px-2 pb-2 text-[11px] tracking-wider text-muted-foreground uppercase">Workspace</p>
      {all.map(({ href, label, icon: Icon, nested }) => (
        <Link
          key={href}
          href={href}
          aria-current={href === current ? "page" : undefined}
          className={cn(
            "relative flex items-center gap-2.5 rounded-md px-2 py-1.5 text-sm text-muted-foreground hover:bg-sidebar-accent hover:text-foreground",
            nested && "ml-4",
            // The current page is marked in the brand's violet: it says where
            // you are, which is not a status.
            href === current &&
              "bg-sidebar-accent text-foreground before:absolute before:inset-y-1.5 before:-left-2 before:w-0.5 before:rounded-full before:bg-brand [&>svg]:text-brand",
          )}
        >
          <Icon aria-hidden className="size-4" />
          {label}
        </Link>
      ))}
    </nav>
  );
}
