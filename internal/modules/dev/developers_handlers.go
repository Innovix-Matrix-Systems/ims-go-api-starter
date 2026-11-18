package dev

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/access"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/identity"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/tenancy"
)

// Developers panel: dedicated internal accounts that hold the hidden Developer
// role. Creating an account creates a tenantless internal user and assigns the
// role in the same operation; the role itself can be provisioned from here
// when a fresh database was seeded without it.

// DeveloperStats is the dashboard header payload.
type DeveloperStats struct {
	Developers       int64  `json:"developers"`
	RoleExists       bool   `json:"role_exists"`
	RoleID           *int64 `json:"role_id"`
	RoleName         string `json:"role_name"`
	RolePermissions  int64  `json:"role_permissions"`
	TotalPermissions int64  `json:"total_permissions"`
}

func (h *Handler) listDevelopers(w http.ResponseWriter, r *http.Request) {
	page, perPage, offset := httpx.PageParams(r)
	filter := DeveloperFilter{
		Search: strings.TrimSpace(r.URL.Query().Get("search")),
		Limit:  int32(perPage),
		Offset: int32(offset),
	}
	items, total, err := h.developers.List(r.Context(), filter)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if actorID, ok := authnIdentity(r); ok {
		for i := range items {
			items[i].IsSelf = items[i].UserID == actorID
		}
	}
	meta, links := httpx.NewPagination(r, page, perPage, total)
	h.responder.Paginated(w, r, http.StatusOK, items, meta, links)
}

func (h *Handler) developerStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.developerStatsData(r.Context())
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, stats)
}

type createDeveloperRequest struct {
	Name     string `json:"name" validate:"required,min=2,max=255"`
	Email    string `json:"email" validate:"required,email,max=255"`
	Password string `json:"password" validate:"required,min=8,max=72"`
	IsActive *bool  `json:"is_active"`
}

func (h *Handler) createDeveloper(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.requireManage(w, r)
	if !ok {
		return
	}
	var request createDeveloperRequest
	if err := h.binder.JSON(w, r, &request); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &request); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	role, err := h.developerRole(r.Context())
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if role == nil {
		h.responder.Error(w, r, apperr.New(apperr.Conflict,
			"The Developer role is missing. Create the role from the Developers panel first."))
		return
	}
	scope, err := devScope(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	isActive := true
	if request.IsActive != nil {
		isActive = *request.IsActive
	}

	started := time.Now()
	created, opErr := h.identity.Create(r.Context(), scope, actorID, identity.CreateParams{
		Name:     request.Name,
		Email:    request.Email,
		Password: request.Password,
		IsActive: &isActive,
		Roles:    []int64{role.ID},
	})
	target := request.Email
	if created != nil {
		target = formatTarget("user", created.ID)
	}
	h.recordAction(r.Context(), actorID, "developers.create", target, map[string]any{
		"name":  request.Name,
		"email": request.Email,
	}, started, opErr)
	if opErr != nil {
		h.responder.Error(w, r, opErr)
		return
	}
	h.responder.Data(w, r, http.StatusCreated, developerFromUser(created, actorID))
}

func (h *Handler) ensureDeveloperRole(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.requireManage(w, r)
	if !ok {
		return
	}
	started := time.Now()
	role, assigned, opErr := h.roleService.EnsureDeveloperRole(r.Context(), actorID)
	target := authz.DeveloperRole
	if role != nil {
		target = formatTarget("role", role.ID)
	}
	h.recordAction(r.Context(), actorID, "developers.role.ensure", target, map[string]any{
		"role":        authz.DeveloperRole,
		"permissions": assigned,
	}, started, opErr)
	if opErr != nil {
		h.responder.Error(w, r, opErr)
		return
	}
	stats, err := h.developerStatsData(r.Context())
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, stats)
}

func (h *Handler) removeDeveloper(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.requireManage(w, r)
	if !ok {
		return
	}
	targetID, err := pathParamID(r, "user_id")
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if targetID == actorID {
		h.responder.Error(w, r, apperr.New(apperr.Forbidden,
			"You cannot remove your own Developer role."))
		return
	}
	role, err := h.developerRole(r.Context())
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if role == nil {
		h.responder.Error(w, r, apperr.New(apperr.ResourceNotFound, "The Developer role does not exist."))
		return
	}
	scope, err := devScope(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}

	started := time.Now()
	_, opErr := h.identity.RevokeRole(r.Context(), scope, targetID, role.ID)
	h.recordAction(r.Context(), actorID, "developers.unassign", formatTarget("user", targetID),
		map[string]any{"user_id": targetID, "role": authz.DeveloperRole}, started, opErr)
	if opErr != nil {
		h.responder.Error(w, r, opErr)
		return
	}
	h.responder.Data(w, r, http.StatusOK, map[string]any{"removed": true})
}

func (h *Handler) developerStatsData(ctx context.Context) (DeveloperStats, error) {
	developers, err := h.developers.Count(ctx)
	if err != nil {
		return DeveloperStats{}, err
	}
	stats := DeveloperStats{
		Developers:       developers,
		RoleName:         authz.DeveloperRole,
		TotalPermissions: int64(len(authz.DeveloperPermissions())),
	}
	role, err := h.developerRole(ctx)
	if err != nil {
		return DeveloperStats{}, err
	}
	if role == nil {
		return stats, nil
	}
	stats.RoleExists = true
	roleID := role.ID
	stats.RoleID = &roleID
	permissions, err := h.roles.CountPermissions(ctx, role.ID)
	if err != nil {
		return DeveloperStats{}, err
	}
	stats.RolePermissions = permissions
	return stats, nil
}

func (h *Handler) developerRole(ctx context.Context) (*access.Role, error) {
	role, err := h.roles.GetSystemByName(ctx, authz.DeveloperRole)
	if err != nil {
		if apperr.Is(err, apperr.ResourceNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return role, nil
}

func devScope(r *http.Request) (tenancy.Scope, error) {
	scope, ok := tenancy.FromContext(r.Context())
	if !ok {
		return tenancy.Scope{}, apperr.New(apperr.InternalServerError, "Tenant scope is not resolved.")
	}
	return scope, nil
}

func developerFromUser(user *identity.User, actorID int64) Developer {
	return Developer{
		UserID:      user.ID,
		Name:        user.Name,
		Email:       user.Email,
		IsActive:    user.IsActive,
		IsSelf:      user.ID == actorID,
		LastLoginAt: user.LastLoginAt,
		CreatedAt:   user.CreatedAt,
	}
}

func authnIdentity(r *http.Request) (int64, bool) {
	identity, ok := authn.IdentityFromContext(r.Context())
	if !ok {
		return 0, false
	}
	return identity.UserID, true
}
