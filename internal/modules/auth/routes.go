package auth

import "github.com/go-chi/chi/v5"

// RegisterPublicRoutes mounts unauthenticated auth routes.
func (h *Handler) RegisterPublicRoutes(r chi.Router) {
	r.Post("/login", h.login)
	r.Post("/verify-otp", h.verifyOTP)
	if h.recovery != nil {
		r.Post("/forgot-password", h.forgotPassword)
		r.Post("/reset-password", h.resetPassword)
		r.Post("/email/verification-notification", h.resendVerification)
		r.Post("/email/verify", h.verifyEmail)
	}
}

// RegisterRoutes mounts authenticated session routes (no tenant resolution).
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/logout", h.logout)
}
