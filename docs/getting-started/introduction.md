---
title: Introduction
description: What the IMS Go API Starter is, who it is for, and the ideas behind it.
---

The IMS Go API Starter is a **production-ready, multi-tenant HTTP API** written in Go, organised
the way a Laravel developer expects. You get a working application on day one — authentication,
tenants and branches, roles and permissions, billing with plan limits, settings, feature flags,
files, background jobs, a scheduler, backups — and a clear pattern for adding your own modules.

It ships as **one static binary**. The same `ims` binary serves HTTP, runs queue workers, runs the
scheduler, applies migrations and hosts maintenance commands. A developer dashboard (think Laravel
Telescope plus Horizon plus a little Nova) is embedded in it.

## Who it is for

- **Teams building a SaaS or internal platform** that serves many organisations (tenants), each
  with branches, users and roles.
- **Laravel developers moving to Go** who want Go's performance and deployment story without
  giving up conventions they rely on: controllers, form requests, `can:` middleware, artisan
  commands, queues, scheduled tasks, feature tests.
- **Go developers** who want a codebase where every concern has an obvious home and the "boring"
  parts — errors, pagination, tenancy, audit trails — are already decided.

## What you get

| Area | Highlights |
| --- | --- |
| HTTP | chi router, RFC 9457 problem errors, envelopes, pagination, rate limiting, OpenAPI as the contract |
| Identity | opaque tokens per device, OTP, login throttling, password reset, e-mail verification |
| Tenancy | tenants and branches, explicit `tenant_id` filters that fail closed, staff acting as a tenant or a user, public tenant routes |
| Access | permission catalog in Go, roles per tenant, Super-Admin, route-level checks, a tenant activity log and a system audit log |
| Billing | packages with features and limits, subscriptions, bKash/cash/bank payments, approvals |
| Platform | cache (memory/file/redis/database), River queue, cron scheduler, storage disks, mail, i18n |
| Data | sqlc type-safe SQL, goose migrations, transactions, advisory-lock quotas |
| Ops | health and metrics, backups to S3/R2, systemd deploy with gated migrations, rollback |
| AI agents | an MCP server added as a connector (claude.ai, Claude Code, ChatGPT, Cursor), with built-in OAuth; tools are OpenAPI operations marked `x-mcp` |
| DX | `ims gen module`, `make rename`, `testkit`, hot reload, developer dashboard, AI-agent skills |

## The ideas behind it

1. **Explicit over magic.** No ORM, no global scopes, no reflection-driven wiring. Tenancy,
   transactions and dependencies are visible in the code that uses them. You can read a request
   from the route to the SQL without guessing.
2. **One direction of data flow.** Handler → service → repository → generated SQL. Each layer has
   one job and never skips the next. See [How it works](./how-it-works.mdx).
3. **Fail closed.** A forgotten tenant filter matches no rows. A route without a permission fails
   CI. A failed health check answers 503. Defaults are the safe choice.
4. **Laravel names for Laravel ideas.** Where Laravel has a well-known concept, this codebase uses
   the same shape and often the same name, so the mental model transfers.
5. **The code is the source of truth.** The OpenAPI spec, generated SQL code and these reference
   pages are checked in CI against the code, so they cannot quietly drift.

The full list of deliberate trade-offs — including what was *not* built and why — is in
[Design decisions](../architecture/decisions.md).

## What it is not

- **Not a framework.** It is an application you fork and own. Change anything.
- **Not database-agnostic.** v1 is PostgreSQL only, on purpose (see decision 22).
- **Not a frontend.** It is an API. The only UI is the developer dashboard; your product's admin
  app (billing screens, tenant management for staff) is a client of the API.

Ready? Head to the [Quick start](./quick-start.mdx).
