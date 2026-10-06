import http from "k6/http";
import exec from "k6/execution";
import { Counter } from "k6/metrics";
import { NAMESPACE_ORDER, SCHEMA, seedUnits, unitTuples } from "./dataset.js";

const BASE = __ENV.BASE_URL || "http://localhost:8080";
const BATCH = Number(__ENV.SEED_BATCH || 2000);
const WORKSPACES_PER_UNIT = Number(__ENV.SEED_WORKSPACES_PER_UNIT || 4);
const VUS = Number(__ENV.SEED_VUS || 8);
const JSON_HDR = { headers: { "Content-Type": "application/json" }, timeout: "120s" };

const UNITS = seedUnits(WORKSPACES_PER_UNIT);
const tuplesWritten = new Counter("tuples_written");

export const options = {
  setupTimeout: "60s",
  scenarios: {
    seed: {
      executor: "shared-iterations",
      vus: VUS,
      iterations: UNITS.length,
      maxDuration: "2h",
    },
  },
  thresholds: { http_req_failed: ["rate==0"] },
  summaryTrendStats: ["avg", "med", "p(95)", "max", "count"],
};

export function setup() {
  for (const ns of NAMESPACE_ORDER) {
    const r = http.put(`${BASE}/v1/namespaces/${ns}`, JSON.stringify(SCHEMA[ns]), JSON_HDR);
    if (r.status !== 204) throw new Error(`PUT namespace ${ns}: ${r.status} ${r.body}`);
  }
  console.log(`schema saved; ${UNITS.length} seed units`);
}

export default function () {
  const i = exec.scenario.iterationInTest;
  const unit = UNITS[i];
  const tuples = unitTuples(unit);
  for (let off = 0; off < tuples.length; off += BATCH) {
    const chunk = tuples.slice(off, off + BATCH);
    const r = http.post(`${BASE}/v1/tuples`, JSON.stringify({ tuples: chunk }), JSON_HDR);
    if (r.status !== 204) {
      exec.test.abort(`unit ${i} ${JSON.stringify(unit)}: ${r.status} ${r.body}`);
    }
    tuplesWritten.add(chunk.length);
  }
  if (i % 200 === 0) console.log(`unit ${i}/${UNITS.length} ${unit.kind}`);
}
