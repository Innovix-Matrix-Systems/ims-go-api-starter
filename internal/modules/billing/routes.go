package billing

import "github.com/go-chi/chi/v5"

// RegisterPublicRoutes mounts unauthenticated package and callback routes.
func (h *Handler) RegisterPublicRoutes(r chi.Router) {
	r.Get("/public/packages", h.publicPackages)
	r.Get("/billing/{gateway}/callback", h.callback)
}

// RegisterRoutesResolve mounts tenant-scoped billing routes.
func (h *Handler) RegisterRoutesResolve(r chi.Router) {
	r.Get("/billing/subscription/status", h.status)
	r.Get("/billing/payments", h.listTenantPayments)
	r.Post("/billing/subscribe", h.subscribe)
}

// RegisterRoutesExempt mounts system billing management routes.
func (h *Handler) RegisterRoutesExempt(r chi.Router) {
	r.Get("/packages", h.listPackages)
	r.Post("/packages", h.storePackage)
	r.Get("/packages/{id}", h.showPackage)
	r.Patch("/packages/{id}", h.updatePackage)
	r.Delete("/packages/{id}", h.destroyPackage)

	r.Get("/billing/internal/subscriptions", h.listSubscriptions)
	r.Post("/billing/internal/subscriptions/{id}/cancel", h.cancelSubscription)
	r.Post("/billing/internal/subscriptions/{id}/overrides", h.overrides)
	r.Get("/billing/internal/payments", h.listAllPayments)
	r.Post("/billing/internal/payments", h.recordPayment)
	r.Get("/billing/internal/payment-approvals", h.listApprovals)
	r.Post("/billing/internal/payment-approvals/{id}/approve", h.approve)
	r.Post("/billing/internal/payment-approvals/{id}/reject", h.reject)
}
