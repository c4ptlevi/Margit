import http from "k6/http";
import { check } from "k6";

const BASE = __ENV.BASE_URL || "http://localhost:8080";
const USERS = 1000;
const GROUPS = 50;
const DOCS = 2000;
const PEAK_CHECK_RPS = Number(__ENV.PEAK_CHECK_RPS || 1000);
const JSON_HDR = { headers: { "Content-Type": "application/json" } };

const rand = (n) => Math.floor(Math.random() * n);

function ramp(peak) {
  return [
    { target: Math.round(peak * 0.2), duration: "20s" },
    { target: Math.round(peak * 0.5), duration: "30s" },
    { target: peak, duration: "30s" },
    { target: peak, duration: "40s" },
    { target: 0, duration: "10s" },
  ];
}

function arrival(exec, peak, vus) {
  return {
    executor: "ramping-arrival-rate",
    exec,
    startRate: 1,
    timeUnit: "1s",
    preAllocatedVUs: vus,
    maxVUs: vus * 4,
    stages: ramp(peak),
  };
}

export const options = {
  setupTimeout: "120s",
  scenarios: {
    check: arrival("checkTuple", PEAK_CHECK_RPS, 100),
    write: arrival("writeTuple", Math.max(1, Math.round(PEAK_CHECK_RPS / 20)), 20),
    expand: arrival("expandRelation", Math.max(1, Math.round(PEAK_CHECK_RPS / 20)), 20),
    lookup: arrival("lookupObjects", Math.max(1, Math.round(PEAK_CHECK_RPS / 100)), 20),
  },
  thresholds: {
    http_req_failed: ["rate<0.01"],
    "http_req_duration{scenario:check}": ["p(95)<100", "p(99)<250"],
    "http_req_duration{scenario:write}": ["p(95)<200"],
    "http_req_duration{scenario:expand}": ["p(95)<200"],
    "http_req_duration{scenario:lookup}": ["p(95)<1000"],
  },
  summaryTrendStats: ["avg", "min", "med", "p(90)", "p(95)", "p(99)", "max"],
};

function put(path, body) {
  const r = http.put(`${BASE}${path}`, JSON.stringify(body), JSON_HDR);
  if (r.status !== 204) throw new Error(`PUT ${path}: ${r.status} ${r.body}`);
}

function post(path, body, tags) {
  return http.post(`${BASE}${path}`, JSON.stringify(body), Object.assign({ tags }, JSON_HDR));
}

export function setup() {
  put("/v1/namespaces/user", { relations: {} });
  put("/v1/namespaces/group", { relations: { member: { types: ["user"] } } });
  put("/v1/namespaces/doc", {
    relations: {
      owner: { types: ["user"] },
      viewer_group: { types: ["group"] },
      banned: { types: ["user"] },
      viewer: { expr: "(owner + viewer_group->member) - banned" },
    },
  });

  const tuples = [];
  for (let u = 0; u < USERS; u++) {
    tuples.push({ object: `group:g${u % GROUPS}`, relation: "member", subject: `user:u${u}` });
  }
  for (let d = 0; d < DOCS; d++) {
    tuples.push({ object: `doc:d${d}`, relation: "owner", subject: `user:u${(d * 7) % USERS}` });
    tuples.push({ object: `doc:d${d}`, relation: "viewer_group", subject: `group:g${d % GROUPS}` });
    if (d % 10 === 0) {
      tuples.push({ object: `doc:d${d}`, relation: "banned", subject: `user:u${(d * 13) % USERS}` });
    }
  }
  for (let i = 0; i < tuples.length; i += 500) {
    const r = post("/v1/tuples", { tuples: tuples.slice(i, i + 500) }, { name: "setup" });
    if (r.status !== 204) throw new Error(`seed tuples: ${r.status} ${r.body}`);
  }
  console.log(`seeded ${tuples.length} tuples`);
}

export function checkTuple() {
  const r = post(
    "/v1/check",
    { object: `doc:d${rand(DOCS)}`, relation: "viewer", subject: `user:u${rand(USERS)}` },
    { name: "check" },
  );
  check(r, { "check 200": (x) => x.status === 200 });
}

export function writeTuple() {
  const r = post(
    "/v1/tuples",
    { tuples: [{ object: `doc:w${__VU}_${__ITER}`, relation: "owner", subject: `user:u${rand(USERS)}` }] },
    { name: "write" },
  );
  check(r, { "write 204": (x) => x.status === 204 });
}

export function expandRelation() {
  const r = post("/v1/expand", { object: `doc:d${rand(DOCS)}`, relation: "viewer" }, { name: "expand" });
  check(r, { "expand 200": (x) => x.status === 200 });
}

export function lookupObjects() {
  const r = post(
    "/v1/lookup",
    { subject: `user:u${rand(USERS)}`, relation: "viewer", namespace: "doc" },
    { name: "lookup" },
  );
  check(r, { "lookup 200": (x) => x.status === 200 });
}
