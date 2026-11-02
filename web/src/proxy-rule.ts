// What the proxy does with a request, apart from Next so that it can be tested.
//
// "/" is the front door: somebody signed in goes on to their projects, anybody
// else is shown the landing page. Every other page asks for a session.
export type Decision = "continue" | "landing" | "login" | "projects";

export function decide(pathname: string, hasSession: boolean): Decision {
  if (pathname === "/") return hasSession ? "projects" : "landing";
  return hasSession ? "continue" : "login";
}
