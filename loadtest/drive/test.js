import http from "k6/http";
import { Rate, Trend } from "k6/metrics";
import * as D from "./dataset.js";

const BASE = __ENV.BASE_URL || "http://localhost:8080";
const CACHE = __ENV.CACHE || "cold";
const CHECK_RPS = Number(__ENV.CHECK_RPS || 200);
const WRITE_RPS = Number(__ENV.WRITE_RPS || 10);
const DURATION = __ENV.DURATION || "60s";
const DEEP_NEGATIVE = __ENV.DEEP_NEGATIVE === "1";
const FOLDER_NEG_MAX = Number(__ENV.FOLDER_NEG_MAX || 8);
const folderNegOk = (d) => DEEP_NEGATIVE || d <= FOLDER_NEG_MAX;
const JSON_HDR = { "Content-Type": "application/json" };
const OK_STATUS = http.expectedStatuses(200, 204);
const DEEP_STATUS = http.expectedStatuses(422);

const hot = CACHE === "hot";
const RUN = `drive-${CACHE}${DEEP_NEGATIVE ? "-deep" : ""}`;
const rand = (n) => Math.floor(Math.random() * n);
const pick = (a) => a[rand(a.length)];
const ws = () => rand(hot ? D.HOT_WORKSPACES : D.WORKSPACES);
const chain = () => rand(hot ? D.HOT_CHAINS : D.CHAINS);
const slot = () => rand(D.USERS / D.GROUPS);

function fileIn(w, avoidBanned = true) {
  const i = rand(D.L1);
  const j = rand(D.L2);
  const k = rand(avoidBanned ? D.FILES - 1 : D.FILES);
  return { i, j, k, id: D.fileId(w, i, j, k), f: D.fid(w, i, j, k) };
}

const CHECKS = [];
const WRITES = [];
const lat = {};
const correct = new Rate("correct");
const correctBy = {};

function register(list, name, depth, fn) {
  const key = depth === undefined ? name : `${name}_d${depth}`;
  lat[key] = new Trend(`lat_${key}`, true);
  correctBy[key] = new Rate(`ok_${key}`);
  list.push({ key, name, depth: depth === undefined ? "-" : String(depth), fn });
}

function checkCase(name, depth, gen) {
  register(CHECKS, name, depth, (c) => {
    const q = gen();
    const res = call(c, "check", "/v1/check", { object: q.object, relation: q.relation, subject: q.subject }, q.status);
    const good = q.status === 422 ? res.status === 422 : res.status === 200 && res.json("allowed") === q.expect;
    record(c, good);
  });
}

function call(c, op, path, body, status) {
  const res = http.post(`${BASE}${path}`, JSON.stringify(body), {
    headers: JSON_HDR,
    responseCallback: status === 422 ? DEEP_STATUS : OK_STATUS,
    tags: { kind: c.name, depth: c.depth, op, cache: CACHE, name: op },
  });
  lat[c.key].add(res.timings.duration);
  return res;
}

function record(c, good) {
  correct.add(good, { kind: c.name });
  correctBy[c.key].add(good);
}

checkCase("file_owner_direct", 1, () => {
  const x = fileIn(ws());
  return { object: x.id, relation: "owner", subject: D.user(D.fileOwner(x.f)), expect: true };
});
checkCase("file_viewer_direct", 1, () => {
  const w = ws();
  const x = fileIn(w);
  const k = 2 * rand(3);
  const f = D.fid(w, x.i, x.j, k);
  return { object: D.fileId(w, x.i, x.j, k), relation: "viewer", subject: D.user(D.fileViewer(f)), expect: true };
});
checkCase("file_l2_group", 3, () => {
  const w = ws();
  const x = fileIn(w);
  const g = D.l2Group(w, x.i, x.j);
  return { object: x.id, relation: "viewer", subject: D.user(D.groupMemberOf(g, slot())), expect: true };
});
checkCase("file_l1_user", 3, () => {
  const w = ws();
  const x = fileIn(w);
  return { object: x.id, relation: "viewer", subject: D.user(D.l1Viewer(w, x.i)), expect: true };
});
checkCase("file_role_user", 4, () => {
  const w = ws();
  const x = fileIn(w);
  const f = D.fileId(w, 0, x.j, x.k);
  return { object: f, relation: "viewer", subject: D.user(D.roleUser(D.wsOrg(w), 2, rand(D.ROLE_USERS))), expect: true };
});
checkCase("file_role_group", 5, () => {
  const w = ws();
  const x = fileIn(w);
  const g = pick(D.roleGroups(D.wsOrg(w), 2));
  return {
    object: D.fileId(w, 0, x.j, x.k),
    relation: "viewer",
    subject: D.user(D.groupMemberOf(g, slot())),
    expect: true,
  };
});
checkCase("file_root_owner", 4, () => {
  const w = ws();
  return { object: fileIn(w).id, relation: "editor", subject: D.user(D.rootOwner(w)), expect: true };
});
checkCase("file_root_editor_group", 5, () => {
  const w = ws();
  const g = D.rootEditorGroup(w);
  return { object: fileIn(w).id, relation: "editor", subject: D.user(D.groupMemberOf(g, slot())), expect: true };
});
checkCase("file_root_editor_role", 6, () => {
  const w = ws();
  const u = D.roleUser(D.wsOrg(w), 1, rand(D.ROLE_USERS));
  return { object: fileIn(w).id, relation: "editor", subject: D.user(u), expect: true };
});
checkCase("file_org_admin", 5, () => {
  const w = ws();
  const u = D.orgAdmin(D.wsOrg(w), rand(D.ORG_ADMINS));
  return { object: fileIn(w).id, relation: "viewer", subject: D.user(u), expect: true };
});
checkCase("file_org_group", 6, () => {
  const w = ws();
  const g = D.orgGroup(D.wsOrg(w));
  return { object: fileIn(w).id, relation: "viewer", subject: D.user(D.groupMemberOf(g, slot())), expect: true };
});
checkCase("file_banned", 5, () => {
  const w = ws();
  const f = D.fileId(w, rand(D.L1), rand(D.L2), D.FILES - 1);
  return { object: f, relation: "viewer", subject: D.user(D.bannedUser(w)), expect: false };
});
checkCase("file_outsider", 4, () => {
  return { object: fileIn(ws(), false).id, relation: "viewer", subject: D.outsider(), expect: false };
});
checkCase("share_owner", 1, () => {
  const w = ws();
  const x = fileIn(w);
  const f = D.fid(w, x.i, x.j, 0);
  return { object: D.fileId(w, x.i, x.j, 0), relation: "share", subject: D.user(D.fileOwner(f)), expect: true };
});
checkCase("share_outsider_sharer", 1, () => {
  const w = ws();
  const x = fileIn(w);
  const f = D.fid(w, x.i, x.j, 1);
  return { object: D.fileId(w, x.i, x.j, 1), relation: "share", subject: D.user(D.fileOwner(f)), expect: false };
});
for (const d of D.CHAIN_DEPTHS) {
  checkCase("chain_owner", d, () => {
    const c = chain();
    return { object: D.chainFile(d, c), relation: "viewer", subject: D.user(D.chainOwner(d, c)), expect: true };
  });
  if (folderNegOk(d)) checkCase("chain_outsider", d, () => {
    return { object: D.chainFile(d, chain()), relation: "viewer", subject: D.outsider(), expect: false };
  });
}
checkCase("chain_too_deep", D.TOO_DEEP, () => {
  const c = rand(D.TOO_DEEP_CHAINS);
  return { object: D.chainFile(D.TOO_DEEP, c), relation: "viewer", subject: D.user(D.chainOwner(D.TOO_DEEP, c)), status: 422 };
});
for (const d of D.GROUP_DEPTHS) {
  checkCase("nested_group", d, () => {
    const c = chain();
    return { object: D.nestedFile(d, c), relation: "viewer", subject: D.user(D.nestedMember(d, c)), expect: true };
  });
  checkCase("nested_group_outsider", d, () => {
    return { object: D.nestedFile(d, chain()), relation: "viewer", subject: D.outsider(), expect: false };
  });
}
for (const d of D.MIXED_FOLDER_DEPTHS) {
  if (!folderNegOk(d)) continue;
  checkCase("mixed_chain_group", d + D.MIXED_GROUP_DEPTH, () => {
    const c = chain();
    return {
      object: D.chainFile(d, c),
      relation: "viewer",
      subject: D.user(D.nestedMember(D.MIXED_GROUP_DEPTH, c)),
      expect: true,
    };
  });
}

function flow(c, tuple, probe) {
  const sub = { object: probe.object, relation: probe.relation, subject: probe.subject };
  const w = call(c, "write", "/v1/tuples", { tuples: [tuple] });
  const before = call(c, "check", "/v1/check", sub);
  const del = call(c, "delete", "/v1/tuples/delete", { tuples: [tuple] });
  const after = call(c, "check", "/v1/check", sub);
  record(
    c,
    w.status === 204 &&
      del.status === 204 &&
      before.status === 200 &&
      after.status === 200 &&
      before.json("allowed") === true &&
      after.json("allowed") === probe.afterDelete,
  );
}

const newUser = () => `user:x${__VU}_${__ITER}`;
const newFile = () => `file:x${__VU}_${__ITER}`;

register(WRITES, "w_create_file", 4, (c) => {
  const w = ws();
  const f = newFile();
  const parent = D.l2Folder(w, rand(D.L1), rand(D.L2));
  flow(c, D.t(f, "parent", parent), { object: f, relation: "viewer", subject: D.user(D.rootOwner(w)), afterDelete: false });
});
register(WRITES, "w_share_revoke", 1, (c) => {
  const x = fileIn(ws());
  const u = newUser();
  flow(c, D.t(x.id, "viewer_user", u), { object: x.id, relation: "viewer", subject: u, afterDelete: false });
});
register(WRITES, "w_role_assign", 6, (c) => {
  const w = ws();
  const u = newUser();
  flow(c, D.t(D.roleId(D.wsOrg(w), "editor"), "assignee", u), {
    object: fileIn(w).id,
    relation: "editor",
    subject: u,
    afterDelete: false,
  });
});
for (const d of D.GROUP_DEPTHS) {
  register(WRITES, "w_group_join", d, (c) => {
    const ch = chain();
    const u = newUser();
    flow(c, D.t(D.nestedGroup(d, ch, d - 1), "direct_member", u), {
      object: D.nestedFile(d, ch),
      relation: "viewer",
      subject: u,
      afterDelete: false,
    });
  });
}
for (const d of D.CHAIN_DEPTHS) {
  register(WRITES, "w_chain_file", d, (c) => {
    const ch = chain();
    const f = newFile();
    flow(c, D.t(f, "parent", D.chainFolder(d, ch, d - 1)), {
      object: f,
      relation: "viewer",
      subject: D.user(D.chainOwner(d, ch)),
      afterDelete: false,
    });
  });
}

export const options = {
  discardResponseBodies: false,
  scenarios: {
    checks: {
      executor: "constant-arrival-rate",
      exec: "checks",
      rate: CHECK_RPS,
      timeUnit: "1s",
      duration: DURATION,
      preAllocatedVUs: 100,
      maxVUs: 400,
      tags: { cache: CACHE },
    },
    writes: {
      executor: "constant-arrival-rate",
      exec: "writes",
      rate: WRITE_RPS,
      timeUnit: "1s",
      duration: DURATION,
      preAllocatedVUs: 20,
      maxVUs: 100,
      tags: { cache: CACHE },
    },
  },
  thresholds: {
    correct: ["rate==1"],
    http_req_failed: ["rate<0.01"],
  },
  summaryTrendStats: ["avg", "med", "p(95)", "p(99)", "max", "count"],
};

export function checks() {
  const c = pick(CHECKS);
  c.fn(c);
}

export function writes() {
  const c = pick(WRITES);
  c.fn(c);
}

const pad = (s, n) => String(s).padEnd(n);
const num = (v) => (v === undefined ? "-" : v.toFixed(1));

export function handleSummary(data) {
  const rows = [];
  for (const c of [...CHECKS, ...WRITES]) {
    const m = data.metrics[`lat_${c.key}`];
    const ok = data.metrics[`ok_${c.key}`];
    if (!m) continue;
    rows.push({
      kind: c.name,
      depth: c.depth,
      count: m.values.count,
      avg: m.values.avg,
      p95: m.values["p(95)"],
      p99: m.values["p(99)"],
      max: m.values.max,
      correct: ok ? ok.values.rate : undefined,
    });
  }
  const head = `${pad("kind", 24)}${pad("depth", 7)}${pad("n", 7)}${pad("avg", 9)}${pad("p95", 9)}${pad("p99", 9)}${pad("max", 9)}ok`;
  const lines = rows.map(
    (r) =>
      `${pad(r.kind, 24)}${pad(r.depth, 7)}${pad(r.count, 7)}${pad(num(r.avg), 9)}${pad(num(r.p95), 9)}${pad(num(r.p99), 9)}${pad(num(r.max), 9)}${r.correct === undefined ? "-" : (r.correct * 100).toFixed(1) + "%"}`,
  );
  const g = (k, s) => (data.metrics[k] ? data.metrics[k].values[s] : undefined);
  const footer = [
    `cache=${CACHE} deep_negative=${DEEP_NEGATIVE} check_rps=${CHECK_RPS} write_rps=${WRITE_RPS} duration=${DURATION}`,
    `http_reqs=${g("http_reqs", "count")} rate=${num(g("http_reqs", "rate"))}/s failed=${num((g("http_req_failed", "rate") || 0) * 100)}% dropped=${g("dropped_iterations", "count") || 0} correct=${num((g("correct", "rate") || 0) * 100)}%`,
  ];
  const text = [`\nMargit drive load test (${RUN}) latency ms`, head, ...lines, "", ...footer, ""].join("\n");
  return {
    stdout: text,
    [`/results/${RUN}.json`]: JSON.stringify({ cache: CACHE, rows, metrics: data.metrics }, null, 1),
    [`/results/${RUN}.txt`]: text,
  };
}
