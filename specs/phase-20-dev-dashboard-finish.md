# Phase 20 — Developer dashboard finish: inspector, commands, form kit, service layer

**Status:** Done (2026-11-19)
**Source:** project-owner review of the dev dashboard (compared with the Laravel version: Settings,
Inspector and Commands screens), plus the two items Phase 19 deferred (M53/M54, porting old
integration tests to testkit)
**Depends on:** Phases 9, 11, 19

## Goal

Make the developer dashboard the one place to look at and operate a running instance —
`route:list`, roles, config, packages, and the everyday maintenance commands — with form controls
that match the theme, and give the dashboard code the same service-layer shape as the API modules.

## Scope checklist

### UI kit
- [x] `components/inputs.tsx`: `Select` (keyboard navigation, filter box for long lists, hints),
      `DatePicker` (calendar, min/max, Today/Clear), `TimePicker`. Popovers close on Escape and
      outside clicks, flip to the right edge when needed, and work inside `<label className="field">`.
- [x] Every native `<select>` and `type="date"` input in the dashboard replaced.
- [x] Bug: file cards showed a broken `<img>` after a PDF was opened (the card and the drawer share
      one cached blob). Cards only render images now.

### Inspector (`/dev/inspector`)
- [x] Routes: every route with handler, Laravel-style middleware aliases (`auth`,
      `tenant.resolve`, `can:…`), the global stack shown once, group/method filters.
      `authz.Require` returns a named `*authz.Gate` so the permission is readable.
- [x] Roles & permissions: platform roles (permission/user counts), tenant-role count, the catalog
      compared with the database (declared-not-stored, stored-not-declared), admin defaults marked.
- [x] Config: every setting by section via reflection; secret-looking keys and URL credentials
      masked.
- [x] Packages: `go.mod` (embedded through the root `project` package) and
      `web/dev/package.json`; direct/indirect filter.

### Commands (`/dev/commands`)
- [x] `dev.CommandService` + `app/dev_commands.go` allowlist: about, migrate:status, health:check,
      permission:sync, settings:sync, feature-flags:sync, cache:clear, schedule:list, queue:stats,
      billing:expire-subscriptions, backup:run (background), backup:clean, activity:prune,
      notifications:purge-expired.
- [x] Typed options (bool/number/text/select), confirmation for data-changing commands,
      background runs with polling, run history with output (stored in `dev_actions`,
      `command.run`), panics and errors captured. Running requires `dev.manage`.
- [x] The previous "Commands" page (the audit log) is now **Audit log**.

### Settings
- [x] "All settings" view (default): every setting grouped with inline editors — switches,
      inputs that save on Enter/blur, JSON preview, units from the key, default + Reset + Details,
      search/group/type filters. The drag-and-drop board remains as "Groups board".

### Tenants (`/dev/tenants`)
- [x] Every tenant with its current subscription (an active/trial one when present, otherwise the
      latest), plan, expiry, users, branches and override counts; search plus tenant-status and
      subscription filters.
- [x] Drawer: usage cards (users, branches, overrides, storage), every plan feature with
      on/off or usage against its limit (subscription overrides applied and marked), and the
      subscription history. "Open setting overrides" / "Open feature flags" deep-link to those
      pages with the tenant selected (`?tenant=<id>`).
- [x] Read-only on purpose: `tenants.is_active` is not enforced at request time yet, so a toggle
      here would promise more than it does.

### Logs
- [x] Search box: matches the message, the attributes and the start of the request id;
      "No logs match" state with Clear filters; "Generate sample logs" calls the simulator.

### Settings drag and drop
- [x] Migration `00023_setting_sort_order.sql`: hand-picked position per setting (global row).
- [x] `PUT /dev/api/settings/groups/{id}/keys` (place keys in a group, in order) and
      `PUT /dev/api/settings/groups/order` (reorder groups), audited, `dev.manage`.
- [x] All-settings view: a left rail with **Ungrouped** settings and the group list. Drag a
      setting by its grip onto another setting (lands before it), onto a group card or a rail
      group (lands at the end), or onto Ungrouped; drag rail groups to reorder them.

### Audit log (`/dev/actions`)
- [x] Migration `00024_dev_actions_context.sql`: `ip`, `user_agent` on `dev_actions`; every audited
      action records where it came from (`httpx.ClientIP` / `httpx.UserAgent`).
- [x] Dashboard sign-ins (`auth.login`, including failures with the e-mail tried, never the
      password) and sign-outs (`auth.logout`) are recorded through `auth.SignInRecorder`; API
      logins from other devices are not.
- [x] Server-side filters: free text (actor e-mail, target, IP prefix, details), action, person,
      result, date range; `meta.total` count. `GET /actions/{id}`, `/actions/facets` (filter
      menus), `/actions/stats` (last 24 h, failed sign-ins, retention).
- [x] `DEV_DASHBOARD_AUDIT_RETENTION_DAYS` (default 180), pruned by `devdashboard.prune` and
      `dev-dashboard:prune`.
- [x] UI: stats header ("show them" jumps to failed sign-ins), filter card synced to the URL,
      readable action names with category tags, CSV export, and a drawer with result, who, IP,
      browser, details, prev/next (↑/↓) and quick filters (this person, this action, this IP).

### Command palette (Ctrl+K)
- [x] Lists every command (opens its run prompt), the inspector tabs and the new pages.

### Service layer (M53/M54, deferred from Phase 19)
- [x] `dev/services.go`: `SessionService`, `DeveloperService`, `FileBrowser`,
      `TelemetryService`, `TargetService`, `Auditor`. Handlers no longer call repositories.
- [x] All mutation routes declare `dev.manage` as route middleware (`r.With(h.manage)`);
      `requireManage` removed.
- [x] Audit writes use a detached context.

### Tests
- [x] `internal/app` integration suite (14 files, 37 tests) ported to `testkit` (package
      `app_test`); `testkit.WithLogger` added.
- [x] New: `dev/commands_test.go` (options, errors, panics, background runs, history) and
      `dev/inspector_integration_test.go` (all four inspector endpoints, command run, 403/404/422).

## Notes

- Commands run in-process with their own deadline (2 minutes by default; `BACKUP_TIMEOUT` for
  backups) and are detached from the request, so closing the tab does not cancel them.
- Add a command by appending to `devCommands` in `internal/app/dev_commands.go`; set `Confirm` for
  anything that changes data and `Background` for anything slow.
