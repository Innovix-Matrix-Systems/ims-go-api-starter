package auth

import "github.com/go-chi/chi/v5"

// RegisterPublicRoutes mounts unauthenticated auth routes.
func (h *Handler) RegisterPublicRoutes(r chi.Router) {
	r.Post("/login", h.login)
	r.Post("/verify-otp", h.verifyOTP)
}

// RegisterRoutes mounts authenticated auth routes (tenant-resolved group).
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/logout", h.logout)
}
