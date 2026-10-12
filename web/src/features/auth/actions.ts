"use server";

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { sessionCookie } from "@/lib/api-client";

// signOut forgets the session. The session is a signed token with nothing
// kept on the server, so removing the cookie is the whole of signing out.
// The cookie was set by the API for this host, and cookies do not depend on
// the port, so the front end's server can remove it.
export async function signOut() {
  (await cookies()).delete(sessionCookie);
  redirect("/login");
}
