import { Building2, Check, ChevronsUpDown } from "lucide-react";

// OrgSelector shows the organization the session acts for, and lists the ones
// the user can switch to. Until invitations exist a user belongs to exactly
// one, so the list holds the current organization and nothing to switch to.
//
// A native <details> rather than a client-side menu: it opens and closes
// without a line of JavaScript, and is keyboard-accessible as it stands.
export function OrgSelector({ organization }: { organization: { id: string; name: string } }) {
  return (
    <details className="group relative">
      <summary className="flex cursor-pointer list-none items-center gap-2 rounded-md border bg-background/40 px-2.5 py-2 text-sm hover:bg-sidebar-accent [&::-webkit-details-marker]:hidden">
        <Building2 aria-hidden className="size-4 shrink-0 text-muted-foreground" />
        <span className="min-w-0 flex-1 truncate">{organization.name}</span>
        <ChevronsUpDown aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
      </summary>

      <div className="absolute inset-x-0 top-full z-20 mt-1 rounded-md border bg-popover p-1">
        <p className="px-2 pt-1 pb-1.5 text-[11px] tracking-wider text-muted-foreground uppercase">
          Organizations
        </p>
        <p className="flex items-center gap-2 rounded-sm px-2 py-1.5 text-sm" aria-current="true">
          <span className="min-w-0 flex-1 truncate">{organization.name}</span>
          <Check aria-hidden className="size-3.5 text-muted-foreground" />
        </p>
        <p className="px-2 pt-1.5 pb-1 text-xs text-muted-foreground">
          Other organizations appear here once you are invited to one.
        </p>
      </div>
    </details>
  );
}
