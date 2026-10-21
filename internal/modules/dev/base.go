package dev

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/access"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/backup"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/featureflags"
	filesmod "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/files"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/identity"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/settings"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/queue"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/telemetry"
)

const (
	defaultAdminLimit = 25
	maxAdminLimit     = 100
)

// Deps is the dependency set for the developer dashboard handler. It replaces
// the positional constructor so each management panel can grow its own
// dependencies without churn at every call site.
type Deps struct {
	Checker       *authz.Checker
	Permissions   *access.PermissionService
	QueueAdmin    queue.Admin
	Queue         queue.Engine
	Playground    bool
	Actions       ActionRepository
	Telemetry     TelemetryRepository
	Sessions      SessionRepository
	Developers    DeveloperRepository
	Files         FileRepository
	FileService   *filesmod.Service
	Backups       *backup.Service
	Targets       TargetRepository
	FeatureFlags  *featureflags.Service
	Settings      *settings.Service
	Identity      *identity.Service
	Exchange      *identity.ExchangeService
	Roles         access.RoleRepository
	RoleService   *access.RoleService
	Collector     *telemetry.Collector
	Logger        *slog.Logger
	RetentionDays int
	Binder        *httpx.Binder
	Validator     *httpx.Validator
	Responder     *httpx.Responder
}

// panelBase carries the dependencies and helpers shared by every dashboard
// panel. Panels are plain files on Handler today; the embedded base keeps the
// shared surface in one place as panels are added.
type panelBase struct {
	checker     *authz.Checker
	permissions *access.PermissionService
	actions     ActionRepository
	logger      *slog.Logger
	binder      *httpx.Binder
	validator   *httpx.Validator
	responder   *httpx.Responder
}

// requireManage resolves the caller and enforces dev.manage for mutations.
func (h *Handler) requireManage(w http.ResponseWriter, r *http.Request) (int64, bool) {
	identity, ok := authn.IdentityFromContext(r.Context())
	if !ok {
		h.responder.Error(w, r, apperr.New(apperr.Unauthorized, "Authentication is required."))
		return 0, false
	}
	if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermDevManage); err != nil {
		h.responder.Error(w, r, err)
		return 0, false
	}
	return identity.UserID, true
}

func parseLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultAdminLimit, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > maxAdminLimit {
		return 0, apperr.Validation(map[string][]string{
			"limit": {"The limit must be between 1 and 100."},
		})
	}
	return value, nil
}

func pathID(r *http.Request) (int64, error) {
	return pathParamID(r, "id")
}

func pathParamID(r *http.Request, name string) (int64, error) {
	raw := chi.URLParam(r, name)
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		return 0, apperr.New(apperr.InvalidInput, "The provided identifier is invalid.")
	}
	return id, nil
}

func parseCursor(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, apperr.Validation(map[string][]string{"cursor": {"The cursor is invalid."}})
	}
	return value, nil
}

func parseOptionalID(raw, field string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 1 {
		return 0, apperr.Validation(map[string][]string{field: {"The identifier is invalid."}})
	}
	return value, nil
}
