"use client";

import { ChevronRight } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { Fragment } from "react";

// A client component for one reason: the trail is read from the current URL.
//
// Known sections are named; anything else in the path is an identifier, set in
// the monospace face and shortened, since a full UUID says nothing more to a
// reader than its first eight characters.
const sections: Record<string, string> = {
  runs: "Runs",
  projects: "Projects",
  scenarios: "Scenarios",
  environments: "Environments",
  members: "Members",
};

// The paths that are pages of their own. A step of the trail links only to
// one of these: /runs, for instance, is a step on the way to a run and not a
// page anyone can open. Screens add their path here when they exist.
const pages = new Set(["/", "/projects"]);

const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function AppHeader() {
  const segments = usePathname().split("/").filter(Boolean);

  const trail = [
    { href: "/", label: "Ballast", id: false },
    ...segments.map((segment, i) => ({
      href: "/" + segments.slice(0, i + 1).join("/"),
      label: sections[segment] ?? (uuid.test(segment) ? segment.slice(0, 8) : segment),
      id: !(segment in sections),
    })),
  ];

  return (
    <header className="flex h-12 items-center border-b px-4 md:px-page-x">
      <nav aria-label="Breadcrumb">
        <ol className="flex items-center gap-1.5 text-xs text-muted-foreground">
          {trail.map((crumb, i) => {
            const last = i === trail.length - 1;
            return (
              <Fragment key={crumb.href}>
                {i > 0 && <ChevronRight aria-hidden className="size-3" />}
                <li className={crumb.id ? "data" : undefined}>
                  {last ? (
                    <span aria-current="page" className="text-foreground">
                      {crumb.label}
                    </span>
                  ) : pages.has(crumb.href) ? (
                    <Link href={crumb.href} className="hover:text-foreground">
                      {crumb.label}
                    </Link>
                  ) : (
                    <span>{crumb.label}</span>
                  )}
                </li>
              </Fragment>
            );
          })}
        </ol>
      </nav>
    </header>
  );
}
