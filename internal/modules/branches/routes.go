package branches

import "github.com/go-chi/chi/v5"

// RegisterRoutes mounts tenant-scoped branch routes.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/branch", h.index)
	r.Get("/branch/{id}", h.show)
	r.Post("/branch", h.store)
	r.Patch("/branch/{id}", h.update)
	r.Delete("/branch/{id}", h.destroy)
}
