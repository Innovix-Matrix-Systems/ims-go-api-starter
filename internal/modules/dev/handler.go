package dev

import (
	"log/slog"
	"net/http"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/access"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/queue"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/telemetry"
)

// Handler serves the developer dashboard JSON API at /dev/api.
type Handler struct {
	checker       *authz.Checker
	permissions   *access.PermissionService
	queueAdmin    queue.Admin
	actions       ActionRepository
	telemetry     TelemetryRepository
	collector     *telemetry.Collector
	logger        *slog.Logger
	retentionDays int
	binder        *httpx.Binder
	validator     *httpx.Validator
	responder     *httpx.Responder
}

// NewHandler creates the developer dashboard handler.
func NewHandler(
	checker *authz.Checker,
	permissions *access.PermissionService,
	queueAdmin queue.Admin,
	actions ActionRepository,
	telemetryRepo TelemetryRepository,
	collector *telemetry.Collector,
	logger *slog.Logger,
	retentionDays int,
	binder *httpx.Binder,
	validator *httpx.Validator,
	responder *httpx.Responder,
) *Handler {
	if retentionDays < 1 {
		retentionDays = 7
	}
	return &Handler{
		checker:       checker,
		permissions:   permissions,
		queueAdmin:    queueAdmin,
		actions:       actions,
		telemetry:     telemetryRepo,
		collector:     collector,
		logger:        logger,
		retentionDays: retentionDays,
		binder:        binder,
		validator:     validator,
		responder:     responder,
	}
}

// MeResponse is the SPA bootstrap payload: the current identity plus the
// effective permission set (super-admins bypass permission checks, so the
// seeded role permissions are returned as-is).
type MeResponse struct {
	UserID       int64    `json:"user_id"`
	Name         string   `json:"name"`
	Email        string   `json:"email"`
	TenantID     *int64   `json:"tenant_id"`
	BranchID     *int64   `json:"branch_id"`
	IsSuperAdmin bool     `json:"is_super_admin"`
	Permissions  []string `json:"permissions"`
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	identity, ok := authn.IdentityFromContext(r.Context())
	if !ok {
		h.responder.Error(w, r, apperr.New(apperr.Unauthorized, "Authentication is required."))
		return
	}
	permissions, err := h.permissions.ForUser(r.Context(), identity.UserID)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	super, err := h.checker.IsSuperAdmin(r.Context(), identity.UserID)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	names := make([]string, 0, len(permissions))
	for _, permission := range permissions {
		names = append(names, permission.Name)
	}
	var tenantID *int64
	if identity.TenantID != 0 {
		value := identity.TenantID
		tenantID = &value
	}
	var branchID *int64
	if identity.BranchID != 0 {
		value := identity.BranchID
		branchID = &value
	}
	h.responder.Data(w, r, http.StatusOK, MeResponse{
		UserID:       identity.UserID,
		Name:         identity.Name,
		Email:        identity.Email,
		TenantID:     tenantID,
		BranchID:     branchID,
		IsSuperAdmin: super,
		Permissions:  names,
	})
}
