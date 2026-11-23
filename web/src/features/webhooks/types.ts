// A project's webhook as GET /projects/{projectId}/webhook says it. There is no secret
// here and nothing that could be one: the secret is shown once, when it is made or replaced.
export type Webhook = {
  id: string;
  project_id: string;
  // Where a tool sends its signed requests, relative to the API's address:
  // /webhooks/{organization}/{project}. Not a secret, and it proves nothing.
  path: string;
  created_at: string;
  // Absent when the secret was never replaced.
  rotated_at?: string;
};
