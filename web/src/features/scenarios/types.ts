import type { ProfileKind, ProfileParams } from "@/features/runs/types";

// Mirror of dto.ScenarioSummary in internal/adapter/http/dto.
export type ScenarioSummary = {
  id: string;
  name: string;
  version: number;
  default_profile: ProfileKind;
  default_params: ProfileParams;
  created_at: string;
  updated_at: string;
};

// Mirror of dto.ScenarioListItem: a scenario as the list shows it.
export type ScenarioListItem = ScenarioSummary & { threshold_count: number };

// Mirror of dto.ScriptFinding. Key on the code; the message is the API's.
export type ScriptFinding = {
  code: string;
  message: string;
  detail?: string;
  // From one; absent when the finding is about the script as a whole.
  line?: number;
};

// The kinds of step a graph is made of. A closed set, as in the domain.
export const nodeKinds = [
  "http_request",
  "extract_variable",
  "set_variable",
  "condition",
  "loop",
  "think_time",
  "assertion",
  "end",
] as const;
export type NodeKind = (typeof nodeKinds)[number];

// Mirror of dto.GraphNode. One shape for every kind: what a kind does not use is
// left out, and the API says which fields a kind requires.
export type GraphNode = {
  id: string;
  kind: NodeKind;
  method?: string;
  path?: string;
  body?: string;
  headers?: Record<string, string>;
  query?: { name: string; value: string }[];
  variable?: string;
  // Where an extract_variable reads in the response; the body when left out.
  source?: "body" | "header" | "status";
  from?: string;
  value?: string;
  from_env?: string;
  expression?: string;
  iterations?: number;
  seconds?: number;
  group?: string;
  label?: string;
  critical?: boolean;
};

// Mirror of dto.GraphEdge. `when` labels the two ways out of a condition (true,
// false) or of a loop (body, after).
export type GraphEdge = {
  from: string;
  to: string;
  when?: "true" | "false" | "body" | "after";
};

// Mirror of dto.ScenarioGraph.
export type ScenarioGraph = {
  start: string;
  nodes: GraphNode[];
  edges: GraphEdge[];
};

// Mirror of dto.Scenario: one scenario with its script, and the warnings
// inspecting it raised. Errors never get this far: they refuse the script.
export type Scenario = ScenarioSummary & {
  script: string;
  // "graph" means Ballast generated the script from a graph, and it is read
  // only: the API refuses a new script for it.
  mode: "code" | "graph";
  // The graph of a scenario whose mode is "graph"; absent for one written as code.
  graph?: ScenarioGraph;
  warnings: ScriptFinding[];
};

// Mirror of dto.ThresholdRule.
export type Threshold = {
  id: string;
  // Accepts k6's tagged syntax: http_req_duration{name:POST /login}.
  metric: string;
  expression: string;
};

// One version of a scenario's script, as it was when it was written. A run
// names the version it executed, and that version never changes.
export type ScenarioVersion = {
  version: number;
  script: string;
  default_profile: ProfileKind;
  default_params: ProfileParams;
  created_at: string;
};

// Mirror of dto.OpenAPIEndpoint: one operation of an uploaded specification.
export type OpenAPIEndpoint = {
  method: string;
  // The template as the specification wrote it: /users/{id}.
  path: string;
  tag: string;
  summary: string;
  path_params: string[];
  has_body: boolean;
  body_too_long: boolean;
};

// Mirror of dto.OpenAPIAuthScheme. It names a scheme and says where it goes;
// it never holds a value.
export type OpenAPIScheme = {
  name: string;
  type: string;
  detail: string;
};

// Mirror of dto.OpenAPIPreviewResponse.
export type OpenAPIPreview = {
  title: string;
  base_path: string;
  endpoints: OpenAPIEndpoint[];
  schemes: OpenAPIScheme[];
  notes: string[];
};

// What a file of requests was exported from. The path segment of the import
// routes: /scenarios/import/{format}.
export type RequestsFormat = "postman" | "har" | "curl";

// What a browser used a HAR request for. Mirrors the kinds the HAR reader sorts
// into; the other formats leave it empty.
export type RequestKind = "api" | "document" | "script" | "style" | "image" | "font" | "media" | "other";

// Mirror of dto.CollectionRequest: one request of an uploaded collection, HAR
// file or set of cURL commands.
export type CollectionRequest = {
  // How the request is named when chosen. Names repeat; positions do not.
  position: number;
  folders: string[];
  name: string;
  method: string;
  // The host a HAR request was sent to, and what the browser used it for. Both
  // are empty for the other formats.
  host: string;
  kind: RequestKind | "";
  // The template it becomes. Empty when it was left out.
  path: string;
  has_body: boolean;
  left_out: boolean;
  findings: string[];
};

// Mirror of dto.DetectedSecret. It holds no value: the person types it where it
// belongs, on the environment.
export type DetectedSecret = {
  key: string;
  variable: string;
};

// Mirror of dto.ImportFinding: one thing an import could not carry over.
export type ImportFinding = {
  // The name of the request; empty when the finding is about the collection.
  request: string;
  message: string;
};

// Mirror of dto.CollectionPreviewResponse.
export type CollectionPreview = {
  name: string;
  requests: CollectionRequest[];
  secrets: DetectedSecret[];
  findings: ImportFinding[];
  // How many nodes the variables would take at the start of a scenario: that much
  // less room for requests.
  variable_nodes: number;
};
