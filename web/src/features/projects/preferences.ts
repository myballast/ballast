import { cookies } from "next/headers";

import { isSortKey, isViewMode, type SortKey, type ViewMode } from "./arrangement";

// The names of the two cookies, shared by the action that sets them and the page
// that reads them. Not HttpOnly: there is nothing in them worth hiding, and
// nothing here would be a reason to refuse a script reading how a list is laid
// out.
export const viewCookie = "ballast_projects_view";
export const sortCookie = "ballast_projects_sort";

export type Preferences = { view: ViewMode; sort: SortKey };

// getPreferences reads how this browser likes the list, on the server. A missing
// or unrecognised value is the default — a cookie is whatever a browser sends.
export async function getPreferences(): Promise<Preferences> {
  const jar = await cookies();
  const view = jar.get(viewCookie)?.value;
  const sort = jar.get(sortCookie)?.value;

  return {
    view: isViewMode(view) ? view : "grid",
    sort: isSortKey(sort) ? sort : "recent",
  };
}
