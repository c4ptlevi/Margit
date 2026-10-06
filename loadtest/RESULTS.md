# Load test results: max QPS with check p95 < 50 ms

How much traffic one margit instance sustains on the Drive dataset (4.57 M tuples) per traffic profile,
with and without the two caches. Every number here comes from `loadtest/run.ps1 -FindMax` and can be
reproduced with the commands at the bottom.

## Setup

| | |
|---|---|
| margit | 1 CPU / 1 GiB (`GOMAXPROCS=1`), Postgres store, bloom filter on |
| Postgres | 17 in Docker, `shared_buffers` 64 MB, 384 MiB limit, same host |
| Dataset | Drive (`loadtest/drive/dataset.js`), 4,573,838 tuples, warm (`-Warm`) |
| Load | k6 constant-arrival-rate from `loadtest/drive/traffic.js`, ramp 15 s + hold 45 s per step |
| Pass rule | check p95 < 50 ms, failed ≤ 1%, dropped iterations ≤ max(5, 0.5%), correct answers ≥ 99.9% |
| Search | double the rate until a step fails, then bisect down to ~3% |
| Caches | `-CacheTtlMs 5000` (response cache), `-QueryCacheTtlMs 5000` (query cache) |
| Host | Windows, Docker Desktop (WSL 2); k6, Prometheus and Grafana share the host |

`it/s` is k6 iterations per second. A write flow is 2–3 requests (write → check → delete), so
`server req/s` is higher than it/s for `read-write`, `write-heavy` and `realistic`. CPU columns are
average cores over the hold window; `PG stmts/s` comes from `pg_stat_statements`. Every run at the
reported rate answered ≥ 99.9% of requests correctly (most 100%).

| Profile | Traffic |
|---|---|
| `read-shallow` | checks only, file-level kinds (direct, group, role, org, workspace inheritance; 1–5 hops), Zipf popularity |
| `checks-only` | same popularity and check kinds as `realistic`, checks only |
| `read-deep` | checks only on deep hierarchies: folder chains 8–48, nested groups 8–32; some denied |
| `read-write` | 91% check, 3% expand, 6% write flows |
| `write-heavy` | 50% check, 50% write flows over all write kinds |
| `realistic` | Zipf workspace popularity, mostly checks plus lookups, expands and write flows |

## Max it/s at check p95 < 50 ms

| Profile | baseline | response cache | query cache | both caches |
|---|---:|---:|---:|---:|
| `read-shallow` | **226** | **188** | **1062** | **1094** |
| `checks-only` | **169** | **244** | **581** | **938** |
| `read-deep` | < 10 | < 10 | **469** | **1300** |
| `read-write` | **232** | **250** | **775** | **775** |
| `write-heavy` | **244** | **253** | **488** | **394** |
| `realistic` | **106** | **125** | **338** | **338** |

`< 10` means even 10 it/s failed (check p95 1.7–1.8 s).

## Findings

- **The query cache is the change that matters: 2–4.7× capacity on every profile, and `read-deep`
  goes from unusable to 469 it/s.** A check walks the graph with ~25 sequential `SELECT`s; the query
  cache serves 80–83% of them from memory (98.7% on `read-deep`, where the same folder and group
  chains are walked over and over). Postgres statements per request drop from 31–37 (62 on
  `realistic`) to 5–6 (10 on `realistic`), Postgres CPU drops from ~1.1 to 0.35–0.8 cores, and check
  p95 at baseline-level rates falls from 23–29 ms to 5–7 ms.
- **The response cache alone does nothing measurable here.** With realistic, randomized traffic, an
  identical `(object, relation, subject)` repeats within 5 s for only 1–2% of checks (14.5% on
  `read-deep`), so the differences in that column (188 vs 226, 244 vs 169) are run-to-run variance,
  not cache effect. It only pays off for traffic that repeats exact questions; `read-deep` with both
  caches (66.5% response hits, 1,300 it/s) shows that case.
- **Both caches together** only beat the query cache where exact answers repeat: `read-deep` reaches
  1,300 it/s with 66.5% response hits. `checks-only` 938 vs 581 is not a cache effect (5% response
  hits); the 581 search was cut short by dropped iterations (below). Elsewhere the results match the
  query cache alone within noise (`write-heavy` 394 vs 488).
- **With the caches on, the bottleneck becomes margit's single core** (0.8–0.95 cores at the limit),
  plus a host-level wall around ~1,050–1,100 req/s on this machine where k6, Docker networking and
  margit compete for CPU; past that rate p95 jumps to 350–700 ms and k6 drops iterations.
- **Lookup still dominates `realistic`.** Lookup p95 falls from ~1.1 s to 336 ms with the query cache,
  but it is the reason `realistic` stops at 338 it/s while `read-write` reaches 775. `checks-only`
  with the query cache stopped at 600 it/s on dropped iterations (184, just over 0.5%) while check
  p95 was still 6.7 ms, so its true limit is somewhat higher.
- **Run-to-run variance is ~±15–20%** on this shared host (compare the response-cache column with the
  baseline). Treat differences smaller than that as noise; the query-cache gains are well outside it.
- **Freshness cost.** The query cache invalidates on local writes, so `read-write` / `write-heavy`
  correctness stayed at 100%. Writes made by another instance would be visible after at most
  `ttl_ms`; see *Caches and consistency* in the README.

## Details at the max rate

#### baseline

| Profile | max it/s | server req/s | check p95 | other p95 | margit CPU | PG CPU | PG stmts/s | resp hit | query hit | first fail |
|---|---:|---:|---:|---|---:|---:|---:|---:|---:|---:|
| `read-shallow` | 226 | 226 | 27.4 | - | 0.87 | 1.08 | 8,320 | - | - | 234 |
| `checks-only` | 169 | 169 | 23.2 | - | 0.68 | 0.90 | 5,807 | - | - | 178 |
| `read-deep` | none ≥ 10 | 10 | 1,844.6 (at 10) | - | 0.67 | 1.05 | 8,564 | - | - | 10 |
| `read-write` | 232 | 259 | 29.2 | expand 34, write 5 | 0.79 | 1.15 | 7,935 | - | - | 238 |
| `write-heavy` | 244 | 485 | 40.3 | write 6 | 0.90 | 1.17 | 6,720 | - | - | 253 |
| `realistic` | 106 | 120 | 28.6 | lookup 1063, expand 40, write 5 | 0.70 | 0.94 | 7,444 | - | - | 112 |

#### response cache

| Profile | max it/s | server req/s | check p95 | other p95 | margit CPU | PG CPU | PG stmts/s | resp hit | query hit | first fail |
|---|---:|---:|---:|---|---:|---:|---:|---:|---:|---:|
| `read-shallow` | 188 | 188 | 21.6 | - | 0.84 | 0.95 | 6,846 | 0.7% | - | 196 |
| `checks-only` | 244 | 244 | 26.7 | - | 0.90 | 1.09 | 8,279 | 2.3% | - | 253 |
| `read-deep` | none ≥ 10 | 10 | 1,710.5 (at 10) | - | 0.58 | 0.69 | 6,263 | 14.5% | - | 10 |
| `read-write` | 250 | 279 | 30.2 | expand 39, write 5 | 0.89 | 1.13 | 8,487 | 1.9% | - | 262 |
| `write-heavy` | 253 | 507 | 29.9 | write 5 | 0.87 | 1.04 | 6,903 | 1.3% | - | 262 |
| `realistic` | 125 | 122 | 42.5 | lookup 1097, expand 44, write 6 | 0.86 | 1.18 | 7,520 | 1.3% | - | 132 |

#### query cache

| Profile | max it/s | server req/s | check p95 | other p95 | margit CPU | PG CPU | PG stmts/s | resp hit | query hit | first fail |
|---|---:|---:|---:|---|---:|---:|---:|---:|---:|---:|
| `read-shallow` | 1062 | 1,060 | 31.0 | - | 0.84 | 0.80 | 5,343 | - | 83.3% | 1094 |
| `checks-only` | 581 | 581 | 7.2 | - | 0.64 | 0.52 | 3,354 | - | 80.1% | 600 |
| `read-deep` | 469 | 469 | 30.7 | - | 0.73 | 0.35 | 2,504 | - | 98.7% | 488 |
| `read-write` | 775 | 866 | 12.8 | expand 15, write 10 | 0.77 | 0.74 | 4,842 | - | 81.5% | 800 |
| `write-heavy` | 488 | 977 | 32.4 | write 12 | 0.95 | 1.05 | 6,336 | - | 79.8% | 506 |
| `realistic` | 338 | 380 | 19.3 | lookup 336, expand 17, write 16 | 0.70 | 0.61 | 3,926 | - | 82.0% | 350 |

#### both caches

| Profile | max it/s | server req/s | check p95 | other p95 | margit CPU | PG CPU | PG stmts/s | resp hit | query hit | first fail |
|---|---:|---:|---:|---|---:|---:|---:|---:|---:|---:|
| `read-shallow` | 1094 | 1,085 | 45.2 | - | 0.83 | 0.82 | 5,433 | 3.0% | 83.2% | 1125 |
| `checks-only` | 938 | 936 | 13.6 | - | 0.84 | 0.77 | 4,930 | 5.0% | 81.2% | 975 |
| `read-deep` | 1300 | 1,299 | 39.1 | - | 0.92 | 0.55 | 3,787 | 66.5% | 96.5% | 1350 |
| `read-write` | 775 | 865 | 44.8 | expand 55, write 17 | 0.88 | 0.76 | 4,902 | 4.3% | 80.9% | 800 |
| `write-heavy` | 394 | 780 | 42.8 | write 15 | 0.89 | 0.93 | 5,132 | 1.9% | 78.9% | 412 |
| `realistic` | 338 | 380 | 29.1 | lookup 790, expand 27, write 17 | 0.69 | 0.67 | 4,402 | 2.6% | 83.4% | 350 |
## Reproduce

```powershell
docker compose build margit
foreach ($p in 'read-shallow','checks-only','read-deep','read-write','write-heavy','realistic') {
    .\loadtest\run.ps1 -Profile $p -FindMax -Warm -Ramp 15s -Duration 45s                                       # baseline
    .\loadtest\run.ps1 -Profile $p -FindMax -Warm -Ramp 15s -Duration 45s -CacheTtlMs 5000                      # response cache
    .\loadtest\run.ps1 -Profile $p -FindMax -Warm -Ramp 15s -Duration 45s -QueryCacheTtlMs 5000                 # query cache
    .\loadtest\run.ps1 -Profile $p -FindMax -Warm -Ramp 15s -Duration 45s -CacheTtlMs 5000 -QueryCacheTtlMs 5000 # both
}
```

Per-step reports (`<profile>[-cache…][-qcache…]-<rate>-<stamp>.md/.json`) and per-search summaries
are written to `loadtest/results/` (git-ignored). Baseline runs used the images at `0957ea9` / `401f5a0` (same check path); cache runs
used `401f5a0` + `f1c0e90` (response cache only) and `1db9757` (query cache, both). The response-cache
hit ratios for the response-cache-only runs were read back from Prometheus after the run, because that
image exported the cache metrics without the `cache` label.
