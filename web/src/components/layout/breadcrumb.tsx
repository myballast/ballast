import Link from "next/link";
import { Fragment } from "react";

import type { Crumb } from "@/features/shell/trail";

// Breadcrumb draws a trail in the monospace face. Every step is a name, a step
// is a link when it is a page, and the last one is where you are.
export function Breadcrumb({ trail }: { trail: Crumb[] }) {
  return (
    <nav aria-label="Breadcrumb" className="min-w-0">
      <ol className="data flex flex-wrap items-center gap-x-1.5 gap-y-0.5 text-[11.5px] text-muted-foreground">
        {trail.map((crumb, i) => {
          const last = i === trail.length - 1;
          return (
            <Fragment key={`${i}-${crumb.label}`}>
              {i > 0 && (
                <li aria-hidden className="shrink-0">
                  /
                </li>
              )}
              <li className="min-w-0 break-words">
                {last ? (
                  <span aria-current="page" className="text-foreground">
                    {crumb.label}
                  </span>
                ) : crumb.href ? (
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
  );
}
