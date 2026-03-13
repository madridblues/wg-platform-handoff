import http from "k6/http";
import { check, sleep } from "k6";

export const options = {
  scenarios: {
    relay_read: {
      executor: "ramping-vus",
      startVUs: 1,
      stages: [
        { duration: "30s", target: 10 },
        { duration: "1m", target: 50 },
        { duration: "30s", target: 0 }
      ]
    }
  },
  thresholds: {
    http_req_failed: ["rate<0.01"],
    http_req_duration: ["p(95)<500"]
  }
};

const BASE = __ENV.BASE_URL || "http://localhost:8080";

export default function () {
  const relayRes = http.get(`${BASE}/app/v1/relays`);
  check(relayRes, {
    "relays status 200": (r) => r.status === 200 || r.status === 304
  });

  const addrsRes = http.get(`${BASE}/app/v1/api-addrs`);
  check(addrsRes, {
    "api-addrs status 200": (r) => r.status === 200
  });

  sleep(1);
}
