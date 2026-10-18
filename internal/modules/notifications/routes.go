package notifications

import "github.com/go-chi/chi/v5"

// RegisterRoutesResolve mounts tenant-scoped notification routes (feed and
// read state for the authenticated user).
func (h *Handler) RegisterRoutesResolve(r chi.Router) {
	r.Get("/notification/feed", h.feed)
	r.Post("/notification/{id}/read", h.markRead)
	r.Post("/notification/read-all", h.markAllRead)
}

// RegisterRoutesExempt mounts the internal (tenant-exempt) routes: the same
// feed/read endpoints for identities without a tenant, plus the admin CRUD
// (permission-gated).
func (h *Handler) RegisterRoutesExempt(r chi.Router) {
	r.Get("/internal/notification/feed", h.feed)
	r.Post("/internal/notification/{id}/read", h.markRead)
	r.Post("/internal/notification/read-all", h.markAllRead)

	r.Get("/internal/notification", h.index)
	r.Get("/internal/notification/{id}", h.show)
	r.Post("/internal/notification", h.store)
	r.Patch("/internal/notification/{id}", h.update)
	r.Delete("/internal/notification/{id}", h.destroy)
}
