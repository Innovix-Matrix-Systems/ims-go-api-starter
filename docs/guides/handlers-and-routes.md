---
title: Handlers and routes
description: The httpx.Kit handler pattern, route permissions, pagination, filters and the response envelope.
---

## The handler pattern

Every handler embeds `httpx.Kit`, so an action reads like a Laravel controller method:

```go
type Handler struct {
	service *Service
	checker *authz.Checker
	httpx.Kit // Bind, Validate, PathID, Data, Error, NoContent, Paginated, Cursor, Message
}

func NewHandler(service *Service, checker *authz.Checker, kit httpx.Kit) *Handler {
	return &Handler{service: service, checker: checker, Kit: kit}
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id") // positive int64 from {id}; writes the 400 itself
	if !ok {
		return
	}
	var req InsertUpdateRequest
	if !h.Bind(w, r, &req) { // decode JSON + run `validate` tags; writes 400/415/422
		return
	}
	branch, err := h.service.Update(r.Context(), tenancy.From(r), authn.UserID(r), id, req.toInput())
	if err != nil {
		h.Error(w, r, err) // *apperr.Error → RFC 9457 problem
		return
	}
	h.Data(w, r, http.StatusOK, NewResponse(*branch))
}
```

The kit is built **once** in `internal/app/wiring.go` (`httpx.NewKit(binder, validator,
responder)`) and handed to every `NewHandler` — the developer dashboard's handler included.

| Helper | Does |
| --- | --- |
| `h.Bind(w, r, &req)` | strict JSON decode + validation; on failure writes the problem and returns `false` |
| `h.Validate(w, r, &req)` | validation only — for query-string or multipart structs you filled yourself |
| `h.PathID(w, r, "id")` | reads a positive integer route parameter, 400 otherwise |
| `h.Data(w, r, status, v)` | `{"data": v}` |
| `h.Paginated(w, r, status, data, meta, links)` | `{"data": [...], "links": {...}, "meta": {...}}` |
| `h.Cursor(w, r, status, data, meta, links)` | cursor-paginated lists |
| `h.Message(w, r, status, "Done.")` | `{"message": "Done."}` |
| `h.NoContent(w)` | 204 |
| `h.Error(w, r, err)` | the right problem response for any error |
| `authn.UserID(r)` | the signed-in user (`auth()->id()`) |
| `tenancy.From(r)` | the resolved tenant scope |

## Routes and permissions

A module registers its routes in `routes.go`, and **each route declares its permission** — the
equivalent of Laravel's `->middleware('can:...')`:

```go
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.With(h.can(authz.PermBranchViewAll)).Get("/branch", h.index)
	r.With(h.can(authz.PermBranchView)).Get("/branch/{id}", h.show)
	r.With(h.can(authz.PermBranchCreate)).Post("/branch", h.store)
	r.With(h.can(authz.PermBranchUpdate)).Patch("/branch/{id}", h.update)
	r.With(h.can(authz.PermBranchDelete)).Delete("/branch/{id}", h.destroy)
}

// can is route middleware requiring a permission.
func (h *Handler) can(permission string) func(http.Handler) http.Handler {
	return authz.Require(h.checker, permission, h.Responder)
}
```

Only when the permission depends on the **payload** — sending `roles` while creating a user needs
`user.role.assign` — does a handler check inside the action:

```go
if len(req.Roles) > 0 {
	if err := h.checker.MustCan(r.Context(), authn.UserID(r), authz.PermUserRoleAssign); err != nil {
		h.Error(w, r, err)
		return
	}
}
```

:::note[Why permissions live at the edge]
CLI commands, queue jobs and scheduled tasks are trusted code and don't re-check permissions —
exactly like artisan commands and queued jobs. Services stay permission-free. What services
**always** enforce, whoever calls them, is tenant scoping and business invariants. See
[Permissions](./permissions.md).
:::

The module's `RegisterRoutes` is mounted into a route group in `internal/app/routes.go`; the group
decides authentication and tenancy (see [How it works](../getting-started/how-it-works.mdx#route-groups)).

## Pagination

Management lists use **page-based** pagination:

```go
page, perPage, offset := httpx.PageParams(r)                  // ?page=, ?per_page= (default 10, max 100)
orderBy, orderDir := httpx.OrderParams(r, "id", "name", "created_at") // whitelisted ?order_by=&order_direction=
items, total, err := h.service.List(r.Context(), tenancy.From(r), ListFilter{
	Search: r.URL.Query().Get("search"), OrderBy: orderBy, OrderDir: orderDir,
	Limit: int32(perPage), Offset: int32(offset),
})
meta, links := httpx.NewPagination(r, page, perPage, total)
h.Paginated(w, r, http.StatusOK, data, meta, links)
```

Feeds and streams (notifications, activity, dashboard telemetry) use **cursor** pagination:
`?limit=` (default 20–25, max 100) and `?cursor=`, answered with `h.Cursor` and links from
`httpx.CursorURL`.

`order_by` is always **whitelisted** per endpoint — an unknown column falls back to the default,
so user input never reaches `ORDER BY`.

## The response envelope

| Response | Shape |
| --- | --- |
| a resource | `{"data": {...}}` |
| a list | `{"data": [...], "links": {first, last, prev, next}, "meta": {current_page, per_page, total, …}}` |
| a message | `{"message": "…"}` |
| deleted / logged out | `204 No Content` |
| any error | `application/problem+json` (RFC 9457) — see [Validation & errors](./validation-and-errors.md) |

Timestamps are RFC 3339 in UTC. Money is an integer in minor units (paisa). IDs are 64-bit integers.
