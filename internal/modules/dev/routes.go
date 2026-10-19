package dev

import "github.com/go-chi/chi/v5"

// RegisterRoutes mounts the developer dashboard JSON API. The caller provides
// the authentication, tenancy-exempt and dev.view middleware chain.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/me", h.me)
	r.Get("/actions", h.listActions)
	r.Post("/simulate-events", h.simulateEvents)

	r.Get("/requests", h.listRequests)
	r.Get("/requests/{id}", h.getRequest)
	r.Get("/logs", h.listLogs)
	r.Get("/logs/{id}", h.getLog)
	r.Get("/exceptions", h.listExceptions)
	r.Get("/exceptions/{id}", h.getException)
	r.Post("/telemetry/prune", h.pruneTelemetry)

	r.Route("/queue", func(r chi.Router) {
		r.Get("/queues", h.listQueues)
		r.Post("/queues/{name}/pause", h.pauseQueue)
		r.Post("/queues/{name}/resume", h.resumeQueue)
		r.Get("/jobs", h.listJobs)
		r.Get("/jobs/{id}", h.getJob)
		r.Post("/jobs/{id}/retry", h.retryJob)
		r.Post("/jobs/{id}/cancel", h.cancelJob)
		r.Delete("/jobs/{id}", h.deleteJob)
	})
}
