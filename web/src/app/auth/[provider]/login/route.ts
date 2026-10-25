import { relayToApi } from "@/features/auth/relay";
import { apiBaseURL } from "@/lib/api-client";

// Starts a sign-in: relayed to the API, which sends the browser on to the
// provider. See features/auth/relay.ts for why it is relayed.
export async function GET(request: Request, context: RouteContext<"/auth/[provider]/login">) {
  const { provider } = await context.params;

  return relayToApi(request, apiBaseURL, `/auth/${encodeURIComponent(provider)}/login`);
}
