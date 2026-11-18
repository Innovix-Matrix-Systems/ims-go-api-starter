package identity

import (
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/tenancy"
)

// avatarMaxBytes is the maximum avatar upload size (5 MB).
const avatarMaxBytes = 5 << 20

// PhotoManager resolves and updates user profile photos (implemented by the
// files module; nil disables avatar handling).
type PhotoManager interface {
	UpdateAvatar(ctx context.Context, actorID, userID int64, tenantID int64, header *multipart.FileHeader) error
	PhotoURL(ctx context.Context, userID int64) (string, error)
	PhotoURLs(ctx context.Context, userIDs []int64) (map[int64]string, error)
}

// Handler serves user HTTP endpoints.
type Handler struct {
	service   *Service
	photo     PhotoManager
	exchange  *ExchangeService
	checker   *authz.Checker
	binder    *httpx.Binder
	validator *httpx.Validator
	responder *httpx.Responder
}

// NewHandler creates the user handler.
func NewHandler(service *Service, photo PhotoManager, exchange *ExchangeService, checker *authz.Checker, binder *httpx.Binder, validator *httpx.Validator, responder *httpx.Responder) *Handler {
	return &Handler{service: service, photo: photo, exchange: exchange, checker: checker, binder: binder, validator: validator, responder: responder}
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermUserViewAll); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	scope, _ := tenancy.FromContext(r.Context())

	page, perPage, offset := httpx.PageParams(r)
	orderBy, orderDir := httpx.OrderParams(r, "id", "name", "email", "created_at")
	filter := ListFilter{
		Search:   r.URL.Query().Get("search"),
		RoleName: r.URL.Query().Get("role_name"),
		OrderBy:  orderBy,
		OrderDir: orderDir,
		Limit:    int32(perPage),
		Offset:   int32(offset),
	}
	if raw := r.URL.Query().Get("is_active"); raw != "" {
		if value, err := strconv.ParseBool(raw); err == nil {
			filter.IsActive = &value
		}
	}

	users, total, err := h.service.List(r.Context(), scope, filter)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	photos := map[int64]string{}
	if h.photo != nil {
		ids := make([]int64, 0, len(users))
		for _, user := range users {
			ids = append(ids, user.ID)
		}
		if resolved, err := h.photo.PhotoURLs(r.Context(), ids); err == nil {
			photos = resolved
		}
	}
	data := make([]Response, 0, len(users))
	for _, user := range users {
		roles, err := h.checker.RoleRefs(r.Context(), user.ID)
		if err != nil {
			roles = nil
		}
		data = append(data, NewResponse(user, roles, stringPtr(photos[user.ID])))
	}
	meta, links := httpx.NewPagination(r, page, perPage, total)
	h.responder.Paginated(w, r, http.StatusOK, data, meta, links)
}

func (h *Handler) profile(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	user, err := h.service.GetRecord(r.Context(), identity.UserID)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, h.responseFor(r.Context(), *user))
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	id, err := userPathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if id != identity.UserID {
		if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermUserView); err != nil {
			h.responder.Error(w, r, err)
			return
		}
	}
	scope, _ := tenancy.FromContext(r.Context())
	user, err := h.service.Get(r.Context(), scope, id)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, h.responseFor(r.Context(), *user))
}

func (h *Handler) store(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermUserCreate); err != nil {
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
	user, err := h.service.Create(r.Context(), scope, identity.UserID, req.toCreateInput())
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusCreated, h.responseFor(r.Context(), *user))
}

func (h *Handler) storeInternal(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermUserInternalCreate); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	var req InternalInsertRequest
	if err := h.binder.JSON(w, r, &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	scope, _ := tenancy.FromContext(r.Context())
	input := req.InsertUpdateRequest.toCreateInput()
	input.TenantID = req.TenantID
	user, err := h.service.Create(r.Context(), scope, identity.UserID, input)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusCreated, h.responseFor(r.Context(), *user))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermUserUpdate); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.updateTarget(w, r, authz.PermUserRoleAssign)
}

func (h *Handler) updateInternal(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermUserInternalUpdate); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.updateTarget(w, r, authz.PermUserInternalRoleAssign)
}

// updateTarget applies a user update. Changing roles through the update
// payload needs the role-assign permission too, not just update.
func (h *Handler) updateTarget(w http.ResponseWriter, r *http.Request, rolePerm string) {
	identity, _ := authn.IdentityFromContext(r.Context())
	id, err := userPathID(r)
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
	if req.Roles != nil {
		if err := h.checker.MustCan(r.Context(), identity.UserID, rolePerm); err != nil {
			h.responder.Error(w, r, err)
			return
		}
	}
	scope, _ := tenancy.FromContext(r.Context())
	user, err := h.service.Update(r.Context(), scope, identity.UserID, id, UpdateParams{
		FirstName: req.FirstName,
		LastName:  req.LastName,
		Name:      strings.TrimSpace(req.Name),
		Phone:     req.Phone,
		IsActive:  req.IsActive,
		BranchID:  req.BranchID,
		Roles:     req.Roles,

		ExpectedUpdatedAt: req.UpdatedAt,
	})
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, h.responseFor(r.Context(), *user))
}

func (h *Handler) destroy(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermUserDelete); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	id, err := userPathID(r)
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

func (h *Handler) assignRole(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermUserRoleAssign); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	var req AssignRoleRequest
	if err := h.binder.JSON(w, r, &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	scope, _ := tenancy.FromContext(r.Context())
	user, err := h.service.AssignRoles(r.Context(), scope, identity.UserID, req.UserID, req.Roles)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, h.responseFor(r.Context(), *user))
}

func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermUserUpdate); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	var req ChangePasswordRequest
	if err := h.binder.JSON(w, r, &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	scope, _ := tenancy.FromContext(r.Context())
	if err := h.service.ChangePassword(r.Context(), scope, identity.UserID, req.UserID, req.Password); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Message(w, r, http.StatusOK, "Password changed successfully.")
}

func (h *Handler) profileUpdate(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	var req ProfileUpdateRequest
	if err := h.binder.JSON(w, r, &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	current, err := h.service.GetRecord(r.Context(), identity.UserID)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	name := current.Name
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
	}
	user, err := h.service.ProfileUpdate(r.Context(), identity.UserID, UpdateParams{
		FirstName: req.FirstName,
		LastName:  req.LastName,
		Name:      name,
		Phone:     req.Phone,
	})
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, h.responseFor(r.Context(), *user))
}

func (h *Handler) profileChangePassword(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	var req ProfileChangePasswordRequest
	if err := h.binder.JSON(w, r, &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.service.ProfileChangePassword(r.Context(), identity.UserID, identity.TokenID, req.CurrentPassword, req.Password); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Message(w, r, http.StatusOK, "Password changed successfully.")
}

func (h *Handler) switchBranch(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermUserBranchSwitch); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	branchID, err := userPathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	scope, _ := tenancy.FromContext(r.Context())
	user, err := h.service.SwitchBranch(r.Context(), scope, identity.UserID, branchID)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, h.responseFor(r.Context(), *user))
}

func (h *Handler) responseFor(ctx context.Context, user User) Response {
	roles, err := h.checker.RoleRefs(ctx, user.ID)
	if err != nil {
		roles = nil
	}
	return NewResponse(user, roles, h.photoURL(ctx, user.ID))
}

// updateAvatar replaces a user's profile photo (multipart: id + avatar file).
func (h *Handler) updateAvatar(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if h.photo == nil {
		h.responder.Error(w, r, apperr.New(apperr.ServiceUnavailable, "Avatar uploads are not available."))
		return
	}
	form, err := httpx.Multipart(w, r, avatarMaxBytes+avatarMultipartSlack)
	if err != nil {
		h.responder.Error(w, r, avatarUploadError(err))
		return
	}
	targetID, err := avatarTargetID(form.Value["id"])
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if targetID != identity.UserID {
		if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermUserUpdate); err != nil {
			h.responder.Error(w, r, err)
			return
		}
	}
	scope, _ := tenancy.FromContext(r.Context())
	target, err := h.service.Get(r.Context(), scope, targetID)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	header, err := httpx.FormFile(form, "avatar")
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.photo.UpdateAvatar(r.Context(), identity.UserID, targetID, target.TenantIDValue(), header); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, h.responseFor(r.Context(), *target))
}

// avatarMultipartSlack leaves room for multipart framing.
const avatarMultipartSlack = 64 << 10

// avatarUploadError turns a transport-level size rejection into the same 422
// field error the size rule produces, so clients never see a bare 413 for an
// avatar that is merely too big.
func avatarUploadError(err error) error {
	if apperr.Is(err, apperr.RequestEntityTooLarge) {
		return apperr.Validation(map[string][]string{
			"avatar": {fmt.Sprintf("The avatar field must not be greater than %d kilobytes.", avatarMaxBytes>>10)},
		})
	}
	return err
}

func avatarTargetID(values []string) (int64, error) {
	if len(values) == 0 || values[0] == "" {
		return 0, apperr.Validation(map[string][]string{"id": {"The id field is required."}})
	}
	id, err := strconv.ParseInt(values[0], 10, 64)
	if err != nil || id < 1 {
		return 0, apperr.Validation(map[string][]string{"id": {"The id field must be a positive integer."}})
	}
	return id, nil
}

func (h *Handler) photoURL(ctx context.Context, userID int64) *string {
	if h.photo == nil {
		return nil
	}
	url, err := h.photo.PhotoURL(ctx, userID)
	if err != nil {
		return nil
	}
	return stringPtr(url)
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// importMultipartSlack leaves room for multipart framing.
const importMultipartSlack = 64 << 10

// importUsersBackground queues a background user import (multipart upload).
func (h *Handler) importUsersBackground(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	scope, _ := tenancy.FromContext(r.Context())
	permission := authz.PermUserImport
	if scope.Internal {
		permission = authz.PermUserInternalImport
	}
	if err := h.checker.MustCan(r.Context(), identity.UserID, permission); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if h.exchange == nil {
		h.responder.Error(w, r, apperr.New(apperr.ServiceUnavailable, "User imports are not available."))
		return
	}
	form, err := httpx.Multipart(w, r, maxImportBytes+importMultipartSlack)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	header, err := httpx.FormFile(form, "file")
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	job, err := h.exchange.StartImport(r.Context(), scope, identity.UserID, header)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, BackgroundJobResponse{
		JobID:   derefString(job.QueueJobID),
		Status:  job.Status,
		Message: "Job queued successfully.",
	})
}

// exportUsersBackground queues a background user export.
func (h *Handler) exportUsersBackground(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	scope, _ := tenancy.FromContext(r.Context())
	permission := authz.PermUserExport
	if scope.Internal {
		permission = authz.PermUserInternalExport
	}
	if err := h.checker.MustCan(r.Context(), identity.UserID, permission); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if h.exchange == nil {
		h.responder.Error(w, r, apperr.New(apperr.ServiceUnavailable, "User exports are not available."))
		return
	}
	var req ExportFiltersRequest
	if err := h.binder.JSON(w, r, &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	job, err := h.exchange.StartExport(r.Context(), scope, identity.UserID, ExportRequest{
		Search:   req.Search,
		IsActive: req.IsActive,
		RoleName: req.RoleName,
		OrderBy:  req.OrderBy,
		OrderDir: req.OrderDirection,
	})
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, BackgroundJobResponse{
		JobID:   derefString(job.QueueJobID),
		Status:  job.Status,
		Message: "Job queued successfully.",
	})
}

// importTemplate downloads the CSV import template (heading + sample rows).
func (h *Handler) importTemplate(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermUserImport); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if h.exchange == nil {
		h.responder.Error(w, r, apperr.New(apperr.ServiceUnavailable, "User imports are not available."))
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="user_import_template.csv"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(h.exchange.ImportTemplate()))
}

// --- Internal (tenant-exempt) counterparts ---

// changePasswordInternal sets another user's password across tenants.
func (h *Handler) changePasswordInternal(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermUserInternalUpdate); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	var req ChangePasswordRequest
	if err := h.binder.JSON(w, r, &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	scope, _ := tenancy.FromContext(r.Context())
	if err := h.service.ChangePassword(r.Context(), scope, identity.UserID, req.UserID, req.Password); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Message(w, r, http.StatusOK, "Password changed successfully.")
}

// destroyInternal soft-deletes a user across tenants.
func (h *Handler) destroyInternal(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermUserInternalDelete); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	id, err := userPathID(r)
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

// assignRoleInternal replaces a user's roles across tenants.
func (h *Handler) assignRoleInternal(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermUserInternalRoleAssign); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	var req AssignRoleRequest
	if err := h.binder.JSON(w, r, &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	scope, _ := tenancy.FromContext(r.Context())
	user, err := h.service.AssignRoles(r.Context(), scope, identity.UserID, req.UserID, req.Roles)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, h.responseFor(r.Context(), *user))
}

// updateUserAvatarInternal replaces any user's avatar (path id) across tenants.
func (h *Handler) updateUserAvatarInternal(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermUserInternalUpdate); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if h.photo == nil {
		h.responder.Error(w, r, apperr.New(apperr.ServiceUnavailable, "Avatar uploads are not available."))
		return
	}
	id, err := userPathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	scope, _ := tenancy.FromContext(r.Context())
	target, err := h.service.Get(r.Context(), scope, id)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	form, err := httpx.Multipart(w, r, avatarMaxBytes+avatarMultipartSlack)
	if err != nil {
		h.responder.Error(w, r, avatarUploadError(err))
		return
	}
	header, err := httpx.FormFile(form, "avatar")
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.photo.UpdateAvatar(r.Context(), identity.UserID, id, target.TenantIDValue(), header); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, h.responseFor(r.Context(), *target))
}

// updateProfileAvatarInternal replaces the caller's own avatar.
func (h *Handler) updateProfileAvatarInternal(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if h.photo == nil {
		h.responder.Error(w, r, apperr.New(apperr.ServiceUnavailable, "Avatar uploads are not available."))
		return
	}
	form, err := httpx.Multipart(w, r, avatarMaxBytes+avatarMultipartSlack)
	if err != nil {
		h.responder.Error(w, r, avatarUploadError(err))
		return
	}
	header, err := httpx.FormFile(form, "avatar")
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.photo.UpdateAvatar(r.Context(), identity.UserID, identity.UserID, identity.TenantID, header); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	target, err := h.service.GetRecord(r.Context(), identity.UserID)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, h.responseFor(r.Context(), *target))
}

// exportUsersInternal streams an xlsx export across tenants.
func (h *Handler) exportUsersInternal(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermUserInternalExport); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if h.exchange == nil {
		h.responder.Error(w, r, apperr.New(apperr.ServiceUnavailable, "User exports are not available."))
		return
	}
	var req ExportFiltersRequest
	if err := h.binder.JSON(w, r, &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	scope, _ := tenancy.FromContext(r.Context())
	data, err := h.exchange.ExportUsers(r.Context(), scope, ExportRequest{
		Search:   req.Search,
		IsActive: req.IsActive,
		RoleName: req.RoleName,
		OrderBy:  req.OrderBy,
		OrderDir: req.OrderDirection,
	})
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	fileName := fmt.Sprintf("internal_user_data_%s.xlsx", time.Now().Format("2006-01-02_H-04-05"))
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="`+fileName+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// importUsersInternal imports a spreadsheet synchronously across tenants.
func (h *Handler) importUsersInternal(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermUserInternalImport); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if h.exchange == nil {
		h.responder.Error(w, r, apperr.New(apperr.ServiceUnavailable, "User imports are not available."))
		return
	}
	form, err := httpx.Multipart(w, r, maxImportBytes+importMultipartSlack)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	header, err := httpx.FormFile(form, "file")
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	scope, _ := tenancy.FromContext(r.Context())
	result, err := h.exchange.ImportUsers(r.Context(), scope, identity.UserID, header)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, result)
}

func (req InsertUpdateRequest) toCreateInput() CreateParams {
	var password string
	if req.Password != nil {
		password = *req.Password
	}
	return CreateParams{
		BranchID:  req.BranchID,
		FirstName: req.FirstName,
		LastName:  req.LastName,
		UserName:  req.UserName,
		Name:      strings.TrimSpace(req.Name),
		Email:     strings.TrimSpace(req.Email),
		Password:  password,
		Phone:     req.Phone,
		IsActive:  req.IsActive,
		Roles:     req.Roles,
	}
}

func userPathID(r *http.Request) (int64, error) {
	raw := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		return 0, apperr.New(apperr.InvalidInput, "The provided identifier is invalid.")
	}
	return id, nil
}
