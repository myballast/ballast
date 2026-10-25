import { relayToApi } from "@/features/auth/relay";
import { apiBaseURL } from "@/lib/api-client";

// Where the provider sends the browser back: relayed to the API, which checks
// the sign-in is the one this browser started and answers with the session.
export async function GET(request: Request, context: RouteContext<"/auth/[provider]/callback">) {
  const { provider } = await context.params;

  return relayToApi(request, apiBaseURL, `/auth/${encodeURIComponent(provider)}/callback`);
}
