package access

import "github.com/go-chi/chi/v5"

// RegisterRoutesResolve mounts tenant-scoped role routes.
func (h *RoleHandler) RegisterRoutesResolve(r chi.Router) {
	r.Get("/role", h.index)
	r.Post("/role", h.store)
	r.Get("/role/{id}", h.show)
	r.Patch("/role/{id}", h.update)
	r.Delete("/role/{id}", h.destroy)
	r.Post("/role/assign-permission", h.assignPermissions)
}

// RegisterRoutesExempt mounts internal role routes.
func (h *RoleHandler) RegisterRoutesExempt(r chi.Router) {
	r.Post("/internal/roles", h.storeInternal)
	r.Patch("/internal/roles/{id}", h.updateInternal)
	r.Post("/internal/role/assign-permission", h.assignPermissionsInternal)
}

// RegisterRoutes mounts permission routes (tenant-exempt group).
func (h *PermissionHandler) RegisterRoutes(r chi.Router) {
	r.Get("/permission", h.index)
	r.Post("/permission", h.store)
	r.Delete("/permission/{id}", h.destroy)
	r.Get("/permission/user", h.forUser)
}
