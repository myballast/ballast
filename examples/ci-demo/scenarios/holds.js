import http from "k6/http";
import { check } from "k6";

// The scenario that holds its threshold: it only asks for what is fast.
//
// Make a scenario of this script in Ballast and give it the threshold
//   http_req_duration   p(95)<500
// The address of the target comes from Ballast (BASE_URL), and so do the load and the
// threshold: there is no options block here, and no address.

export default function () {
  const health = http.get(`${__ENV.BASE_URL}/health`);
  check(health, { "health is 200": (r) => r.status === 200 });

  const items = http.get(`${__ENV.BASE_URL}/items`);
  check(items, { "items is 200": (r) => r.status === 200 });
}
