import type { ShellScope } from "./types";

// One step of the breadcrumb. A step without an href is not a page of its own
// — the organization — and is only said.
export type Crumb = { label: string; href?: string };

// buildTrail reads the breadcrumb from the address: the organization, the
// project, the section, and what the address names, by name.
//
// No step is ever an identifier. A name that is not known — a scenario created
// a moment ago, in a shell drawn before it existed — is said by its kind
// instead, which is less than the name and never less honest than a UUID.
export function buildTrail(pathname: string, organization: string, scope: ShellScope | null): Crumb[] {
  const segments = pathname.split("/").filter(Boolean);
  const trail: Crumb[] = [{ label: organization }];

  if (segments[0] === "projects" && segments.length === 1) {
    return [...trail, { label: "Projects", href: "/projects" }];
  }

  const onProject = segments[0] === "projects" && scope && segments[1] === scope.project.id;
  const onRun = segments[0] === "runs" && scope?.names.run?.id === segments[1];
  if (!scope || !(onProject || onRun)) {
    return [...trail, { label: "Projects", href: "/projects" }];
  }

  const base = `/projects/${scope.project.id}`;
  trail.push({ label: scope.project.name, href: base });

  if (onRun) {
    return [...trail, { label: "Runs", href: `${base}/runs` }, { label: scope.names.run?.label ?? "Run", href: pathname }];
  }

  const [section, item, action] = segments.slice(2);
  switch (section) {
    case "scenarios":
    case "environments": {
      const singular = section === "scenarios" ? "Scenario" : "Environment";
      const names = section === "scenarios" ? scope.names.scenarios : scope.names.environments;
      trail.push({ label: section === "scenarios" ? "Scenarios" : "Environments", href: `${base}/${section}` });
      if (item === "new") {
        trail.push({ label: `New ${singular.toLowerCase()}` });
      } else if (item) {
        trail.push({ label: names[item] ?? singular, href: `${base}/${section}/${item}` });
        if (action === "edit") trail.push({ label: "Edit" });
      }
      break;
    }
    case "runs":
      trail.push({ label: "Runs", href: `${base}/runs` });
      if (item === "new") trail.push({ label: "New run" });
      break;
    case "members":
      trail.push({ label: "Members", href: `${base}/members` });
      break;
    case "tokens":
      trail.push({ label: "CI tokens", href: `${base}/tokens` });
      break;
    case "webhook":
      trail.push({ label: "Webhook", href: `${base}/webhook` });
      break;
  }
  return trail;
}
