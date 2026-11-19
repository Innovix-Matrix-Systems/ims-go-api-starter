---
title: Health and observability
description: Liveness and readiness probes, Prometheus metrics, structured logs, request ids and dashboard telemetry.
---

## Health checks

| Endpoint | Purpose | Answers |
| --- | --- | --- |
| `GET /up` | **liveness** — is the process up? | always `200 {"status":"ok"}` |
| `GET /api/health` | **readiness** — can it serve traffic? | `200`, or `503` when a critical check fails |

`/api/health` runs a registry of checks:

| Check | Critical? |
| --- | --- |
| environment, debug mode, application | yes |
| database (every connection), migrations + River schema | yes |
| cache, queue tables | yes |
| scheduler heartbeat | advisory |
| backups freshness (when enabled) | advisory |

Advisory checks report problems without making the API unready. Results are cached for 30
seconds so a public endpoint can't be used to amplify load; `?fresh=1` from loopback forces a run.
Load balancers and deploy scripts read the **status code**, so readiness fails closed.

```bash
ims health:check      # the same checks from the CLI
```

The dashboard's **Inspector → System** tab shows every check, the runtime (version, Go, uptime)
and re-runs them on demand.

## Metrics

With `METRICS_ENABLED=true`, `GET /metrics` serves a small, dependency-free Prometheus exporter:
request counts and latency by **method, route pattern and status** — low cardinality by design (no
ids, no raw paths). Protect it with `METRICS_TOKEN` (bearer); unset, it answers loopback only.

## Logs

Logs are structured (`log/slog`): text locally, JSON-friendly key/values in production, written
to stdout so systemd's journal collects them (`APP_LOG_DIR` writes files instead). Every request
gets an **id** (`X-Request-ID`: a well-formed one from the client is kept, anything else is
replaced) that appears in the access
log, in every log line of that request, and as `trace_id` in 5xx problems. An access log line
looks roughly like:

```text
level=INFO msg="http request" request_id=01JD… method=POST route=/api/v1/branch status=201 duration=8.4ms user_id=3 tenant_id=1
```

Secrets are redacted from logs and captured payloads (tokens, passwords, anything matching
`DEV_DASHBOARD_REDACT_KEYS`).

## Telemetry (the dashboard)

When the developer dashboard is enabled, a collector captures — sampled with
`DEV_DASHBOARD_SAMPLE_RATE`, bodies redacted and capped at `DEV_DASHBOARD_MAX_BODY_BYTES`:

| Category | What |
| --- | --- |
| `http` | requests: method, route, status, timing, headers, bodies |
| `sql` | queries per request with timing |
| `cache` | hits, misses, writes |
| `queue` | jobs enqueued (linked to the request) |
| `logs` | warn and above |
| exceptions | panics and 5xx with stack |

Choose categories with `DEV_DASHBOARD_CAPTURE`; rows are pruned after
`DEV_DASHBOARD_RETENTION_DAYS`. See the [dashboard tour](../dashboard/overview.md).
