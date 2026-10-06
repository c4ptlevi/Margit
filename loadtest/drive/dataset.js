export const SCALE = Number(__ENV.SCALE || 1);

export const USERS = 200000 * SCALE;
export const GROUPS = 20000 * SCALE;
export const ORGS = 100;
export const WORKSPACES = 10000 * SCALE;
export const L1 = 4;
export const L2 = 4;
export const FILES = 8;
export const ROLE_NAMES = ["admin", "editor", "viewer"];
export const ROLE_USERS = 10;
export const ORG_ADMINS = 5;

export const CHAIN_DEPTHS = [1, 2, 4, 8, 16, 32, 48];
export const TOO_DEEP = 80;
export const CHAINS = 200;
export const TOO_DEEP_CHAINS = 20;
export const GROUP_DEPTHS = [1, 2, 4, 8, 16, 32];
export const GROUP_CHAINS = 200;
export const MIXED_FOLDER_DEPTHS = [4, 16, 32];
export const MIXED_GROUP_DEPTH = 8;

export const HOT_WORKSPACES = 4;
export const HOT_CHAINS = 2;

const SHARER_OUTSIDER_BASE = 5000000;
const OUTSIDER_BASE = 9000000;

export const SCHEMA = {
  user: { relations: {} },
  group: {
    relations: {
      direct_member: { types: ["user"] },
      subgroup: { types: ["group"] },
      member: { expr: "direct_member + subgroup->member" },
    },
  },
  role: {
    relations: {
      assignee: { types: ["user"] },
      assignee_group: { types: ["group"] },
      member: { expr: "assignee + assignee_group->member" },
    },
  },
  org: {
    relations: {
      admin: { types: ["user"] },
      member_group: { types: ["group"] },
      member: { expr: "admin + member_group->member" },
    },
  },
  folder: {
    relations: {
      parent: { types: ["folder"] },
      org: { types: ["org"] },
      owner_direct: { types: ["user"] },
      editor_user: { types: ["user"] },
      editor_group: { types: ["group"] },
      editor_role: { types: ["role"] },
      viewer_user: { types: ["user"] },
      viewer_group: { types: ["group"] },
      viewer_role: { types: ["role"] },
      owner: { expr: "owner_direct + parent->owner" },
      editor: { expr: "owner + editor_user + editor_group->member + editor_role->member + parent->editor" },
      viewer: {
        expr: "editor + viewer_user + viewer_group->member + viewer_role->member + org->member + parent->viewer",
      },
    },
  },
  file: {
    relations: {
      parent: { types: ["folder"] },
      owner_direct: { types: ["user"] },
      editor_user: { types: ["user"] },
      viewer_user: { types: ["user"] },
      viewer_group: { types: ["group"] },
      banned: { types: ["user"] },
      sharer: { types: ["user"] },
      owner: { expr: "owner_direct + parent->owner" },
      editor: { expr: "owner + editor_user + parent->editor" },
      viewer: { expr: "(editor + viewer_user + viewer_group->member + parent->viewer) - banned" },
      share: { expr: "editor & sharer" },
    },
  },
};

export const NAMESPACE_ORDER = ["user", "group", "role", "org", "folder", "file"];

export const user = (i) => `user:u${i}`;
export const group = (g) => `group:g${g}`;
export const t = (object, relation, subject) => ({ object, relation, subject });

export const outsider = () => user(OUTSIDER_BASE + Math.floor(Math.random() * 1000000));

export const groupMemberOf = (g, slot) => g + GROUPS * (slot % (USERS / GROUPS));
export const userGroups = (u) => [u % GROUPS, (u * 31 + 7) % GROUPS];

export const roleId = (o, r) => `role:o${o}_${r}`;
export const roleUser = (o, ri, a) => (o * 131 + ri * 17 + a * 104729) % USERS;
export const roleGroups = (o, ri) => [(o * 7 + ri * 3 + 1) % GROUPS, (o * 7 + ri * 3 + 1 + GROUPS / 2) % GROUPS];
export const orgAdmin = (o, a) => (o * 997 + a * 7919) % USERS;
export const orgGroup = (o) => (o * 3) % GROUPS;

export const wsOrg = (w) => w % ORGS;
export const rootFolder = (w) => `folder:w${w}`;
export const l1Folder = (w, i) => `folder:w${w}_${i}`;
export const l2Folder = (w, i, j) => `folder:w${w}_${i}_${j}`;
export const fileId = (w, i, j, k) => `file:w${w}_${i}_${j}_${k}`;
export const fid = (w, i, j, k) => ((w * L1 + i) * L2 + j) * FILES + k;
export const rootOwner = (w) => (w * 37 + 1) % USERS;
export const rootEditorGroup = (w) => (w * 11) % GROUPS;
export const l1Viewer = (w, i) => ((w * L1 + i) * 53 + 3) % USERS;
export const l2Group = (w, i, j) => (((w * L1 + i) * L2 + j) * 7 + 5) % GROUPS;
export const fileOwner = (f) => (f * 11 + 13) % USERS;
export const fileViewer = (f) => (f * 17 + 29) % USERS;
export const bannedUser = (w) => groupMemberOf(rootEditorGroup(w), w);

export const chainFolder = (d, c, l) => `folder:c${d}_${c}_${l}`;
export const chainFile = (d, c) => `file:c${d}_${c}`;
export const chainOwner = (d, c) => ((d * 1000 + c) * 29 + 11) % USERS;

export const nestedGroup = (d, c, l) => `group:n${d}_${c}_${l}`;
export const nestedFile = (d, c) => `file:n${d}_${c}`;
export const nestedMember = (d, c) => ((d * 1000 + c) * 13 + 7) % USERS;

export function userTuples(u) {
  return userGroups(u).map((g) => t(group(g), "direct_member", user(u)));
}

export function orgTuples(o) {
  const out = [];
  for (let a = 0; a < ORG_ADMINS; a++) out.push(t(`org:o${o}`, "admin", user(orgAdmin(o, a))));
  out.push(t(`org:o${o}`, "member_group", group(orgGroup(o))));
  ROLE_NAMES.forEach((r, ri) => {
    for (let a = 0; a < ROLE_USERS; a++) out.push(t(roleId(o, r), "assignee", user(roleUser(o, ri, a))));
    for (const g of roleGroups(o, ri)) out.push(t(roleId(o, r), "assignee_group", group(g)));
  });
  return out;
}

export function workspaceTuples(w) {
  const o = wsOrg(w);
  const root = rootFolder(w);
  const out = [
    t(root, "owner_direct", user(rootOwner(w))),
    t(root, "editor_group", group(rootEditorGroup(w))),
    t(root, "editor_role", roleId(o, "editor")),
    t(root, "org", `org:o${o}`),
  ];
  for (let i = 0; i < L1; i++) {
    const f1 = l1Folder(w, i);
    out.push(t(f1, "parent", root), t(f1, "viewer_user", user(l1Viewer(w, i))));
    if (i === 0) out.push(t(f1, "viewer_role", roleId(o, "viewer")));
    for (let j = 0; j < L2; j++) {
      const f2 = l2Folder(w, i, j);
      out.push(t(f2, "parent", f1), t(f2, "viewer_group", group(l2Group(w, i, j))));
      for (let k = 0; k < FILES; k++) {
        const f = fid(w, i, j, k);
        const file = fileId(w, i, j, k);
        out.push(t(file, "parent", f2), t(file, "owner_direct", user(fileOwner(f))));
        if (k % 2 === 0) out.push(t(file, "viewer_user", user(fileViewer(f))));
        if (k === 0) out.push(t(file, "sharer", user(fileOwner(f))));
        if (k === 1) out.push(t(file, "sharer", user(SHARER_OUTSIDER_BASE + f)));
        if (k === FILES - 1) out.push(t(file, "banned", user(bannedUser(w))));
      }
    }
  }
  return out;
}

export function chainTuples(d, c) {
  const out = [t(chainFolder(d, c, 0), "owner_direct", user(chainOwner(d, c)))];
  for (let l = 1; l < d; l++) out.push(t(chainFolder(d, c, l), "parent", chainFolder(d, c, l - 1)));
  out.push(t(chainFile(d, c), "parent", chainFolder(d, c, d - 1)));
  if (MIXED_FOLDER_DEPTHS.includes(d)) {
    out.push(t(chainFolder(d, c, 0), "viewer_group", nestedGroup(MIXED_GROUP_DEPTH, c, 0)));
  }
  return out;
}

export function nestedTuples(d, c) {
  const out = [];
  for (let l = 0; l + 1 < d; l++) out.push(t(nestedGroup(d, c, l), "subgroup", nestedGroup(d, c, l + 1)));
  out.push(t(nestedGroup(d, c, d - 1), "direct_member", user(nestedMember(d, c))));
  out.push(t(nestedFile(d, c), "viewer_group", nestedGroup(d, c, 0)));
  return out;
}

export function seedUnits(batchWorkspaces) {
  const units = [];
  const userBatch = 500;
  for (let u = 0; u < USERS; u += userBatch) {
    units.push({ kind: "users", from: u, to: Math.min(u + userBatch, USERS) });
  }
  units.push({ kind: "orgs" });
  for (const d of GROUP_DEPTHS) units.push({ kind: "nested", d });
  for (const d of [...CHAIN_DEPTHS, TOO_DEEP]) units.push({ kind: "chains", d });
  for (let w = 0; w < WORKSPACES; w += batchWorkspaces) {
    units.push({ kind: "workspaces", from: w, to: Math.min(w + batchWorkspaces, WORKSPACES) });
  }
  return units;
}

export function unitTuples(unit) {
  const out = [];
  switch (unit.kind) {
    case "users":
      for (let u = unit.from; u < unit.to; u++) out.push(...userTuples(u));
      break;
    case "orgs":
      for (let o = 0; o < ORGS; o++) out.push(...orgTuples(o));
      break;
    case "nested":
      for (let c = 0; c < GROUP_CHAINS; c++) out.push(...nestedTuples(unit.d, c));
      break;
    case "chains": {
      const n = unit.d === TOO_DEEP ? TOO_DEEP_CHAINS : CHAINS;
      for (let c = 0; c < n; c++) out.push(...chainTuples(unit.d, c));
      break;
    }
    case "workspaces":
      for (let w = unit.from; w < unit.to; w++) out.push(...workspaceTuples(w));
      break;
  }
  return out;
}
