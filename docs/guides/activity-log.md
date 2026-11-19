---
title: Activity log
description: Record who changed what in a tenant, like spatie/laravel-activitylog, and read it back as a feed.
---

The activity log is the tenant-facing audit trail: "Rahim created branch Dhaka", "Admin assigned
role Cashier to Karim". Services record entries after a change succeeds; tenant admins read them
as a feed.

## Recording

Give the service an optional recorder and call it after the write:

```go
// WithActivity records branch changes in the tenant's activity log.
func (s *Service) WithActivity(recorder *activity.Recorder) *Service {
	s.activity = recorder
	return s
}

s.activity.Record(ctx, activity.Entry{
	TenantID:  tenantID,
	Action:    "branch.created",          // <subject>.<verb>
	Subject:   "branch",
	SubjectID: branch.ID,
	Changes:   map[string]any{"name": branch.Name},
})
```

Wire it in the composition root with `.WithActivity(platform.Activity)`.

- **Nil-safe**: a nil recorder records nothing, so unit tests need no setup.
- **Never fails the caller**: a logging failure is logged, not returned.
- **Context does the rest**: the actor, IP address and request id come from the request context.
- **Redacted**: keys that look secret (`password`, `token`, `secret`, …) are masked; add your own
  with `DEV_DASHBOARD_REDACT_KEYS`.

Record what a tenant admin would want to see — creations, deletions, role and permission
changes, plan changes (`subscription.assigned`, `subscription.cancelled`). Don't record reads.

## Reading

```http
GET /api/v1/activity?per_page=25&cursor=…        (activity.view)
```

A tenant-scoped, newest-first **cursor** feed. Filter with `?subject_type=`, `?subject_id=` and
`?actor_id=`.

## Retention

The `activity.prune` task deletes entries older than `ACTIVITY_RETENTION_DAYS` (default 365;
`0` keeps everything).

## Not the same as the dashboard audit log

| Activity log | Dashboard audit log |
| --- | --- |
| tenant changes made through the API | developer-dashboard actions and sign-ins |
| read by tenant admins at `/api/v1/activity` | read in the dashboard's **Audit log** |
| `activity_log` table | `dev_actions` table |
| `ACTIVITY_RETENTION_DAYS` | `DEV_DASHBOARD_AUDIT_RETENTION_DAYS` |
