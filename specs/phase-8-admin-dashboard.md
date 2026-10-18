# Phase 8 — Admin dashboard (React)

**Status:** Stub (post-API — detail deferred until Phases 0–7 complete)
**Roadmap:** PLAN.md §7.21 + §11 Phase 8
**Depends on:** Phases 0–6 (full API surface)

## Goal

Admin React SPA (pure React + Vite + TanStack; **no Next.js**) embedded in the same binary via
`go:embed`, covering the capability surface from Phases 2–5.

## Scope checklist (draft)

- [ ] `web/admin` Vite app in-repo; build integrated into `make build` + `go:embed`
- [ ] TanStack Router / Query / Table / Form (+ Virtual where needed); zustand only if needed
- [ ] TypeScript types generated from `api/openapi.yaml`; CI type-check gate
- [ ] Auth/login + profile; token-vs-cookie mode decided in this spec
- [ ] Users, roles & permissions, tenants & branches screens
- [ ] Billing screens (packages, subscriptions, payments, approvals) for super-admin areas
- [ ] Notifications, settings, data-processing jobs (progress), uploads
- [ ] SPA serving: `/`, hashed assets + long cache, CSP, SPA fallback

## Out of scope

- Developer tooling views (Phase 9); public marketing pages.

## Acceptance criteria

- TBD — defined when this phase is specced.

## Open questions

- Token vs httpOnly-cookie auth mode for the SPA.
- Is zustand actually needed, or is TanStack Router+Query state sufficient?
- Component/styling library choice.
- UI i18n (reuse `messages.*` catalogs or separate UI catalogs?).
