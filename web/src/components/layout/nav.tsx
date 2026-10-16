"use client";

import { usePathname } from "next/navigation";

import { NavGroup, NavItem } from "./nav-item";

// A client component for one reason: knowing which page is current.
//
// The sidebar has a section for the project the page belongs to, on every page
// of that project — a run's included. Runs and Members have no screen yet (T35
// and T38) and are not listed until they do.
export function Nav({ project }: { project?: { id: string; name: string } }) {
  const pathname = usePathname();
  const base = project ? `/projects/${project.id}` : null;

  // Inside a project, Projects is the list and nothing else, and Overview is
  // the project's own page: both are prefixes of every page below them, and
  // would be lit on all of them.
  const active = (href: string, exact: boolean) =>
    exact ? pathname === href : pathname === href || pathname.startsWith(`${href}/`);

  return (
    <nav aria-label="Main" className="flex flex-col gap-6">
      <NavGroup title="Workspace">
        <NavItem href="/projects" active={active("/projects", base !== null)}>
          Projects
        </NavItem>
      </NavGroup>

      {project && base && (
        <NavGroup title={project.name}>
          <NavItem href={base} active={active(base, true)}>
            Overview
          </NavItem>
          <NavItem href={`${base}/scenarios`} active={active(`${base}/scenarios`, false)}>
            Scenarios
          </NavItem>
          <NavItem href={`${base}/environments`} active={active(`${base}/environments`, false)}>
            Environments
          </NavItem>
        </NavGroup>
      )}
    </nav>
  );
}
