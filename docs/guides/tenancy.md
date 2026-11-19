---
title: Tenancy
description: How tenants and branches are resolved, scoped in SQL, impersonated — and why a forgotten filter fails closed.
---

Every organisation using the API is a **tenant**; each tenant has one or more **branches** (shops,
clinics, offices). Users belong to a tenant and work in one branch at a time. Platform accounts —
the super admin, developers — belong to **no** tenant.

## Isolation is explicit, in SQL

There is no row-level security and no ORM global scope. **Every tenant-scoped query receives and
filters on an explicit `tenant_id`**, and a missing filter is a security bug. That is decision 18:
explicit scoping is the only isolation boundary, and per-module isolation tests enforce it.

```sql
-- single row
WHERE id = @id AND (@all_tenants::boolean OR tenant_id = @tenant_id)
-- lists
WHERE tenant_id = @tenant_id
```

## The scope

Middleware resolves a `tenancy.Scope` for every request in the tenant-resolved route group, and
handlers pass it down explicitly:

```go
scope := tenancy.From(r)            // in a handler
scope := tenancy.FromContext(ctx)   // below the handler, if you must
```

| Use | For |
| --- | --- |
| `scope.Filter()` | single-row reads/writes — rendered as `(@all_tenants OR tenant_id = @tenant_id)` |
| `scope.EffectiveTenantID()` | lists and inserts |
| `scope.EffectiveBranchID()` | branch-scoped lists |

:::tip[Fails closed]
A zero `tenancy.Filter{}` matches **no** tenant rows. Forget to pass the scope and you get empty
results or a 404 — never another tenant's data. Reading every tenant is explicit:
`scope.AllTenants()` / `tenancy.Unscoped()`, allowed only for internal routes or global
impersonation.
:::

## How a request is resolved

**Tenant users** must have a `tenant_id` and an active branch. The branch-belongs-to-tenant check
is cached for five minutes (`branch_check:*`) and invalidated when branches change.

**Impersonators** — the super admin, or anyone with `user.tenant.switch` — choose a tenant per
request with headers:

| Headers | Result |
| --- | --- |
| none | a **global, read-only** view of all tenants (tenant 0); writes are refused with 403 |
| `X-Acting-Tenant: 7` | act as tenant 7 (its default branch) |
| `X-Acting-Tenant: 7` + `X-Acting-Branch: 12` | act as branch 12 of tenant 7 |

**Tenant-exempt routes** (`tenancy.Exempt()`) skip resolution — tenants, permissions, packages,
internal users. `tenancy.InternalOnly` additionally rejects tenant-bound identities, so a granted
system permission alone can never reach cross-tenant endpoints.

**Public** tenant resolution (before login) accepts `X-Tenant-Code` (+ optional `X-Branch-Id`).

## Queue jobs carry the scope

A queue envelope stores `tenant_id` and `branch_id`; the dispatcher restores the scope before the
job handler runs, so a job sees exactly the tenant that enqueued it. Enqueueing a tenant job
without a tenant is refused.

## Proving isolation

Every module that holds tenant data gets an isolation test: a second tenant's admin must get
**404** on read, update and delete of the first tenant's records.

```go
other := app.User(t, testkit.InTenant(app.Tenant(t, "Tenant B")))
app.As(other).Get(fmt.Sprintf("/api/v1/branch/%d", id)).Status(t, http.StatusNotFound)
```

`internal/app/tenancy_isolation_integration_test.go` runs the matrix across modules. See
[Testing](./testing.md).

## Known gap

`tenants.is_active` is stored and shown, but **not yet enforced** at request time — deactivating
a tenant does not block its users today.
