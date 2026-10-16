// The script a new scenario starts from: already what the checks ask for. It
// reads its target from __ENV.BASE_URL, names its request, checks the answer,
// and declares no options — Ballast writes those from the profile and the
// thresholds.
export const starterScript = `import http from "k6/http";
import { check } from "k6";

// No "export const options" here: Ballast writes it from the scenario's
// profile and thresholds.
export default function () {
  // The target comes from the environment the run is launched against.
  const res = http.get(\`\${__ENV.BASE_URL}/health\`, {
    // The name groups this request's metrics, and lets a threshold single it
    // out: http_req_duration{name:GET /health}.
    tags: { name: "GET /health" },
  });

  check(res, {
    "status is 200": (r) => r.status === 200,
  });
}
`;
