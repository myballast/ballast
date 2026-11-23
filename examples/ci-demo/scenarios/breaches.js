import http from "k6/http";
import { check } from "k6";

// The scenario that breaches its threshold: it asks for what takes 800 ms.
//
// Make a scenario of this script in Ballast and give it the same threshold as the other one,
//   http_req_duration   p(95)<500
// and the job that runs it fails, with BREACHED in its log. Nothing is wrong with the
// service: /slow answers after the time it is asked for, which is what makes it a target for
// a demonstration.

export default function () {
  const slow = http.get(`${__ENV.BASE_URL}/slow?ms=800`);
  check(slow, { "slow is 200": (r) => r.status === 200 });
}
