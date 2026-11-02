export const sidebarCookie = "sidebar";

export type SidebarPreference = "collapsed" | "open";

// collapsedFor says whether the sidebar is drawn collapsed: what the person chose
// wins, and with no choice yet the builder's pages start collapsed, since the
// canvas is what is there to look at.
export function collapsedFor(preference: SidebarPreference | undefined, pathname: string): boolean {
  if (preference) return preference === "collapsed";
  return /\/scenarios\/[^/]+\/builder\/?$/.test(pathname);
}
