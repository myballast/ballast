// Mirror of dto.CatalogEndpoint in internal/adapter/http/dto: one endpoint of a
// project's catalog, read from a specification somebody imported.
export type CatalogEndpoint = {
  id: string;
  // The file it came from. Importing the same source again replaces it.
  source: string;
  method: string;
  // The template as the specification wrote it: /users/{id}.
  path: string;
  tag: string;
  summary: string;
  path_params: string[];
  // Never a credential: the API leaves those out.
  headers: Record<string, string>;
  content_type: string;
  body: string;
  body_too_long: boolean;
  created_at: string;
};
