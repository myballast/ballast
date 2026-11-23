import type { ApiToken } from "./types";

// liveTokens are the tokens that count against a project's ceiling: everything
// that has not been revoked, expired or not.
export function liveTokens(tokens: ApiToken[]): number {
  return tokens.filter((token) => token.state !== "revoked").length;
}

// creditsLeft is how many more tokens the project may hold.
export function creditsLeft(tokens: ApiToken[], limit: number): number {
  return Math.max(0, limit - liveTokens(tokens));
}

// whoMade says who created a token, in words. The list never shows an
// identifier: a person with no name on file is still "a member", and one who has
// left — the API cleared the id — is "a former member". The token outlives them.
export function whoMade(token: ApiToken): string {
  if (token.created_by_name) return token.created_by_name;
  return token.created_by ? "a member" : "a former member";
}
