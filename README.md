# Margit

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
(`localhost:9090`, scrapes `/metrics` every 5s) and Grafana (`localhost:3000`, admin/admin;
anonymous users can view). Grafana opens on the provisioned **Margit** dashboard: request rate by
route and status, errors, check decisions, p50/p95/p99 latency, and CPU/memory against the
container budget.

The margit container is limited to
1 CPU and 1 GiB memory (no swap), with `GOMAXPROCS=1` and `GOMEMLIMIT=900MiB` so the Go runtime stays
inside that budget. The image uses `docker/config.json`, driven by environment variables:

| Variable | Default in image | Compose value |
|---|---|---|
| `MARGIT_STORE_TYPE` | `memory` | `postgres` |
| `MARGIT_LOG_LEVEL` | `info` | `info` |
| `MARGIT_PG_DSN` | — | `postgres://margit:margit@postgres:5432/margit?sslmode=disable` |

Standalone in-memory: `docker run --rm --cpus 1 --memory 1g -p 8080:8080 margit:latest`.

### Load test (k6)

```powershell
docker compose run --rm k6                              # peak 1000 check/s
docker compose run --rm -e PEAK_CHECK_RPS=2000 k6       # push harder
```

`loadtest/margit.js` seeds 1,000 users, 50 groups and 2,000 docs (~5,200 tuples), then ramps
open-model arrival rates over ~2 min: checks to `PEAK_CHECK_RPS`, writes and expands at 1/20 of it,
lookups at 1/100 (page size `LOOKUP_LIMIT`, default 20). Thresholds: <1% errors, check p95 <100 ms /
p99 <250 ms. Watch the Grafana dashboard while it runs.

Result on the 1 CPU / 1 GiB container with Postgres (default settings, 1,110 req/s peak, 0 errors,
no dropped iterations, ~30 MiB RSS):

| | Unpaginated lookup | Lookup `limit: 20` |
|---|---|---|
| Check p95 / p99 | 13.7 / 39.6 ms | 4.2 / 14.1 ms |
| Write p95 | 11.3 ms | 5.0 ms |
| Expand p95 | 14.6 ms | 5.0 ms |
| Lookup p95 / p99 | 215 / 498 ms | 42 / 102 ms |
| Margit CPU at peak | 0.96 core | 0.93 core |

## Configuration

`config.json` (path set with `-config`). `${VAR}` is expanded from the environment; unknown fields are rejected.

| Key | Default | Meaning |
|---|---|---|
| `log_level` | `info` | `debug`, `info`, `warn`, `error` |
| `store.type` | `memory` | `memory` or `postgres` |
| `store.postgres.dsn` | — | Postgres connection string |
| `store.postgres.bloom_expected` | `1000000` | Expected keys in the bloom filter; `0` disables it |
| `store.postgres.bloom_fp_rate` | `0.01` | Target bloom false-positive rate |
| `engine.max_depth` | `25` | Max relation nesting depth per evaluation |
| `engine.lookup_default_limit` | `100` | Lookup page size when the request omits `limit` |
| `engine.lookup_max_limit` | `1000` | Upper bound on lookup `limit` (larger values are capped) |
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
| POST | `/v1/check` | `{"object","relation","subject"}` | `200 {"allowed":bool}` |
| POST | `/v1/expand` | `{"object","relation"}` | `200 {"subjects":[...]}` |
| POST | `/v1/lookup` | `{"subject","relation","namespace","limit"?,"cursor"?}` | `200 {"objects":[...],"next_cursor"?}`; pass `next_cursor` back as `cursor` for the next page (absent on the last page) |

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
| `engine/` | Check / Expand / Lookup evaluation |
| `store/` | `Store` interface; memory and Postgres implementations |
| `model/` | Entities, namespaces, relations, tuples, validation, errors |
| `ast/` | Relation expression lexer and parser |
| `bloom/` | Bloom filter used by the Postgres store |
| `logger/` | Leveled logger, trace ids |
| `config/` | Config file loading |
| `cmd/tagger/` | Log tag filler/checker |
| `Dockerfile`, `docker-compose.yml`, `docker/` | Container image, compose stack, container config, Prometheus/Grafana provisioning |
| `loadtest/` | k6 load test script |
