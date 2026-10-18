package settings

import "github.com/go-chi/chi/v5"

// RegisterRoutes mounts the tenant-resolved settings routes (any authenticated
// user reads; tenant admins write their own override).
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/settings", h.index)
	r.Patch("/settings/tenant/{key}", h.updateTenant)
}

// RegisterInternalRoutes mounts the internal (cross-tenant) settings routes.
func (h *Handler) RegisterInternalRoutes(r chi.Router) {
	r.Get("/internal/settings", h.internalIndex)
	r.Patch("/internal/settings/{key}", h.internalUpdate)
	r.Delete("/internal/settings/{key}", h.internalDelete)
	r.Put("/internal/settings/{key}/group", h.assignGroup)

	r.Get("/internal/settings-groups", h.groupsIndex)
	r.Post("/internal/settings-groups", h.groupsStore)
	r.Get("/internal/settings-groups/{id}", h.groupsShow)
	r.Patch("/internal/settings-groups/{id}", h.groupsUpdate)
	r.Delete("/internal/settings-groups/{id}", h.groupsDestroy)
}
