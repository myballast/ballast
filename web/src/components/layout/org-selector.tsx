import { Building2, Check, ChevronsUpDown } from "lucide-react";

import { switchOrganization } from "@/features/members/actions";

type Organization = { id: string; name: string };

// OrgSelector shows the organization the session acts for, and lists the ones
// the person belongs to. A person belongs to more than one once they have
// accepted an invitation: the project they were invited to is in another
// organization, and this is how they get back to their own.
//
// A native <details> rather than a client-side menu: it opens and closes
// without a line of JavaScript, and is keyboard-accessible as it stands. Each
// other organization is a form, so that switching works the same way.
export function OrgSelector({
  organization,
  organizations,
}: {
  organization: Organization;
  organizations: Organization[];
}) {
  const others = organizations.filter((o) => o.id !== organization.id);

  return (
    <details className="group relative">
      <summary
        title={organization.name}
        className="flex cursor-pointer list-none items-center gap-2 rounded-md border bg-background/40 px-2.5 py-2 text-sm hover:bg-sidebar-accent group-data-[collapsed=true]/sidebar:justify-center group-data-[collapsed=true]/sidebar:px-0 [&::-webkit-details-marker]:hidden"
      >
        <Building2 aria-hidden className="size-4 shrink-0 text-muted-foreground" />
        <span className="min-w-0 flex-1 truncate group-data-[collapsed=true]/sidebar:sr-only">{organization.name}</span>
        <ChevronsUpDown aria-hidden className="size-3.5 shrink-0 text-muted-foreground group-data-[collapsed=true]/sidebar:hidden" />
      </summary>

      <div className="absolute top-full left-0 z-20 mt-1 min-w-56 rounded-md border bg-popover p-1 group-data-[collapsed=true]/sidebar:w-60">
        <p className="px-2 pt-1 pb-1.5 text-[11px] tracking-wider text-muted-foreground uppercase">Organizations</p>
        <p className="flex items-center gap-2 rounded-sm px-2 py-1.5 text-sm" aria-current="true">
          <span className="min-w-0 flex-1 truncate">{organization.name}</span>
          <Check aria-hidden className="size-3.5 text-muted-foreground" />
        </p>
        {others.map((other) => (
          <form key={other.id} action={switchOrganization.bind(null, other.id)}>
            <button
              type="submit"
              className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-sm hover:bg-sidebar-accent"
            >
              <span className="min-w-0 flex-1 truncate">{other.name}</span>
              <span className="sr-only">Switch to this organization</span>
            </button>
          </form>
        ))}
        {others.length === 0 && (
          <p className="px-2 pt-1.5 pb-1 text-xs text-muted-foreground">
            Other organizations appear here once you accept an invitation to one.
          </p>
        )}
      </div>
    </details>
  );
}
