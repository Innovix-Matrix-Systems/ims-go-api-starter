package files

import "github.com/go-chi/chi/v5"

// RegisterRoutes mounts the file resource routes (tenant-exempt: files are
// platform-global).
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/files", h.index)
	r.Post("/files", h.store)
	r.Get("/files/{id}", h.show)
	r.Get("/files/{id}/download", h.download)
	r.Delete("/files/{id}", h.destroy)
}
