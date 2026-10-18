// Package auth serves the authentication HTTP endpoints (login, OTP, logout).
package auth

import (
	"net"
	"net/http"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/identity"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
)

// Handler serves authentication endpoints.
type Handler struct {
	service   *authn.Service
	users     *identity.Service
	checker   *authz.Checker
	binder    *httpx.Binder
	validator *httpx.Validator
	responder *httpx.Responder
}

// NewHandler creates the auth handler.
func NewHandler(service *authn.Service, users *identity.Service, checker *authz.Checker, binder *httpx.Binder, validator *httpx.Validator, responder *httpx.Responder) *Handler {
	return &Handler{service: service, users: users, checker: checker, binder: binder, validator: validator, responder: responder}
}

// LoginRequest is the login payload.
type LoginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=6"`
	Device   string `json:"device" validate:"required,max=100"`
}

// VerifyOTPRequest is the OTP completion payload.
type VerifyOTPRequest struct {
	Email  string `json:"email" validate:"required,email"`
	OTP    string `json:"otp" validate:"required"`
	Device string `json:"device" validate:"required,max=100"`
}

// LogoutRequest revokes one device token.
type LogoutRequest struct {
	Device string `json:"device" validate:"required,max=100"`
}

type loginData struct {
	User    identity.Response `json:"user"`
	Status  string            `json:"status"`
	Message string            `json:"message"`
	Token   *string           `json:"token"`
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := h.binder.JSON(w, r, &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	result, err := h.service.Login(r.Context(), req.Email, req.Password, req.Device, clientIP(r))
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.respondLogin(w, r, result)
}

func (h *Handler) verifyOTP(w http.ResponseWriter, r *http.Request) {
	var req VerifyOTPRequest
	if err := h.binder.JSON(w, r, &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	result, err := h.service.VerifyOTP(r.Context(), req.Email, req.OTP, req.Device, clientIP(r))
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.respondLogin(w, r, result)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	var req LogoutRequest
	if err := h.binder.JSON(w, r, &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.service.Logout(r.Context(), identity.UserID, req.Device); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.NoContent(w)
}

func (h *Handler) respondLogin(w http.ResponseWriter, r *http.Request, result *authn.LoginResult) {
	user, err := h.users.GetRecord(r.Context(), result.User.ID)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	roles, err := h.checker.RoleRefs(r.Context(), user.ID)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	var token *string
	if result.Token != nil {
		token = &result.Token.PlainText
	}
	h.responder.Data(w, r, http.StatusOK, loginData{
		User:    identity.NewResponse(*user, roles, nil),
		Status:  result.Status,
		Message: result.Message,
		Token:   token,
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
