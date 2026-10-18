package featureflags

import "github.com/go-chi/chi/v5"

// RegisterRoutes mounts the tenant-resolved feature-flag routes (any
// authenticated user reads their effective flags).
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/feature-flags", h.index)
}

// RegisterInternalRoutes mounts the internal (cross-tenant) flag admin routes.
func (h *Handler) RegisterInternalRoutes(r chi.Router) {
	r.Get("/internal/feature-flags", h.internalIndex)
	r.Patch("/internal/feature-flags", h.internalUpdate)
	r.Delete("/internal/feature-flags/{key}", h.internalDelete)
}
