package billing

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/billing/domain"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/billing/service"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/tenancy"
)

// Handler serves billing HTTP endpoints.
type Handler struct {
	packages      *service.PackageService
	subscriptions *service.SubscriptionService
	payments      *service.PaymentService
	approvals     *service.ApprovalService
	checker       *authz.Checker
	binder        *httpx.Binder
	validator     *httpx.Validator
	responder     *httpx.Responder
}

// NewHandler creates the billing handler.
func NewHandler(
	packages *service.PackageService,
	subscriptions *service.SubscriptionService,
	payments *service.PaymentService,
	approvals *service.ApprovalService,
	checker *authz.Checker,
	binder *httpx.Binder,
	validator *httpx.Validator,
	responder *httpx.Responder,
) *Handler {
	return &Handler{
		packages:      packages,
		subscriptions: subscriptions,
		payments:      payments,
		approvals:     approvals,
		checker:       checker,
		binder:        binder,
		validator:     validator,
		responder:     responder,
	}
}

func (h *Handler) publicPackages(w http.ResponseWriter, r *http.Request) {
	items, err := h.packages.ListPublic(r.Context())
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	data := make([]PackageResponse, 0, len(items))
	for _, item := range items {
		data = append(data, NewPackageResponse(item))
	}
	h.responder.Data(w, r, http.StatusOK, data)
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	scope, _ := tenancy.FromContext(r.Context())
	status, err := h.subscriptions.Status(r.Context(), scope.EffectiveTenantID())
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	response := StatusResponse{Active: status.Active}
	if status.Subscription != nil {
		subscription := NewSubscriptionResponse(*status.Subscription)
		response.Subscription = &subscription
	}
	if status.Package != nil {
		pkg := NewPackageResponse(*status.Package)
		response.Package = &pkg
	}
	h.responder.Data(w, r, http.StatusOK, response)
}

func (h *Handler) listTenantPayments(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, "payment.view"); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	scope, _ := tenancy.FromContext(r.Context())
	page, perPage, offset := httpx.PageParams(r)
	items, total, err := h.payments.ListForTenant(r.Context(), scope.EffectiveTenantID(), int32(perPage), int32(offset))
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	data := make([]PaymentResponse, 0, len(items))
	for _, item := range items {
		data = append(data, NewPaymentResponse(item))
	}
	meta, links := httpx.NewPagination(r, page, perPage, total)
	h.responder.Paginated(w, r, http.StatusOK, data, meta, links)
}

func (h *Handler) subscribe(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, "subscription.manage"); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	var req SubscribeRequest
	if err := h.binder.JSON(w, r, &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	scope, _ := tenancy.FromContext(r.Context())
	subscription, payment, err := h.subscriptions.Subscribe(r.Context(), scope.EffectiveTenantID(), req.PackageID, req.PaymentMethod, req.PaymentData, identity.UserID)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusCreated, map[string]any{
		"subscription": NewSubscriptionResponse(*subscription),
		"payment":      NewPaymentResponse(*payment),
	})
}

func (h *Handler) callback(w http.ResponseWriter, r *http.Request) {
	gateway := chi.URLParam(r, "gateway")
	payload := map[string]any{}
	for key, values := range r.URL.Query() {
		if len(values) > 0 {
			payload[key] = values[0]
		}
	}

	if domain.IsManualMethod(gateway) {
		h.responder.Error(w, r, apperr.New(apperr.InvalidInput, "Manual payment methods do not accept callbacks."))
		return
	}
	gatewayRef := queryValue(payload, "paymentID")
	if gatewayRef == "" {
		h.responder.Error(w, r, apperr.New(apperr.ValidationFailed, "The callback is missing its payment reference."))
		return
	}

	payment, err := h.payments.FindByGatewayRef(r.Context(), gatewayRef)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if _, err := h.subscriptions.HandleGatewayCallback(r.Context(), payment.ID, gateway, payload); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Message(w, r, http.StatusOK, "Payment callback processed.")
}

func (h *Handler) listPackages(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, "package.view.all"); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	items, err := h.packages.List(r.Context())
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	data := make([]PackageResponse, 0, len(items))
	for _, item := range items {
		data = append(data, NewPackageResponse(item))
	}
	h.responder.Data(w, r, http.StatusOK, data)
}

func (h *Handler) showPackage(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, "package.view"); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	id, err := billingPathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	pkg, err := h.packages.Get(r.Context(), id)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, NewPackageResponse(*pkg))
}

func (h *Handler) storePackage(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, "package.create"); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	var req PackageInsertRequest
	if err := h.binder.JSON(w, r, &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	pkg, err := h.packages.Create(r.Context(), req.toInput(), req.toFeatures())
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusCreated, NewPackageResponse(*pkg))
}

func (h *Handler) updatePackage(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, "package.update"); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	id, err := billingPathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	var req PackageInsertRequest
	if err := h.binder.JSON(w, r, &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	pkg, err := h.packages.Update(r.Context(), id, req.toInput(), req.toFeatures())
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, NewPackageResponse(*pkg))
}

func (h *Handler) destroyPackage(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, "package.delete"); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	id, err := billingPathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.packages.Delete(r.Context(), id); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.NoContent(w)
}

func (h *Handler) listSubscriptions(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, "subscription.view.all"); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	page, perPage, offset := httpx.PageParams(r)
	items, total, err := h.subscriptions.List(r.Context(), r.URL.Query().Get("status"), int32(perPage), int32(offset))
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	data := make([]SubscriptionResponse, 0, len(items))
	for _, item := range items {
		data = append(data, NewSubscriptionResponse(item))
	}
	meta, links := httpx.NewPagination(r, page, perPage, total)
	h.responder.Paginated(w, r, http.StatusOK, data, meta, links)
}

func (h *Handler) cancelSubscription(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, "subscription.manage"); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	id, err := billingPathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	var req CancelRequest
	if err := h.binder.JSON(w, r, &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	subscription, err := h.subscriptions.Cancel(r.Context(), id, req.Reason)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, NewSubscriptionResponse(*subscription))
}

func (h *Handler) overrides(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, "subscription.override"); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	id, err := billingPathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	var req OverridesRequest
	if err := h.binder.JSON(w, r, &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	subscription, err := h.subscriptions.ApplyOverrides(r.Context(), id, req.Overrides)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, NewSubscriptionResponse(*subscription))
}

func (h *Handler) listAllPayments(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, "payment.view.all"); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	page, perPage, offset := httpx.PageParams(r)
	items, total, err := h.payments.List(r.Context(), r.URL.Query().Get("status"), int32(perPage), int32(offset))
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	data := make([]PaymentResponse, 0, len(items))
	for _, item := range items {
		data = append(data, NewPaymentResponse(item))
	}
	meta, links := httpx.NewPagination(r, page, perPage, total)
	h.responder.Paginated(w, r, http.StatusOK, data, meta, links)
}

func (h *Handler) recordPayment(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, "payment.record"); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	var req RecordPaymentRequest
	if err := h.binder.JSON(w, r, &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	notes := ""
	if req.Notes != nil {
		notes = *req.Notes
	}
	if _, err := h.subscriptions.GetForTenant(r.Context(), req.TenantID, req.SubscriptionID); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	payment, err := h.payments.RecordManual(r.Context(), req.TenantID, req.SubscriptionID, req.PaymentMethod, req.Amount, notes, identity.UserID)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if _, err := h.subscriptions.Activate(r.Context(), req.SubscriptionID); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusCreated, NewPaymentResponse(*payment))
}

func (h *Handler) listApprovals(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, "payment_approval.view.all"); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	page, perPage, offset := httpx.PageParams(r)
	items, total, err := h.approvals.List(r.Context(), r.URL.Query().Get("status"), int32(perPage), int32(offset))
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	data := make([]ApprovalResponse, 0, len(items))
	for _, item := range items {
		data = append(data, NewApprovalResponse(item))
	}
	meta, links := httpx.NewPagination(r, page, perPage, total)
	h.responder.Paginated(w, r, http.StatusOK, data, meta, links)
}

func (h *Handler) approve(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, "payment_approval.review"); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	id, err := billingPathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	var req ReviewRequest
	if err := h.binder.JSON(w, r, &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	approval, err := h.approvals.Approve(r.Context(), id, identity.UserID, req.Note)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, NewApprovalResponse(*approval))
}

func (h *Handler) reject(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustCan(r.Context(), identity.UserID, "payment_approval.review"); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	id, err := billingPathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	var req ReviewRequest
	if err := h.binder.JSON(w, r, &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	approval, err := h.approvals.Reject(r.Context(), id, identity.UserID, req.Note)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, NewApprovalResponse(*approval))
}

func billingPathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 1 {
		return 0, apperr.New(apperr.InvalidInput, "The provided identifier is invalid.")
	}
	return id, nil
}

func queryValue(payload map[string]any, key string) string {
	if value, ok := payload[key].(string); ok {
		return value
	}
	return ""
}
