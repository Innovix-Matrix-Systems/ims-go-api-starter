package branches

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/cache"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/tenancy"
)

// UserChecker verifies user existence inside a tenant (manager validation).
type UserChecker interface {
	ExistsInTenant(ctx context.Context, tenantID, userID int64) (bool, error)
}

// LimitChecker enforces billing plan limits for tenant resources.
type LimitChecker interface {
	RequireWithinLimitByName(ctx context.Context, tenantID int64, feature string, current int64) error
}

// Service implements branch business rules.
type Service struct {
	repo    Repository
	users   UserChecker
	cache   cache.Store
	checker *authz.Checker
	cfg     config.Tenancy
	limits  LimitChecker
}

// NewService creates the branch service.
func NewService(repo Repository, users UserChecker, store cache.Store, checker *authz.Checker, cfg config.Tenancy, limits LimitChecker) *Service {
	return &Service{repo: repo, users: users, cache: store, checker: checker, cfg: cfg, limits: limits}
}

// List returns branches for the effective tenant with pagination.
func (s *Service) List(ctx context.Context, scope tenancy.Scope, filter ListFilter) ([]Branch, int64, error) {
	if scope.AllTenants() {
		filter.AllTenants = true
		filter.TenantID = 0
	} else {
		tenantID := scope.EffectiveTenantID()
		if tenantID == 0 {
			return nil, 0, apperr.New(apperr.TenantContextNotResolved, "A tenant context is required.")
		}
		filter.TenantID = tenantID
	}
	items, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.repo.Count(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// Get returns one branch inside the effective tenant (or any tenant for a
// global read).
func (s *Service) Get(ctx context.Context, scope tenancy.Scope, id int64) (*Branch, error) {
	if scope.AllTenants() {
		return s.repo.Get(ctx, 0, id, true)
	}
	tenantID := scope.EffectiveTenantID()
	if tenantID == 0 {
		return nil, apperr.New(apperr.TenantContextNotResolved, "A tenant context is required.")
	}
	return s.repo.Get(ctx, tenantID, id, false)
}

// CreateParams carries branch creation input from handlers.
type CreateParams struct {
	Name        string
	Address     *string
	Description *string
	Phone       *string
	Email       *string
	ManagerID   *int64
}

// Create adds a branch to the effective tenant.
func (s *Service) Create(ctx context.Context, scope tenancy.Scope, actorID int64, input CreateParams) (*Branch, error) {
	tenantID := scope.EffectiveTenantID()
	if tenantID == 0 {
		return nil, apperr.New(apperr.TenantContextNotResolved, "A tenant context is required.")
	}
	if s.limits != nil {
		count, err := s.repo.Count(ctx, ListFilter{TenantID: tenantID})
		if err != nil {
			return nil, err
		}
		if err := s.limits.RequireWithinLimitByName(ctx, tenantID, "branches.max", count); err != nil {
			return nil, err
		}
	}
	if err := s.validateManager(ctx, tenantID, input.ManagerID); err != nil {
		return nil, err
	}
	branch, err := s.repo.Create(ctx, CreateInput{
		TenantID:    tenantID,
		Name:        input.Name,
		Address:     input.Address,
		Description: input.Description,
		Phone:       input.Phone,
		Email:       input.Email,
		ManagerID:   input.ManagerID,
		ActorID:     &actorID,
	})
	if err != nil {
		return nil, err
	}
	return branch, nil
}

// Update modifies a branch inside the effective tenant.
func (s *Service) Update(ctx context.Context, scope tenancy.Scope, actorID, id int64, input CreateParams) (*Branch, error) {
	tenantID := scope.EffectiveTenantID()
	if tenantID == 0 {
		return nil, apperr.New(apperr.TenantContextNotResolved, "A tenant context is required.")
	}
	if err := s.validateManager(ctx, tenantID, input.ManagerID); err != nil {
		return nil, err
	}
	branch, err := s.repo.Update(ctx, UpdateInput{
		ID:          id,
		TenantID:    tenantID,
		Name:        input.Name,
		Address:     input.Address,
		Description: input.Description,
		Phone:       input.Phone,
		Email:       input.Email,
		ManagerID:   input.ManagerID,
		ActorID:     &actorID,
	})
	if err != nil {
		return nil, err
	}
	tenancy.InvalidateBranchCheck(ctx, s.cache, tenantID, id)
	return branch, nil
}

// Delete soft-deletes a branch, protecting the configured default branch.
func (s *Service) Delete(ctx context.Context, scope tenancy.Scope, actorID, id int64) error {
	tenantID := scope.EffectiveTenantID()
	if tenantID == 0 {
		return apperr.New(apperr.TenantContextNotResolved, "A tenant context is required.")
	}
	if id == s.cfg.DefaultBranchID {
		return apperr.New(apperr.OperationNotAllowed, "The default branch cannot be deleted.")
	}
	if err := s.repo.Delete(ctx, tenantID, id, &actorID); err != nil {
		return err
	}
	tenancy.InvalidateBranchCheck(ctx, s.cache, tenantID, id)
	return nil
}

// CreateDefaultForTenant provisions a default branch for a new tenant using an
// auto-assigned id (the fixed DEFAULT_BRANCH_ID is reserved for the seeded
// default tenant).
func (s *Service) CreateDefaultForTenant(ctx context.Context, tenantID int64) error {
	branch, err := s.repo.Create(ctx, CreateInput{
		TenantID: tenantID,
		Name:     s.cfg.DefaultBranchName,
	})
	if err != nil {
		return err
	}
	tenancy.InvalidateBranchCheck(ctx, s.cache, tenantID, branch.ID)
	return nil
}

// DeleteForTenant soft-deletes every branch of a tenant.
func (s *Service) DeleteForTenant(ctx context.Context, tenantID int64) error {
	return s.repo.SoftDeleteForTenant(ctx, tenantID)
}

func (s *Service) validateManager(ctx context.Context, tenantID int64, managerID *int64) error {
	if managerID == nil {
		return nil
	}
	exists, err := s.users.ExistsInTenant(ctx, tenantID, *managerID)
	if err != nil {
		return err
	}
	if !exists {
		return apperr.Validation(map[string][]string{
			"manager_id": {"The selected manager does not belong to the tenant."},
		})
	}
	return nil
}

// Handler serves branch HTTP endpoints.
type Handler struct {
	service   *Service
	checker   *authz.Checker
	binder    *httpx.Binder
	validator *httpx.Validator
	responder *httpx.Responder
}

// NewHandler creates the branch handler.
func NewHandler(service *Service, checker *authz.Checker, binder *httpx.Binder, validator *httpx.Validator, responder *httpx.Responder) *Handler {
	return &Handler{service: service, checker: checker, binder: binder, validator: validator, responder: responder}
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermBranchViewAll); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	scope, _ := tenancy.FromContext(r.Context())

	page, perPage, offset := httpx.PageParams(r)
	orderBy, orderDir := httpx.OrderParams(r, "id", "name", "created_at")
	filter := ListFilter{
		Search:   r.URL.Query().Get("search"),
		OrderBy:  orderBy,
		OrderDir: orderDir,
		Limit:    int32(perPage),
		Offset:   int32(offset),
	}

	items, total, err := h.service.List(r.Context(), scope, filter)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	data := make([]Response, 0, len(items))
	for _, item := range items {
		data = append(data, NewResponse(item))
	}
	meta, links := httpx.NewPagination(r, page, perPage, total)
	h.responder.Paginated(w, r, http.StatusOK, data, meta, links)
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermBranchView); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	id, err := pathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	scope, _ := tenancy.FromContext(r.Context())
	branch, err := h.service.Get(r.Context(), scope, id)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, NewResponse(*branch))
}

func (h *Handler) store(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermBranchCreate); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	var req InsertUpdateRequest
	if err := h.binder.JSON(w, r, &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	scope, _ := tenancy.FromContext(r.Context())
	branch, err := h.service.Create(r.Context(), scope, identity.UserID, req.toInput())
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusCreated, NewResponse(*branch))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermBranchUpdate); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	id, err := pathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	var req InsertUpdateRequest
	if err := h.binder.JSON(w, r, &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	scope, _ := tenancy.FromContext(r.Context())
	branch, err := h.service.Update(r.Context(), scope, identity.UserID, id, req.toInput())
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, NewResponse(*branch))
}

func (h *Handler) destroy(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermBranchDelete); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	id, err := pathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	scope, _ := tenancy.FromContext(r.Context())
	if err := h.service.Delete(r.Context(), scope, identity.UserID, id); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.NoContent(w)
}

func pathID(r *http.Request) (int64, error) {
	raw := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		return 0, apperr.New(apperr.InvalidInput, "The provided identifier is invalid.")
	}
	return id, nil
}
