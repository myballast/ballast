"use client";

import { FolderKanban, KeyRound, LayoutDashboard, Play, Server, Users, Workflow } from "lucide-react";
import { usePathname } from "next/navigation";

import { NavGroup, NavItem } from "./nav-item";

// A client component for one reason: knowing which page is current.
//
// The sidebar has a section for the project the page belongs to, on every page
// of that project — a run's included.
//
// canManageTokens hides the CI tokens entry from whoever may not use it; the page
// and the API refuse them anyway.
export function Nav({
  project,
  canManageTokens = false,
}: {
  project?: { id: string; name: string };
  canManageTokens?: boolean;
}) {
  const pathname = usePathname();
  const base = project ? `/projects/${project.id}` : null;

  // Inside a project, Projects is the list and nothing else, and Overview is
  // the project's own page: both are prefixes of every page below them, and
  // would be lit on all of them.
  const active = (href: string, exact: boolean) =>
    exact ? pathname === href : pathname === href || pathname.startsWith(`${href}/`);

  return (
    <nav aria-label="Main" className="flex flex-col gap-6 group-data-[collapsed=true]/sidebar:gap-3">
      <NavGroup title="Workspace">
        <NavItem href="/projects" icon={FolderKanban} active={active("/projects", base !== null)}>
          Projects
        </NavItem>
      </NavGroup>

      {project && base && (
        <NavGroup title={project.name}>
          <NavItem href={base} icon={LayoutDashboard} active={active(base, true)}>
            Overview
          </NavItem>
          <NavItem href={`${base}/scenarios`} icon={Workflow} active={active(`${base}/scenarios`, false)}>
            Scenarios
          </NavItem>
          {/* A run's own page is /runs/<id>, outside the project's address, and
              is still a run of the project: the entry stays lit on it. */}
          <NavItem href={`${base}/runs`} icon={Play} active={active(`${base}/runs`, false) || pathname.startsWith("/runs/")}>
            Runs
          </NavItem>
          <NavItem href={`${base}/environments`} icon={Server} active={active(`${base}/environments`, false)}>
            Environments
          </NavItem>
          <NavItem href={`${base}/members`} icon={Users} active={active(`${base}/members`, false)}>
            Members
          </NavItem>
          {canManageTokens && (
            <NavItem href={`${base}/tokens`} icon={KeyRound} active={active(`${base}/tokens`, false)}>
              CI tokens
            </NavItem>
          )}
        </NavGroup>
      )}
    </nav>
  );
}
