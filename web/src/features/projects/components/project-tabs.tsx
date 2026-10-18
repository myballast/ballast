import Link from "next/link";

import { cn } from "@/lib/utils";

// ProjectTabs are the sections of a project, as the sidebar lists them, laid
// out under the project's name. Settings is not here until its page is: a tab
// with nothing behind it is a dead end.
export function ProjectTabs({ projectId }: { projectId: string }) {
  const base = `/projects/${projectId}`;
  const tabs = [
    { href: base, label: "Overview", current: true },
    { href: `${base}/scenarios`, label: "Scenarios" },
    { href: `${base}/runs`, label: "Runs" },
    { href: `${base}/environments`, label: "Environments" },
    { href: `${base}/members`, label: "Members" },
  ];

  return (
    <nav aria-label="Project sections" className="flex flex-wrap gap-x-6 gap-y-1 border-b">
      {tabs.map((tab) => (
        <Link
          key={tab.label}
          href={tab.href}
          aria-current={tab.current ? "page" : undefined}
          className={cn(
            "-mb-px border-b-[1.5px] border-transparent pb-2.5 text-[12.5px] text-muted-foreground hover:text-foreground",
            tab.current && "border-brand text-foreground",
          )}
        >
          {tab.label}
        </Link>
      ))}
    </nav>
  );
}
