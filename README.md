# Margit

> "Thou seekest passage, Tarnished? Show thy permissions, or go no further."
>
> *An authorization-themed nod to Margit, the Fell Omen.*

Margit is a relationship-based access control (ReBAC) service inspired by Google Zanzibar.
You describe object types and how their relations are computed, store relationship tuples
such as `document:readme#owner@user:alice`, and ask questions like *"can bob view readme?"*.

See [ARCHITECTURE.md](ARCHITECTURE.md) for how it works inside.

## Quick start

Requires Go 1.27+.

```powershell
.\build.ps1 -Test          # fill log tags, gofmt, vet, test, build bin\margit.exe
.\bin\margit.exe           # uses config.json (in-memory store, :8080)
```

On Linux/macOS use `./build.sh test`.

### With Postgres

```powershell
docker run -d --rm --name margit-pg -e POSTGRES_PASSWORD=margit -e POSTGRES_DB=margit -p 55432:5432 postgres:17-alpine
$env:MARGIT_PG_DSN = 'postgres://postgres:margit@localhost:55432/margit?sslmode=disable'
```

Set `"store": {"type": "postgres"}` in the config file (or a copy passed with `-config`) and start
`bin\margit.exe`. Tables are created on startup.

### With Docker

```powershell
docker compose up -d --build
docker compose logs -f margit
docker compose down        # add -v to drop the Postgres, Prometheus and Grafana volumes
```

Starts margit on `localhost:8080` backed by a Postgres container, plus Prometheus
(`localhost:9090`, scrapes every 5s) and Grafana (`localhost:3000`, admin/admin; anonymous users can
view). Grafana opens on the provisioned **Margit** dashboard:

| Row | Panels | Source |
|---|---|---|
| Overview, Traffic, Latency | request rate by route/status, errors, check decisions, p50/p95/p99 | margit `/metrics` |
| Container budget | margit CPU, memory, goroutines/GC | margit `/metrics` |
| Containers | CPU cores, memory working set and % of limit per compose service | cAdvisor |
| Postgres | connections (total, by state), active queries, transactions/s, statements/s and mean latency per statement, buffer cache hit ratio, blocks hit/read per second, block read time, rows/s, DB size | postgres-exporter + `pg_stat_statements` |

Per-statement metrics (`margit_statements_*`, labelled by normalized SQL text) come from the custom
exporter query in `docker/postgres/exporter-queries.yml`. `pg_stat_statements` is preloaded by the
compose command and created by `docker/postgres/init.sql` on a fresh volume (on an existing volume
run `CREATE EXTENSION pg_stat_statements` once).

The margit container is limited to
1 CPU and 1 GiB memory (no swap), with `GOMAXPROCS=1` and `GOMEMLIMIT=900MiB` so the Go runtime stays
inside that budget. The image uses `docker/config.json`, driven by environment variables:

| Variable | Default in image | Compose value |
|---|---|---|
| `MARGIT_STORE_TYPE` | `memory` | `postgres` |
| `MARGIT_LOG_LEVEL` | `info` | `info` |
| `MARGIT_PG_DSN` | — | `postgres://margit:margit@postgres:5432/margit?sslmode=disable` |

Standalone in-memory: `docker run --rm --cpus 1 --memory 1g -p 8080:8080 margit:latest`.

Compose also sets `MARGIT_MAX_DEPTH=64` (image default 25) and `MARGIT_BLOOM_EXPECTED=12000000`
(image default 1000000). Postgres memory is tunable from the shell (compose interpolation), so the
same dataset can be run with a cache that fits or one that does not:

| Variable | Default | Meaning |
|---|---|---|
| `PG_SHARED_BUFFERS` | `64MB` | `shared_buffers` |
| `PG_EFFECTIVE_CACHE_SIZE` | `256MB` | planner hint |
| `PG_MEM_LIMIT` | `384m` | Postgres container memory limit (includes its page cache) |

### Load test (k6, Drive dataset)

`loadtest/drive/` models a Google-Drive-like tenant hierarchy. `dataset.js` holds the schema and
deterministic formulas, so the seeder and the test agree on every expected answer without storing
the data in k6.

| Namespace | Relations |
|---|---|
| `group` | `direct_member [user]`, `subgroup [group]`, `member = direct_member + subgroup->member` |
| `role` | `assignee [user]`, `assignee_group [group]`, `member = assignee + assignee_group->member` |
| `org` | `admin [user]`, `member_group [group]`, `member = admin + member_group->member` |
| `folder` | `parent`, `org`, `owner_direct`, `editor_{user,group,role}`, `viewer_{user,group,role}`; `owner = owner_direct + parent->owner`; `editor = owner + editor_user + editor_group->member + editor_role->member + parent->editor`; `viewer = editor + viewer_user + viewer_group->member + viewer_role->member + org->member + parent->viewer` |
| `file` | `parent`, `owner_direct`, `editor_user`, `viewer_user`, `viewer_group`, `banned`, `sharer`; `viewer = (editor + viewer_user + viewer_group->member + parent->viewer) - banned`; `share = editor & sharer` |

Data (4.57M tuples, 900 MB in Postgres): 200k users in 2 of 20k groups each; 100 orgs with admins,
a member group and admin/editor/viewer roles (users + groups); 10k workspaces of
root → 4 folders → 16 folders → 128 files with owners, viewer users/groups/roles, sharers and a
banned editor-group member on every 8th file; 200 folder chains per depth 1–48 plus 80-deep chains;
200 nested group chains per depth 1–32; chains whose root viewer is an 8-deep group chain.

#### Reusable runs: profiles + `run.ps1`

A run is fully described by a profile in `loadtest/profiles/*.json`; `loadtest/drive/traffic.js` executes any
profile and `loadtest/run.ps1` makes the result reproducible:

```powershell
.\loadtest\run.ps1 -Profile smoke                     # 15 s, every kind, answers must be 100% correct
.\loadtest\run.ps1 -Profile realistic                 # verify the profile's rps against its SLOs
.\loadtest\run.ps1 -Profile realistic -Rps 50,100,150 # sweep; one report per rate + summary table
.\loadtest\run.ps1 -Profile checks-only -Duration 60s -Warm
.\loadtest\run.ps1 -Profile .\loadtest\profiles\my.json -Reseed
.\loadtest\run.ps1 -Profile read-deep -FindMax -Warm -Ramp 15s -Duration 45s  # max rate with check p95 < 50 ms
.\loadtest\run.ps1 -Profile realistic -FindMax -Warm -CacheTtlMs 5000          # same, with the response cache on
.\loadtest\run.ps1 -Profile realistic -FindMax -Warm -QueryCacheTtlMs 5000     # same, with the SQL query cache on
.\loadtest\run.ps1 -Profile realistic -Rps 338 -Warm -QueryCacheTtlMs 5000 -BloomExpected 0  # bloom filter off
```

`run.ps1` applies `resources` (margit CPUs/memory/GOMAXPROCS/GOMEMLIMIT, Postgres shared_buffers/
effective_cache_size/memory) through compose env and recreates changed containers, checks the dataset
(≥ 4,573,820 tuples and the 6 namespaces; otherwise runs the idempotent seeder), cold-starts Postgres
(restart + drop OS page cache) when `cold_start` is true, resets `pg_stat_*`, runs k6 at each rate, then
pulls margit/Postgres CPU and memory, connections, TPS, statements/s, mean statement time and server
p95 from Prometheus over the hold window, plus the buffer hit ratio. It writes
`loadtest/results/<profile>-<rps>-<stamp>.{md,json,txt,k6.json}` and `<profile>-<stamp>-summary.md`
(git commit, config, per-op and per-kind×depth latencies, resources, PASS/FAIL) and exits 1 if any SLO
is breached. `-Warm` skips the cold start, `-NoUp` leaves the stack untouched. `-FindMax` starts at the
profile `rps` (or `-Rps`), doubles or halves until it brackets the limit, then bisects to within 5% (min `-Step`, cap
`-MaxRps`); a rate passes when check p95 < `slo.check.p95_ms` (or `-CheckP95Ms`, default 50) with ≤ 1% errors,
≤ 0.5% dropped iterations and ≥ 99.9% correct answers; other ops do not gate. k6 alone:
`docker compose run --rm -e PROFILE=/scripts/profiles/realistic.json -e RPS=80 k6 run /scripts/drive/traffic.js`.

| Profile | Purpose |
|---|---|
| `smoke` | 20 it/s for 15 s over every kind; stack + dataset + correctness check |
| `realistic` | Zipf popularity, 88% check / 3% lookup / 3% expand / 6% write flows; SLO check p95 < 50 ms |
| `read-shallow` | checks only, file-level kinds (1–5 hops: direct, group, role, org, workspace inheritance) |
| `checks-only` | realistic check kind mix (file-level + some chains), checks only |
| `read-deep` | checks only on folder chains 8–48, nested groups 8–32, chain→group chains, some denied |
| `read-write` | realistic without Lookup: 91% check, 3% expand, 6% write flows |
| `write-heavy` | 50% checks, 50% write flows over all write kinds |
| `depth-matrix` | every check kind and depth (1–48), uniform popularity, cold; latency per kind × depth |
| `hot-set` | realistic mix on 4 workspaces / 2 chains per depth / 100 users (fully cached) |
| `heavy` | lookups and expands only at 5 it/s |

Profile fields (all optional except `mix`/`kinds`):

| Field | Meaning |
|---|---|
| `rps`, `ramp`, `duration` | target iterations/s (open model, `ramping-arrival-rate`), ramp-up, hold; `RPS`/`DURATION` env and `-Rps`/`-Duration` override |
| `vus`, `max_vus`, `lookup_limit`, `cold_start` | k6 VU pool, Lookup `limit`, cold start before each run |
| `resources` | `margit: {cpus, memory, gomaxprocs, gomemlimit, cache_ttl_ms, cache_max_entries, query_cache_ttl_ms, query_cache_max_entries, bloom_expected}`, `postgres: {shared_buffers, effective_cache_size, memory}`; `-CacheTtlMs` / `-QueryCacheTtlMs` override the TTLs, `-BloomExpected` the filter size (`0` = off, default 12 M); reports are tagged `<profile>[-cache<ttl>ms][-qcache<ttl>ms][-nobloom]` |
| `popularity` | `workspaces`, `chains`, `users`: `{"dist": "uniform"}`, `{"dist": "zipf", "s": 1.0}` or `{"dist": "hot", "count": 4}` |
| `mix` | weights of `check`, `lookup`, `expand`, `write` |
| `kinds` | per op, weights of request kinds (below); unknown names fail fast |
| `depths` | weights per depth for `folder_chain`, `folder_chain_denied`, `expand_folder_chain` (1,2,4,8,16,32,48), `group_chain` (1–32), `folder_then_group` (4,16,32) |
| `slo` | `check`/`lookup`/`expand`/`write`: `{p95_ms, p99_ms}`; `correct_rate`, `error_rate`, `max_dropped` → k6 thresholds |

Kinds:

- check: `file_view_l2_group`, `file_view_direct`, `file_edit_owner`, `file_edit_team`, `file_edit_role`,
  `file_view_l1`, `file_view_role`, `file_view_role_group`, `file_view_org_admin`, `file_view_org_group`,
  `file_share`, `file_share_denied`, `file_banned`, `file_outsider`, `cross_workspace` (answer not asserted),
  `folder_chain`, `folder_chain_denied`, `group_chain`, `group_chain_denied`, `folder_then_group`, `too_deep` (422)
- lookup: `lookup_my_files`, `lookup_team_files`, `lookup_my_groups`, `lookup_owner_folders`, `lookup_outsider`
- expand: `expand_file_viewers`, `expand_folder_viewers`, `expand_group_chain`, `expand_folder_chain`
- write (write → check → delete flow; the check is reported as `probe`): `write_create_file`, `write_share`,
  `write_join_group`, `write_role_assign`, `write_join_group_chain`, `write_file_in_chain`

Every answer is compared with the value derived from `dataset.js` (`correct` rate). Keep this dataset the only
data in the database: putting a different schema for `group` etc. silently changes every answer.

#### Results

Max it/s with check p95 < 50 ms (margit 1 CPU / 1 GiB, warm, 5 s cache TTLs). Full method, per-profile
CPU, hit ratios and latencies are in [`RESULTS.md`](RESULTS.md).

| Profile | baseline | response cache | query cache | both caches |
|---|---:|---:|---:|---:|
| `read-shallow` | 226 | 188 | 1,062 | 1,094 |
| `checks-only` | 169 | 244 | 581 | 938 |
| `read-deep` | < 10 | < 10 | 469 | 1,300 |
| `read-write` | 232 | 250 | 775 | 775 |
| `write-heavy` | 244 | 253 | 488 | 394 |
| `realistic` | 106 | 125 | 338 | 338 |

Findings:

- The SQL query cache gives 2–4.7× capacity on every profile (80–83% of store reads served from memory,
  98.7% on deep hierarchies). The response cache alone hits only 1–2% of randomized checks, so its column is
  within the ~±15–20% run-to-run variance; it helps only when exact questions repeat (`read-deep`, both caches).
- The Postgres bloom filter is required for every max rate above: with it off (`-BloomExpected 0`) check p95
  rises from 6?42 ms to 0.3?3.7 s at the same rates, because about half of all store reads probe edges that
  do not exist (~75 vs ~36 SQL statements per check without caches). Cost: 4.2 s startup and ~14 MiB.
- Without caches, `realistic` tops out at ~106 it/s, limited by Lookup (p95 ~1.1 s) and ~35 SQL statements
  per request; with the query cache margit's single core becomes the limit.
- Latency is dominated by sequential SQL round trips, not I/O: ~25 `SELECT`s per check at ~46 µs
  server time each. Cold vs hot differs by 5–15%; even a 16 MB buffer pool keeps a 98.9% hit ratio
  because B-tree inner pages stay cached (and the WSL vhdx is cached by the host).
- Positive checks grow linearly with depth (folder chains ~0.35 ms/level, group chains ~0.3 ms/level).
- Negative checks on folder chains grow ~O(d³): `viewer`, `editor` and `owner` each recurse through
  `parent`, and Check has no per-request memoization. Isolated: d=8 → 209 SQL reads / 77 ms,
  d=16 → 1,130 / 0.3 s, d=32 → 7,114 / 1.9 s, d=48 → 22,049 / 5.6 s; under load d=48 exceeded the 30 s
  write timeout. Memoizing `(object, relation, subject)` per request would make this linear; the query cache
  already hides most of it (98.7% hits on `read-deep`, 469 it/s instead of < 10).

## Configuration

`config.json` (path set with `-config`). `${VAR}` is expanded from the environment; unknown fields are rejected.

| Key | Default | Meaning |
|---|---|---|
| `log_level` | `info` | `debug`, `info`, `warn`, `error` |
| `store.type` | `memory` | `memory` or `postgres` |
| `store.postgres.dsn` | — | Postgres connection string |
| `store.postgres.bloom_expected` | `1000000` | Expected keys in the bloom filter; `0` disables it |
| `store.postgres.bloom_fp_rate` | `0.01` | Target bloom false-positive rate |
| `store.query_cache.ttl_ms` | `0` | TTL of cached store reads (tuples by object/subject, namespaces); `0` disables the cache |
| `store.query_cache.max_entries` | `1000000` | Query cache size bound (same eviction as the response cache) |
| `engine.max_depth` | `25` | Max relation nesting depth per evaluation |
| `engine.lookup_default_limit` | `100` | Lookup page size when the request omits `limit` |
| `engine.lookup_max_limit` | `1000` | Upper bound on lookup `limit` (larger values are capped) |
| `engine.response_cache.ttl_ms` | `0` | TTL of cached Check/Expand/Lookup responses; `0` disables the cache |
| `engine.response_cache.max_entries` | `1000000` | Cache size bound; when full, expired entries and then ~1/8 of a shard are evicted |
| `server.addr` | `:8080` | Listen address |
| `server.read_timeout` / `write_timeout` | `10s` / `30s` | HTTP timeouts |
| `server.shutdown_timeout` | `15s` | Time allowed for in-flight requests on shutdown |

## Schema

A **namespace** is an object type. Each relation is either:

- **direct**: `{"types": ["user", "group"]}`. Tuples can be written for it, with subjects of the listed namespaces.
- **computed**: `{"expr": "..."}`. Derived from other relations, never written directly.

Expression syntax:

| Syntax | Meaning |
|---|---|
| `owner` | subjects of relation `owner` on the same object |
| `parent->viewer` | for each subject `p` of `parent`, subjects of `p#viewer` |
| `a + b` | union |
| `a & b` | intersection |
| `a - b` | exclusion (in `a` but not in `b`) |
| `( ... )` | grouping |

`+`, `&` and `-` have equal precedence and apply left to right, so use parentheses when mixing them.
Names match `[A-Za-z_][A-Za-z0-9_]*`. Schemas are validated on save: references must exist,
arrow tuplesets must be direct relations, and computed relations may not form cycles.

Example:

```json
{
  "relations": {
    "parent":      {"types": ["folder"]},
    "owner":       {"types": ["user"]},
    "viewer_group":{"types": ["group"]},
    "banned":      {"types": ["user"]},
    "viewer":      {"expr": "(owner + viewer_group->member + parent->viewer) - banned"}
  }
}
```

## HTTP API

Entities are written as `namespace:id`. Request bodies are JSON (max 1 MB, unknown fields rejected).

| Method | Path | Body | Response |
|---|---|---|---|
| GET | `/healthz` | — | `200 {"status":"ok"}` |
| GET | `/metrics` | — | Prometheus text format (not logged) |
| GET | `/v1/namespaces` | — | `200 {"namespaces":[{"name","relations"}]}` |
| GET | `/v1/namespaces/{name}` | — | `200 {"name","relations"}` |
| PUT | `/v1/namespaces/{name}` | `{"relations":{...}}` | `204` |
| DELETE | `/v1/namespaces/{name}` | — | `204`; `409` if another namespace uses it as a subject type |
| POST | `/v1/tuples` | `{"tuples":[{"object","relation","subject"}]}` | `204` (all-or-nothing validation) |
| POST | `/v1/tuples/delete` | same as above | `204` |
| POST | `/v1/check` | `{"object","relation","subject","consistency"?}` | `200 {"allowed":bool}` |
| POST | `/v1/expand` | `{"object","relation","consistency"?}` | `200 {"subjects":[...]}` |
| POST | `/v1/lookup` | `{"subject","relation","namespace","limit"?,"cursor"?,"consistency"?}` | `200 {"objects":[...],"next_cursor"?}`; pass `next_cursor` back as `cursor` for the next page (absent on the last page) |

### Caches and consistency

With `engine.response_cache.ttl_ms > 0`, Check, Expand and Lookup responses are cached in memory
(`engine.ResponseCache` interface, in-memory sharded implementation `MemResponseCache`). Tuple writes and
deletes do **not** invalidate entries, so a read may be up to `ttl_ms` stale; namespace saves and deletes
clear the whole cache. Errors are never cached. Per request, `consistency` selects the trade-off:

| `consistency` | Behaviour |
|---|---|
| omitted / `minimize_latency` | serve from the cache when present (≤ `ttl_ms` stale) |
| `full` | skip the cache lookup, evaluate against the store and refresh the entry (use right after a write) |

With `store.query_cache.ttl_ms > 0`, the store reads that evaluation repeats on every step (tuples by
object + relation, tuples by subject, namespaces) are cached under `store.CachedStore`. Because entries
are graph edges, not whole answers, many different requests share them. Local tuple writes and deletes
invalidate the affected keys right away, and namespace changes clear the whole cache. Writes from other
instances, and a read racing a write, are bounded by `ttl_ms`. `consistency: "full"` bypasses this cache
too.

Any other `consistency` value returns 400. Metrics: `margit_cache_hits_total`, `margit_cache_misses_total`,
`margit_cache_bypasses_total` (`full` reads) and `margit_cache_entries`, labelled `cache="response"` or
`cache="query"`. The Grafana dashboard has a *Caches* row.

Errors return `{"error": "...", "trace_id": "..."}`:

| Status | Cause |
|---|---|
| 400 | Bad JSON, invalid schema/tuple, unknown namespace or relation |
| 404 | Namespace not found |
| 409 | Namespace still referenced |
| 422 | `engine.max_depth` exceeded |
| 499 / 504 | Client cancelled / deadline exceeded |
| 500 | Internal error (details only in logs) |

Example (PowerShell):

```powershell
$h = @{ContentType='application/json'; UseBasicParsing=$true}
Invoke-WebRequest @h -Method PUT  http://localhost:8080/v1/namespaces/user -Body '{"relations":{}}'
Invoke-WebRequest @h -Method PUT  http://localhost:8080/v1/namespaces/doc  -Body '{"relations":{"owner":{"types":["user"]},"viewer":{"expr":"owner"}}}'
Invoke-WebRequest @h -Method POST http://localhost:8080/v1/tuples -Body '{"tuples":[{"object":"doc:1","relation":"owner","subject":"user:alice"}]}'
Invoke-WebRequest @h -Method POST http://localhost:8080/v1/check  -Body '{"object":"doc:1","relation":"viewer","subject":"user:alice"}'
```

## Logging

One line per event on stdout:

```
2026-10-06T16:39:39.887+05:30 WARN  tag=tag_odelzx pkg=engine trace=bcb7575e3158f93470a58196bc1e6939 msg="tuple rejected" tuple=doc:3#owner@group:eng
```

- **trace**: a random id generated per HTTP request and returned in the `X-Trace-Id` response header.
  Every line produced while serving that request carries it:
  `Select-String margit.log -Pattern 'trace=<id> '`.
- **tag**: unique per call site. Search the code for it to find the line that logged it.
- **pkg**: the Go package that logged the line.

What gets logged at each level:

- **info**: startup config, store/pool setup, bloom loading progress, server start/stop, one `request started` /
  `request finished` pair per request (status, `took`), namespace saves and deletes.
- **warn**: rejected writes/schemas, failed reads/writes, requests slower than 1 s (`slow request`), failed
  response writes.
- **error**: 5xx responses, Postgres errors, panics (with stack), invalid stored relation expressions.
- **debug**: request parameters per handler; `check done` / `expand done` / `lookup done` with `reads` (store
  reads), `depth` (deepest level reached), `cycles` and `took`; the evaluation trace (`following arrow`,
  `direct tuple matched`, `lookup candidates`, `cycle skipped`, `max depth exceeded`); every tuple read with
  its timing; bloom misses; cache hit/miss/bypass. Trace lines are skipped entirely above debug, so they
  cost nothing in production. Set `log_level` to `debug` and filter by `trace=` to follow one request.

When adding a log call, write `"0000"` as the tag; `build.ps1` / `go generate` (via `cmd/tagger`) replaces it
with a unique `tag_xxxxxx`. `tagger -check` fails the build on missing or duplicate tags.

## Development

```powershell
go test ./...                                                  # unit tests (memory store)
$env:MARGIT_PG_DSN = '...'; go test -count=1 -p 1 ./...        # also run store/engine tests on Postgres
```

`-p 1` is required with Postgres because the store and engine tests share tables.

## Layout

| Path | Purpose |
|---|---|
| `main.go` | Load config, open store, build engine, run HTTP server |
| `api/` | HTTP handlers, wire types, server lifecycle |
| `engine/` | Check / Expand / Lookup evaluation; `CachedEngine` response cache |
| `store/` | `Store` interface; memory and Postgres implementations; `CachedStore` query cache |
| `cache/` | TTL cache interface and sharded in-memory implementation shared by both caches |
| `model/` | Entities, namespaces, relations, tuples, validation, errors |
| `ast/` | Relation expression lexer and parser |
| `bloom/` | Bloom filter used by the Postgres store |
| `logger/` | Leveled logger, trace ids |
| `config/` | Config file loading |
| `cmd/tagger/` | Log tag filler/checker |
| `Dockerfile`, `docker-compose.yml`, `docker/` | Container image, compose stack, container config, Prometheus/Grafana provisioning |
| `RESULTS.md` | Published load test results (max QPS per profile, caches, bloom filter) |
| `loadtest/` | `drive/` dataset, seeder and profile-driven k6 script; `profiles/*.json`; `run.ps1` reproducible runner; `results/` (ignored) |
