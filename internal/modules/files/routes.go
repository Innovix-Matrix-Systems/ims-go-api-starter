package files

import (
	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
)

// RegisterRoutes mounts the file resource routes (tenant-exempt: files are
// platform-global).
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.With(h.can(authz.PermFileViewAll)).Get("/files", h.index)
	r.With(h.can(authz.PermFileCreate)).Post("/files", h.store)
	r.Get("/files/{id}", h.show)
	r.Get("/files/{id}/download", h.download)
	r.Delete("/files/{id}", h.destroy)
}
