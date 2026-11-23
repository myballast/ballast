// What the proxy does with a request, apart from Next so that it can be tested.
//
// "/" is the front door: somebody signed in goes on to their projects, anybody
// else is shown the landing page. The legal pages are read by anybody: someone
// deciding whether to sign in has to be able to read what they would accept. So is the
// documentation, which is how somebody finds out what Ballast is before they have an account.
// Every other page asks for a session.
export type Decision = "continue" | "landing" | "login" | "projects";

// Compared whole, never by prefix: "/terms-of-something" is not a legal page, and "/docs" alone is not a page of the documentation.
const publicPages = new Set(["/privacy", "/terms", "/docs/ci"]);

export function decide(pathname: string, hasSession: boolean): Decision {
  if (pathname === "/") return hasSession ? "projects" : "landing";
  if (publicPages.has(pathname)) return "continue";
  return hasSession ? "continue" : "login";
}
