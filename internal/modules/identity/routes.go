package identity

import "github.com/go-chi/chi/v5"

// RegisterRoutesResolve mounts tenant-scoped user routes.
func (h *Handler) RegisterRoutesResolve(r chi.Router) {
	r.Get("/user", h.index)
	r.Get("/user/profile", h.profile)
	r.Post("/user/profile/update", h.profileUpdate)
	r.Post("/user/profile/update-avatar", h.updateAvatar)
	r.Post("/user/profile/change-password", h.profileChangePassword)
	r.Get("/user/{id}", h.show)
	r.Post("/user", h.store)
	r.Patch("/user/{id}", h.update)
	r.Delete("/user/{id}", h.destroy)
	r.Post("/user/assign-role", h.assignRole)
	r.Post("/user/change-password", h.changePassword)
	r.Patch("/user/switch-branch/{id}", h.switchBranch)
	r.Post("/user/import-background", h.importUsersBackground)
	r.Post("/user/export-background", h.exportUsersBackground)
	r.Post("/user/import-template", h.importTemplate)
}

// RegisterRoutesExempt mounts internal (cross-tenant) user routes.
func (h *Handler) RegisterRoutesExempt(r chi.Router) {
	r.Post("/internal/user", h.storeInternal)
	r.Patch("/internal/user/change-password", h.changePasswordInternal)
	r.Patch("/internal/user/{id}", h.updateInternal)
	r.Post("/internal/user/{id}/update-avatar", h.updateUserAvatarInternal)
	r.Delete("/internal/user/{id}", h.destroyInternal)
	r.Post("/internal/user/assign-role", h.assignRoleInternal)
	r.Post("/internal/user/profile/update-avatar", h.updateProfileAvatarInternal)
	r.Patch("/internal/user/profile/update", h.profileUpdate)
	r.Patch("/internal/user/profile/change-password", h.profileChangePassword)
	r.Post("/internal/user/export", h.exportUsersInternal)
	r.Post("/internal/user/export-background", h.exportUsersBackground)
	r.Post("/internal/user/import", h.importUsersInternal)
	r.Post("/internal/user/import-background", h.importUsersBackground)
}
