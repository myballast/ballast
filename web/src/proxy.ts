import { NextResponse, type NextRequest } from "next/server";

// Must match session.CookieName in internal/adapter/http/session. Written out
// here rather than imported: Next runs the proxy apart from the application
// code and advises against sharing modules with it.
const sessionCookie = "ballast_session";

// A browser with no session at all is sent to sign in before any page renders,
// with the page it asked for as the place to come back to. Only a layout
// would otherwise notice, and a layout does not know which page it wraps.
//
// This checks that a cookie is present, not that it is valid: the API decides
// that on every call, and a page whose session the API refuses is sent to the
// login page by the page itself.
export function proxy(request: NextRequest) {
  if (request.cookies.has(sessionCookie)) {
    return NextResponse.next();
  }

  const login = new URL("/login", request.url);
  login.searchParams.set("next", request.nextUrl.pathname + request.nextUrl.search);
  return NextResponse.redirect(login);
}

export const config = {
  // Everything but the login page itself, the route handlers (which answer
  // 401 on their own), the development-only pages (a 404 in production, which
  // a redirect to sign in would hide), the icons and manifest a browser asks
  // for before anyone signs in — favicon.ico included, which browsers request
  // on their own and which is a plain 404 here — and Next's static files.
  matcher: [
    "/((?!login|api|dev/|_next/static|_next/image|favicon.ico|icon|apple-icon|manifest.webmanifest).*)",
  ],
};
