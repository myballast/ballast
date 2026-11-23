export type ApiTokenState = "active" | "expired" | "revoked";

// A token as GET /projects/{projectId}/api-tokens lists it. The secret and the
// hash are never here: the secret is shown once, when the token is created.
export type ApiToken = {
  id: string;
  name: string;
  // The start of the secret: enough to tell two tokens apart.
  prefix: string;
  // Absent when the person who created it has left; the token stays.
  created_by?: string;
  created_by_name?: string;
  // Read against the clock by the API, never stored.
  state: ApiTokenState;
  expires_at: string;
  last_used_at?: string;
  revoked_at?: string;
  created_at: string;
};

export type ApiTokenList = {
  tokens: ApiToken[];
  // How many tokens that have not been revoked a project may hold. Said by the
  // API, so that this screen carries no copy of the ceiling.
  limit: number;
};
