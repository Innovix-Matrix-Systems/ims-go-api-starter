---
title: Notifications
description: In-app notifications targeted at users, roles, branches, tenants or everyone, with a cursor feed and read state.
---

Notifications are in-app messages — "Your import finished", "Subscription expires in 3 days".
They are stored once and **fanned out** to their audience in the background.

## Creating one

Platform staff create notifications through the internal API (`notification.create`):

```json
POST /api/v1/internal/notification
{
  "event_type": "maintenance.scheduled",
  "priority": "WARNING",
  "title": "Maintenance tonight",
  "body": "The API will be read-only from 01:00 to 01:30.",
  "data": { "window": "01:00-01:30" },
  "group_key": "maintenance-2026-11-21",
  "expires_at": "2026-11-22T00:00:00Z",
  "targets": [{ "type": "TENANT", "id": 7 }, { "type": "ROLE", "id": 3 }]
}
```

| Field | Meaning |
| --- | --- |
| `priority` | `INFO`, `WARNING` or `CRITICAL` |
| `targets` | one or more of `USER`, `ROLE`, `BRANCH`, `TENANT` (with an `id`) or `GLOBAL` |
| `group_key` | collapses repeats of the same event |
| `expires_at` | hidden after this; purged 7 days later by `notifications.purge-expired` |

From code, call the notifications service — billing does this for renewal reminders. Fan-out runs
as a queue job and is idempotent (`INSERT … ON CONFLICT … RETURNING`), so a retried job never
double-counts.

The `notification.enabled` [setting](./settings.md) turns fan-out off per tenant.

## Reading the feed

| Endpoint | Does |
| --- | --- |
| `GET /api/v1/notification/feed?limit=20&cursor=…` | the caller's notifications, newest first, with unread count |
| `POST /api/v1/notification/{id}/read` | mark one read |
| `POST /api/v1/notification/read-all` | mark all read (scoped and idempotent) |

Platform accounts use the same endpoints under `/api/v1/internal/notification/…`. Reads are
**user-scoped** — never branch-scoped — so moving branches doesn't hide notifications. Unread
counters are cached and recomputed from the database on a cache miss.
