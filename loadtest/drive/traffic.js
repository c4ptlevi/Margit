import http from "k6/http";
import { Rate, Trend } from "k6/metrics";
import * as D from "./dataset.js";

const PROFILE_PATH = __ENV.PROFILE || "/scripts/profiles/realistic.json";
const P = JSON.parse(open(PROFILE_PATH));
const BASE = __ENV.BASE_URL || "http://localhost:8080";
const RPS = Number(__ENV.RPS || P.rps);
const RAMP = __ENV.RAMP || P.ramp || "30s";
const HOLD = __ENV.DURATION || P.duration || "120s";
const RUN_ID = __ENV.RUN_ID || `${P.name}-${RPS}`;
const LOOKUP_LIMIT = P.lookup_limit || 20;
const SLO = P.slo || {};
const JSON_HDR = { "Content-Type": "application/json" };

const rand = (n) => Math.floor(Math.random() * n);
const slot = () => rand(D.USERS / D.GROUPS);

function zipfCdf(n, s) {
  const cdf = new Float64Array(n);
  let sum = 0;
  for (let r = 0; r < n; r++) {
    sum += 1 / Math.pow(r + 1, s);
    cdf[r] = sum;
  }
  for (let r = 0; r < n; r++) cdf[r] /= sum;
  return cdf;
}

function sampleCdf(cdf) {
  const x = Math.random();
  let lo = 0;
  let hi = cdf.length - 1;
  while (lo < hi) {
    const mid = (lo + hi) >> 1;
    if (cdf[mid] < x) lo = mid + 1;
    else hi = mid;
  }
  return lo;
}

function sampler(n, spec, stride) {
  const s = spec || { dist: "uniform" };
  if (s.dist === "uniform") return () => rand(n);
  if (s.dist === "hot") return () => rand(Math.min(n, s.count || 1));
  if (s.dist === "zipf") {
    const cdf = zipfCdf(n, s.s === undefined ? 1 : s.s);
    return () => (sampleCdf(cdf) * stride) % n;
  }
  throw new Error(`unknown popularity dist ${JSON.stringify(s)}`);
}

const POP = P.popularity || {};
const ws = sampler(D.WORKSPACES, POP.workspaces, 7919);
const chain = sampler(D.CHAINS, POP.chains, 1);
const anyUser = sampler(D.USERS, POP.users, 7919);

function weighted(entries) {
  const list = entries.filter((e) => e.weight > 0);
  const total = list.reduce((s, x) => s + x.weight, 0);
  if (total === 0) return null;
  return () => {
    let r = Math.random() * total;
    for (const x of list) {
      r -= x.weight;
      if (r < 0) return x.value;
    }
    return list[list.length - 1].value;
  };
}

function depthPicker(name, allowed) {
  const spec = (P.depths || {})[name];
  if (!spec) throw new Error(`profile.depths.${name} is required by a selected kind`);
  const entries = Object.entries(spec).map(([d, w]) => {
    const depth = Number(d);
    if (!allowed.includes(depth)) throw new Error(`profile.depths.${name}: depth ${d} not seeded (have ${allowed})`);
    return { weight: w, value: depth };
  });
  return { pick: weighted(entries), depths: entries.filter((e) => e.weight > 0).map((e) => e.value) };
}

function fileIn(w) {
  const i = rand(D.L1);
  const j = rand(D.L2);
  const k = rand(D.FILES - 1);
  return { i, j, k, id: D.fileId(w, i, j, k), f: D.fid(w, i, j, k) };
}

const newUser = () => `user:r${__VU}_${__ITER}`;
const newFile = () => `file:r${__VU}_${__ITER}`;

const check = (object, relation, subject, expect) => ({ path: "/v1/check", body: { object, relation, subject }, expect });
const lookup = (subject, relation, namespace, nonEmpty) => ({
  path: "/v1/lookup",
  body: { subject, relation, namespace, limit: LOOKUP_LIMIT },
  nonEmpty,
});
const expand = (object, relation, contains) => ({ path: "/v1/expand", body: { object, relation }, contains });
const flow = (tuples, probe) => ({ flow: true, tuples, probe });

const CATALOG = {
  check: {
    file_view_l2_group: () => {
      const w = ws();
      const x = fileIn(w);
      return check(x.id, "viewer", D.user(D.groupMemberOf(D.l2Group(w, x.i, x.j), slot())), true);
    },
    file_view_direct: () => {
      const w = ws();
      const x = fileIn(w);
      const k = 2 * rand(D.FILES / 2);
      return check(D.fileId(w, x.i, x.j, k), "viewer", D.user(D.fileViewer(D.fid(w, x.i, x.j, k))), true);
    },
    file_edit_owner: () => {
      const x = fileIn(ws());
      return check(x.id, "editor", D.user(D.fileOwner(x.f)), true);
    },
    file_edit_team: () => {
      const w = ws();
      return check(fileIn(w).id, "editor", D.user(D.groupMemberOf(D.rootEditorGroup(w), slot())), true);
    },
    file_edit_role: () => {
      const w = ws();
      return check(fileIn(w).id, "editor", D.user(D.roleUser(D.wsOrg(w), 1, rand(D.ROLE_USERS))), true);
    },
    file_view_l1: () => {
      const w = ws();
      const x = fileIn(w);
      return check(x.id, "viewer", D.user(D.l1Viewer(w, x.i)), true);
    },
    file_view_role: () => {
      const w = ws();
      const x = fileIn(w);
      return check(D.fileId(w, 0, x.j, x.k), "viewer", D.user(D.roleUser(D.wsOrg(w), 2, rand(D.ROLE_USERS))), true);
    },
    file_view_role_group: () => {
      const w = ws();
      const x = fileIn(w);
      const g = D.roleGroups(D.wsOrg(w), 2)[rand(2)];
      return check(D.fileId(w, 0, x.j, x.k), "viewer", D.user(D.groupMemberOf(g, slot())), true);
    },
    file_view_org_admin: () => {
      const w = ws();
      return check(fileIn(w).id, "viewer", D.user(D.orgAdmin(D.wsOrg(w), rand(D.ORG_ADMINS))), true);
    },
    file_view_org_group: () => {
      const w = ws();
      return check(fileIn(w).id, "viewer", D.user(D.groupMemberOf(D.orgGroup(D.wsOrg(w)), slot())), true);
    },
    file_share: () => {
      const w = ws();
      const x = fileIn(w);
      return check(D.fileId(w, x.i, x.j, 0), "share", D.user(D.fileOwner(D.fid(w, x.i, x.j, 0))), true);
    },
    file_share_denied: () => {
      const w = ws();
      const x = fileIn(w);
      return check(D.fileId(w, x.i, x.j, 1), "share", D.user(D.fileOwner(D.fid(w, x.i, x.j, 1))), false);
    },
    file_banned: () => {
      const w = ws();
      return check(D.fileId(w, rand(D.L1), rand(D.L2), D.FILES - 1), "viewer", D.user(D.bannedUser(w)), false);
    },
    file_outsider: () => check(fileIn(ws()).id, "viewer", D.outsider(), false),
    cross_workspace: () => {
      const other = ws();
      const y = fileIn(other);
      return check(fileIn(ws()).id, "viewer", D.user(D.groupMemberOf(D.l2Group(other, y.i, y.j), slot())));
    },
    folder_chain: (d) => {
      const c = chain();
      return check(D.chainFile(d, c), "viewer", D.user(D.chainOwner(d, c)), true);
    },
    folder_chain_denied: (d) => check(D.chainFile(d, chain()), "viewer", D.outsider(), false),
    group_chain: (d) => {
      const c = chain();
      return check(D.nestedFile(d, c), "viewer", D.user(D.nestedMember(d, c)), true);
    },
    group_chain_denied: (d) => check(D.nestedFile(d, chain()), "viewer", D.outsider(), false),
    folder_then_group: (d) => {
      const c = chain();
      return check(D.chainFile(d, c), "viewer", D.user(D.nestedMember(D.MIXED_GROUP_DEPTH, c)), true);
    },
    too_deep: () => {
      const c = rand(D.TOO_DEEP_CHAINS);
      return Object.assign(check(D.chainFile(D.TOO_DEEP, c), "viewer", D.user(D.chainOwner(D.TOO_DEEP, c))), { status: 422 });
    },
  },
  lookup: {
    lookup_my_files: () => lookup(D.user(D.fileOwner(fileIn(ws()).f)), "viewer", "file", true),
    lookup_team_files: () => {
      const w = ws();
      const x = fileIn(w);
      return lookup(D.user(D.groupMemberOf(D.l2Group(w, x.i, x.j), slot())), "viewer", "file", true);
    },
    lookup_my_groups: () => lookup(D.user(anyUser()), "member", "group", true),
    lookup_owner_folders: () => lookup(D.user(D.rootOwner(ws())), "viewer", "folder", true),
    lookup_outsider: () => lookup(D.outsider(), "viewer", "file", false),
  },
  expand: {
    expand_file_viewers: () => {
      const x = fileIn(ws());
      return expand(x.id, "viewer", D.user(D.fileOwner(x.f)));
    },
    expand_folder_viewers: () => {
      const w = ws();
      return expand(D.rootFolder(w), "viewer", D.user(D.rootOwner(w)));
    },
    expand_group_chain: (d) => {
      const c = chain();
      return expand(D.nestedGroup(d, c, 0), "member", D.user(D.nestedMember(d, c)));
    },
    expand_folder_chain: (d) => {
      const c = chain();
      return expand(D.chainFile(d, c), "viewer", D.user(D.chainOwner(d, c)));
    },
  },
  write: {
    write_create_file: () => {
      const w = ws();
      const f = newFile();
      const owner = D.user(D.groupMemberOf(D.rootEditorGroup(w), slot()));
      return flow([D.t(f, "parent", D.l2Folder(w, rand(D.L1), rand(D.L2))), D.t(f, "owner_direct", owner)], {
        object: f,
        relation: "viewer",
        subject: D.user(D.rootOwner(w)),
      });
    },
    write_share: () => {
      const x = fileIn(ws());
      const u = newUser();
      return flow([D.t(x.id, "viewer_user", u)], { object: x.id, relation: "viewer", subject: u });
    },
    write_join_group: () => {
      const w = ws();
      const x = fileIn(w);
      const u = newUser();
      return flow([D.t(D.group(D.l2Group(w, x.i, x.j)), "direct_member", u)], { object: x.id, relation: "viewer", subject: u });
    },
    write_role_assign: () => {
      const w = ws();
      const u = newUser();
      return flow([D.t(D.roleId(D.wsOrg(w), "editor"), "assignee", u)], { object: fileIn(w).id, relation: "editor", subject: u });
    },
    write_join_group_chain: (d) => {
      const c = chain();
      const u = newUser();
      return flow([D.t(D.nestedGroup(d, c, d - 1), "direct_member", u)], { object: D.nestedFile(d, c), relation: "viewer", subject: u });
    },
    write_file_in_chain: (d) => {
      const c = chain();
      const f = newFile();
      return flow([D.t(f, "parent", D.chainFolder(d, c, d - 1))], { object: f, relation: "viewer", subject: D.user(D.chainOwner(d, c)) });
    },
  },
};

const DEPTH_SOURCE = {
  folder_chain: ["folder_chain", D.CHAIN_DEPTHS],
  folder_chain_denied: ["folder_chain_denied", D.CHAIN_DEPTHS],
  group_chain: ["group_chain", D.GROUP_DEPTHS],
  group_chain_denied: ["group_chain", D.GROUP_DEPTHS],
  folder_then_group: ["folder_then_group", D.MIXED_FOLDER_DEPTHS],
  expand_group_chain: ["group_chain", D.GROUP_DEPTHS],
  expand_folder_chain: ["expand_folder_chain", D.CHAIN_DEPTHS],
  write_join_group_chain: ["group_chain", D.GROUP_DEPTHS],
  write_file_in_chain: ["folder_chain", D.CHAIN_DEPTHS],
};

const KINDS = [];
const latName = (name, d) => (d === "-" ? `lat_${name}` : `lat_${name}_d${d}`);
const correct = new Rate("correct");

function buildOp(op) {
  const weights = (P.kinds || {})[op] || {};
  const entries = Object.entries(weights).map(([name, weight]) => {
    const gen = CATALOG[op][name];
    if (!gen) throw new Error(`profile.kinds.${op}.${name}: unknown kind (have ${Object.keys(CATALOG[op])})`);
    const src = DEPTH_SOURCE[name];
    const depth = src ? depthPicker(src[0], src[1]) : null;
    const k = { op, name, gen, depth, lat: {}, ok: new Rate(`ok_${name}`) };
    for (const d of depth ? depth.depths : ["-"]) k.lat[d] = new Trend(latName(name, d), true);
    if (weight > 0) KINDS.push(k);
    return { weight, value: k };
  });
  return weighted(entries);
}

const pickOp = weighted(
  ["check", "lookup", "expand", "write"].map((op) => {
    const weight = (P.mix || {})[op] || 0;
    const value = buildOp(op);
    if (weight > 0 && !value) throw new Error(`profile.mix.${op} > 0 but profile.kinds.${op} selects no kinds`);
    return { weight, value };
  }),
);
if (!pickOp) throw new Error("profile.mix selects no operations");

function send(k, d, op, path, body, expectStatus) {
  const res = http.post(`${BASE}${path}`, JSON.stringify(body), {
    headers: JSON_HDR,
    timeout: "60s",
    responseCallback: http.expectedStatuses(expectStatus || 200, 204),
    tags: { kind: k.name, op, name: op, depth: String(d) },
  });
  k.lat[d].add(res.timings.duration);
  return res;
}

function run(k) {
  const d = k.depth ? k.depth.pick() : "-";
  const q = k.depth ? k.gen(d) : k.gen();
  let good;
  if (q.flow) {
    const w = send(k, d, "write", "/v1/tuples", { tuples: q.tuples });
    const c = send(k, d, "probe", "/v1/check", { ...q.probe, consistency: "full" });
    const del = send(k, d, "delete", "/v1/tuples/delete", { tuples: q.tuples });
    good = w.status === 204 && del.status === 204 && c.status === 200 && c.json("allowed") === true;
  } else {
    const res = send(k, d, k.op, q.path, q.body, q.status);
    if (q.status) good = res.status === q.status;
    else if (res.status !== 200) good = false;
    else if (k.op === "check") good = q.expect === undefined || res.json("allowed") === q.expect;
    else if (k.op === "lookup") {
      const n = res.json("objects").length;
      good = n <= LOOKUP_LIMIT && n >= (q.nonEmpty ? 1 : 0);
    } else good = res.json("subjects").includes(q.contains);
  }
  correct.add(good, { op: k.op });
  k.ok.add(good);
}

const thresholds = {
  correct: [`rate>=${SLO.correct_rate === undefined ? 1 : SLO.correct_rate}`],
  http_req_failed: [`rate<=${SLO.error_rate === undefined ? 0.01 : SLO.error_rate}`],
};
for (const op of ["check", "lookup", "expand", "write"]) {
  const s = SLO[op] || {};
  const t = [];
  if (s.p95_ms !== undefined) t.push(`p(95)<${s.p95_ms}`);
  if (s.p99_ms !== undefined) t.push(`p(99)<${s.p99_ms}`);
  thresholds[`http_req_duration{op:${op}}`] = t.length ? t : ["p(95)>=0"];
}
for (const op of ["delete", "probe"]) thresholds[`http_req_duration{op:${op}}`] = ["p(95)>=0"];
if (SLO.max_dropped !== undefined) thresholds.dropped_iterations = [`count<=${SLO.max_dropped}`];

export const options = {
  scenarios: {
    traffic: {
      executor: "ramping-arrival-rate",
      startRate: Math.max(1, Math.round(RPS / 10)),
      timeUnit: "1s",
      preAllocatedVUs: P.vus || 100,
      maxVUs: P.max_vus || 600,
      stages: [
        { target: RPS, duration: RAMP },
        { target: RPS, duration: HOLD },
        { target: 0, duration: "5s" },
      ],
    },
  },
  thresholds,
  summaryTrendStats: ["avg", "med", "p(95)", "p(99)", "max", "count"],
};

export default function () {
  const k = pickOp()();
  run(k);
}

const pad = (s, n) => String(s).padEnd(n);
const num = (v) => (v === undefined || v === null ? "-" : v.toFixed(1));

export function handleSummary(data) {
  const m = (k) => data.metrics[k];
  const v = (k, s) => (m(k) ? m(k).values[s] : undefined);
  const rows = [];
  for (const k of KINDS) {
    for (const d of Object.keys(k.lat)) {
      const t = m(latName(k.name, d));
      if (!t) continue;
      rows.push({ op: k.op, kind: k.name, depth: d, n: t.values.count, avg: t.values.avg, p95: t.values["p(95)"], p99: t.values["p(99)"], max: t.values.max, ok: v(`ok_${k.name}`, "rate") });
    }
  }
  const ops = {};
  for (const op of ["check", "lookup", "expand", "write", "probe", "delete"]) {
    const t = m(`http_req_duration{op:${op}}`);
    if (t && t.values.count) ops[op] = { n: t.values.count, p95: t.values["p(95)"], p99: t.values["p(99)"], slo: SLO[op] || null };
  }
  const total = {
    reqs: v("http_reqs", "count"),
    rate: v("http_reqs", "rate"),
    p95: v("http_req_duration", "p(95)"),
    p99: v("http_req_duration", "p(99)"),
    failed: v("http_req_failed", "rate") || 0,
    dropped: v("dropped_iterations", "count") || 0,
    correct: v("correct", "rate"),
  };
  const breached = Object.entries(data.metrics)
    .filter(([, x]) => x.thresholds && Object.values(x.thresholds).some((t) => !t.ok))
    .map(([name]) => name);
  const head = `${pad("op", 8)}${pad("kind", 24)}${pad("depth", 7)}${pad("n", 7)}${pad("avg", 9)}${pad("p95", 9)}${pad("p99", 9)}${pad("max", 9)}ok`;
  const lines = rows.map((r) => `${pad(r.op, 8)}${pad(r.kind, 24)}${pad(r.depth, 7)}${pad(r.n, 7)}${pad(num(r.avg), 9)}${pad(num(r.p95), 9)}${pad(num(r.p99), 9)}${pad(num(r.max), 9)}${r.ok === undefined ? "-" : (r.ok * 100).toFixed(1) + "%"}`);
  const opLines = Object.entries(ops).map(([op, o]) => `${pad(op, 8)}n=${o.n} p95=${num(o.p95)} p99=${num(o.p99)}${o.slo ? ` slo=${JSON.stringify(o.slo)}` : ""}`);
  const text = [
    `\nprofile=${P.name} rps=${RPS} ramp=${RAMP} hold=${HOLD} run=${RUN_ID}`,
    head,
    ...lines,
    "",
    ...opLines,
    `total   reqs=${total.reqs} rate=${num(total.rate)}/s p95=${num(total.p95)} p99=${num(total.p99)} failed=${num(total.failed * 100)}% dropped=${total.dropped} correct=${num((total.correct || 0) * 100)}%`,
    `result  ${breached.length ? "FAIL " + breached.join(", ") : "PASS"}`,
    "",
  ].join("\n");
  return {
    stdout: text,
    [`/results/${RUN_ID}.k6.json`]: JSON.stringify({ profile: P, rps: RPS, hold: HOLD, ops, total, rows, breached }, null, 1),
  };
}
