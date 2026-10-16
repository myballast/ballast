import type { Access } from "@/lib/permissions";

// What the frame around a project's pages needs to know, read once by the
// layout of the project and handed to the sidebar and the breadcrumb.
//
// The names are here because a layout does not render again when the page
// under it changes: the breadcrumb is built in the browser from the address,
// and looks the names up in these maps instead of showing an identifier.
export type ShellScope = {
  project: { id: string; name: string };
  // From the membership, resolved by the API for this request: never from the
  // session token.
  role: Access["role"];
  names: {
    scenarios: Record<string, string>;
    environments: Record<string, string>;
    // Only on a run's own page, where the run is what the address names.
    run?: { id: string; label: string };
  };
};
