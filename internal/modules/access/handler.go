package access

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/tenancy"
)

// RoleHandler serves role endpoints.
type RoleHandler struct {
	service   *RoleService
	checker   *authz.Checker
	tenants   TenantLookup
	binder    *httpx.Binder
	validator *httpx.Validator
	responder *httpx.Responder
}

// NewRoleHandler creates the role handler.
func NewRoleHandler(service *RoleService, checker *authz.Checker, tenants TenantLookup, binder *httpx.Binder, validator *httpx.Validator, responder *httpx.Responder) *RoleHandler {
	return &RoleHandler{service: service, checker: checker, tenants: tenants, binder: binder, validator: validator, responder: responder}
}

// tenantSlims loads the slim tenants for a set of roles in one query and keys
// them by tenant id (nil tenants are skipped).
func (h *RoleHandler) tenantSlims(ctx context.Context, roles []Role) map[int64]*TenantSlim {
	out := map[int64]*TenantSlim{}
	if h.tenants == nil {
		return out
	}
	ids := make([]int64, 0, len(roles))
	seen := map[int64]bool{}
	for _, role := range roles {
		if role.TenantID != nil && !seen[*role.TenantID] {
			seen[*role.TenantID] = true
			ids = append(ids, *role.TenantID)
		}
	}
	if len(ids) == 0 {
		return out
	}
	refs, err := h.tenants.RefsByIDs(ctx, ids)
	if err != nil {
		return out
	}
	for id, ref := range refs {
		out[id] = NewTenantSlim(ref)
	}
	return out
}

// responseFor maps a single role, embedding its tenant when it has one.
func (h *RoleHandler) responseFor(ctx context.Context, role Role) RoleResponse {
	byTenant := h.tenantSlims(ctx, []Role{role})
	if role.TenantID == nil {
		return NewRoleResponse(role, nil)
	}
	return NewRoleResponse(role, byTenant[*role.TenantID])
}

func (h *RoleHandler) index(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermRoleViewAll); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	scope, _ := tenancy.FromContext(r.Context())
	roles, err := h.service.List(r.Context(), scope.EffectiveTenantID())
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	byTenant := h.tenantSlims(r.Context(), roles)
	data := make([]RoleResponse, 0, len(roles))
	for _, role := range roles {
		var tenant *TenantSlim
		if role.TenantID != nil {
			tenant = byTenant[*role.TenantID]
		}
		data = append(data, NewRoleResponse(role, tenant))
	}
	h.responder.Data(w, r, http.StatusOK, data)
}

func (h *RoleHandler) show(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermRoleView); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	id, err := rolePathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	scope, _ := tenancy.FromContext(r.Context())
	role, err := h.service.Get(r.Context(), scope.EffectiveTenantID(), id)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, h.responseFor(r.Context(), *role))
}

func (h *RoleHandler) store(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermRoleCreate); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	var req RoleInsertUpdateRequest
	if err := h.binder.JSON(w, r, &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	scope, _ := tenancy.FromContext(r.Context())
	role, err := h.service.Create(r.Context(), scope.EffectiveTenantID(), identity.UserID, trimString(req.Name), req.Description)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusCreated, h.responseFor(r.Context(), *role))
}

func (h *RoleHandler) update(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermRoleUpdate); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	id, err := rolePathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	var req RoleInsertUpdateRequest
	if err := h.binder.JSON(w, r, &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	scope, _ := tenancy.FromContext(r.Context())
	role, err := h.service.Update(r.Context(), scope.EffectiveTenantID(), id, identity.UserID, trimString(req.Name), req.Description)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, h.responseFor(r.Context(), *role))
}

func (h *RoleHandler) destroy(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermRoleDelete); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	id, err := rolePathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	scope, _ := tenancy.FromContext(r.Context())
	if err := h.service.Delete(r.Context(), scope.EffectiveTenantID(), id); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.NoContent(w)
}

func (h *RoleHandler) assignPermissions(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermRoleUpdate); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	var req RolePermissionAssignRequest
	if err := h.binder.JSON(w, r, &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	scope, _ := tenancy.FromContext(r.Context())
	if err := h.service.AssignPermissions(r.Context(), scope.EffectiveTenantID(), req.ID, req.Permissions); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Message(w, r, http.StatusOK, "Permissions assigned successfully.")
}

func (h *RoleHandler) storeInternal(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermRoleInternalCreate); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	var req RoleInternalRequest
	if err := h.binder.JSON(w, r, &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	role, err := h.service.CreateInternal(r.Context(), req.TenantID, identity.UserID, trimString(req.Name), req.Description, req.IsSystem)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusCreated, h.responseFor(r.Context(), *role))
}

func (h *RoleHandler) updateInternal(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermRoleInternalUpdate); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	id, err := rolePathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	var req RoleInsertUpdateRequest
	if err := h.binder.JSON(w, r, &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	role, err := h.service.UpdateInternal(r.Context(), id, identity.UserID, trimString(req.Name), req.Description)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, h.responseFor(r.Context(), *role))
}

func (h *RoleHandler) assignPermissionsInternal(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermRoleInternalUpdate); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	var req RolePermissionAssignRequest
	if err := h.binder.JSON(w, r, &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.service.AssignPermissionsInternal(r.Context(), req.ID, req.Permissions); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Message(w, r, http.StatusOK, "Permissions assigned successfully.")
}

// PermissionHandler serves permission endpoints (internal surface).
type PermissionHandler struct {
	service   *PermissionService
	checker   *authz.Checker
	binder    *httpx.Binder
	validator *httpx.Validator
	responder *httpx.Responder
}

// NewPermissionHandler creates the permission handler.
func NewPermissionHandler(service *PermissionService, checker *authz.Checker, binder *httpx.Binder, validator *httpx.Validator, responder *httpx.Responder) *PermissionHandler {
	return &PermissionHandler{service: service, checker: checker, binder: binder, validator: validator, responder: responder}
}

func (h *PermissionHandler) index(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermPermissionView); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	permissions, err := h.service.List(r.Context())
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	data := make([]PermissionResponse, 0, len(permissions))
	for _, permission := range permissions {
		data = append(data, NewPermissionResponse(permission))
	}
	h.responder.Data(w, r, http.StatusOK, data)
}

func (h *PermissionHandler) store(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustBeSuperAdmin(r.Context(), identity.UserID); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	var req PermissionInsertRequest
	if err := h.binder.JSON(w, r, &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	permission, err := h.service.Create(r.Context(), trimString(req.Name), req.Group, req.Description)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusCreated, NewPermissionResponse(*permission))
}

func (h *PermissionHandler) destroy(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustBeSuperAdmin(r.Context(), identity.UserID); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	id, err := rolePathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.service.Delete(r.Context(), id); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.NoContent(w)
}

func (h *PermissionHandler) forUser(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	permissions, err := h.service.ForUser(r.Context(), identity.UserID)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	data := make([]PermissionResponse, 0, len(permissions))
	for _, permission := range permissions {
		data = append(data, NewPermissionResponse(permission))
	}
	h.responder.Data(w, r, http.StatusOK, data)
}

func rolePathID(r *http.Request) (int64, error) {
	raw := chi.URLParam(r, "id")
	id, err := parsePathID(raw)
	if err != nil || id < 1 {
		return 0, apperr.New(apperr.InvalidInput, "The provided identifier is invalid.")
	}
	return id, nil
}
