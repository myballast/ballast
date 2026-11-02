import { LogOut } from "lucide-react";

import { signOut } from "@/features/auth/actions";

// UserMenu shows who is signed in, and signs them out.
//
// Inside a project it says the role the person holds there, which is what
// decides what they see. Elsewhere there is no role to say, and the email
// takes its place.
export function UserMenu({ user, role }: { user: { name: string; email: string }; role?: string }) {
  return (
    <div className="flex items-center gap-2.5 rounded-md px-2 py-2 group-data-[collapsed=true]/sidebar:flex-col group-data-[collapsed=true]/sidebar:px-0">
      <span
        aria-hidden
        className="grid size-7 shrink-0 place-items-center rounded-full bg-secondary text-xs font-medium"
      >
        {initial(user)}
      </span>
      <div className="min-w-0 flex-1 group-data-[collapsed=true]/sidebar:sr-only">
        <p className="truncate text-sm">{user.name || user.email}</p>
        <p className="data truncate text-[11px] text-muted-foreground">{role ?? user.email}</p>
      </div>
      <form action={signOut}>
        <button
          type="submit"
          className="grid size-7 place-items-center rounded-md text-muted-foreground hover:bg-sidebar-accent hover:text-foreground"
        >
          <LogOut aria-hidden className="size-4" />
          <span className="sr-only">Sign out</span>
        </button>
      </form>
    </div>
  );
}

function initial(user: { name: string; email: string }): string {
  return (user.name || user.email).trim().charAt(0).toUpperCase() || "?";
}
