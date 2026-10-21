package dev

import "github.com/go-chi/chi/v5"

// RegisterRoutes mounts the developer dashboard JSON API. The caller provides
// the authentication, tenancy-exempt and dev.view middleware chain.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/me", h.me)
	r.Get("/actions", h.listActions)
	r.Post("/simulate-events", h.simulateEvents)

	r.Get("/playground", h.playgroundInfoHandler)
	r.Post("/playground/jobs", h.enqueuePlaygroundJobs)

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

	r.Get("/sessions/stats", h.sessionStats)
	r.Get("/sessions/users", h.listSessionUsers)
	r.Get("/sessions/users/{id}/tokens", h.listUserSessions)
	r.Delete("/sessions/tokens/{id}", h.revokeSessionToken)
	r.Delete("/sessions/users/{id}", h.revokeUserSessions)
	r.Delete("/sessions", h.revokeAllSessions)
	r.Post("/sessions/prune", h.pruneSessions)

	r.Get("/developers", h.listDevelopers)
	r.Get("/developers/stats", h.developerStats)
	r.Post("/developers", h.createDeveloper)
	r.Post("/developers/role", h.ensureDeveloperRole)
	r.Delete("/developers/{user_id}", h.removeDeveloper)

	r.Get("/files", h.listFiles)
	r.Get("/files/stats", h.fileStats)
	r.Get("/files/{id}/download", h.downloadFile)
	r.Delete("/files/{id}", h.deleteFile)

	r.Get("/backups", h.listBackups)
	r.Get("/backups/stats", h.backupStats)
	r.Get("/backups/config", h.backupConfig)
	r.Post("/backups", h.createBackup)
	r.Post("/backups/clean", h.cleanBackups)
	r.Get("/backups/{id}", h.getBackup)
	r.Get("/backups/{id}/download", h.downloadBackup)
	r.Delete("/backups/{id}", h.deleteBackup)

	r.Get("/targets", h.searchTargets)
	r.Get("/targets/stats", h.targetStats)

	r.Get("/settings/groups", h.listDevSettingGroups)
	r.Post("/settings/groups", h.createDevSettingGroup)
	r.Patch("/settings/groups/{id}", h.updateDevSettingGroup)
	r.Delete("/settings/groups/{id}", h.deleteDevSettingGroup)
	r.Post("/settings/sync", h.syncDevSettings)
	r.Get("/settings", h.listDevSettings)
	r.Patch("/settings/{key}", h.setDevSetting)
	r.Delete("/settings/{key}", h.resetDevSetting)
	r.Patch("/settings/{key}/group", h.assignDevSettingGroup)

	r.Get("/flags", h.listFlags)
	r.Patch("/flags", h.updateFlags)
	r.Post("/flags/sync", h.syncFlags)
	r.Delete("/flags/{key}", h.resetFlag)
}
