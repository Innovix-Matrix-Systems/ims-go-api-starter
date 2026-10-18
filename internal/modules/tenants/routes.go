package tenants

import "github.com/go-chi/chi/v5"

// RegisterRoutes mounts tenant management routes.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/tenant", h.index)
	r.Get("/tenant/{id}", h.show)
	r.Post("/tenant", h.store)
	r.Patch("/tenant/{id}", h.update)
	r.Delete("/tenant/{id}", h.destroy)
}
