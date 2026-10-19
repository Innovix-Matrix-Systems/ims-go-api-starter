package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/config"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database/dbtest"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/seeder"
)

// TestDeveloperDashboardEndToEnd proves the Phase 9 access model: the seeded
// developer account logs in, reaches the gated /dev API, and the hidden
// Developer role never appears in role responses.
func TestDeveloperDashboardEndToEnd(t *testing.T) {
	pool := dbtest.Pool(t)
	t.Setenv("DATABASE_DSN", dbtest.DSN(t))
	t.Setenv("APP_ENV_FILE", "")
	t.Setenv("BCRYPT_ROUNDS", "4")
	t.Setenv("AUTH_DEFAULT_PASSWORD", "secret123")
	t.Setenv("DEV_DASHBOARD_ENABLED", "true")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}

	dbtest.Clean(t, pool,
		"personal_access_tokens", "role_has_permissions", "model_has_roles",
		"users", "roles", "permissions", "branches", "tenants",
	)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	deps, cleanup, err := NewDependencies(cfg, logger)
	if err != nil {
		t.Fatalf("NewDependencies() error = %v", err)
	}
	t.Cleanup(cleanup)

	ctx := context.Background()
	runSeed := func() {
		t.Helper()
		if err := seeder.Run(ctx, cfg, deps.Tx.Queries(), deps.Tx, deps.Checker, logger); err != nil {
			t.Fatalf("seeder.Run() error = %v", err)
		}
	}
	runSeed()
	runSeed() // idempotency

	router := deps.Router()

	// ── developer login: the hidden role is not exposed in the payload ──
	rec := apiRequest(t, router, http.MethodPost, "/api/v1/login",
		`{"email":"developer@ims.com","password":"secret123","device":"dev-suite"}`, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("developer login status = %d body = %s", rec.Code, rec.Body.String())
	}
	var login struct {
		Data struct {
			Status string `json:"status"`
			Token  string `json:"token"`
			User   struct {
				Roles []struct {
					Name string `json:"name"`
				} `json:"roles"`
			} `json:"user"`
		} `json:"data"`
	}
	decodeBody(t, rec, &login)
	if login.Data.Status != "AUTH_SUCCESS" || login.Data.Token == "" {
		t.Fatalf("developer login payload = %+v", login.Data)
	}
	for _, role := range login.Data.User.Roles {
		if role.Name == authz.DeveloperRole || role.Name == authz.SuperAdminRole {
			t.Fatalf("hidden/privileged role leaked in login payload: %+v", login.Data.User.Roles)
		}
	}
	devToken := login.Data.Token

	// ── /dev/api/me works for the developer account ─────────────────────
	rec = apiRequest(t, router, http.MethodGet, "/dev/api/me", "", devToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("dev me status = %d body = %s", rec.Code, rec.Body.String())
	}
	var me struct {
		Data struct {
			UserID       int64    `json:"user_id"`
			IsSuperAdmin bool     `json:"is_super_admin"`
			Permissions  []string `json:"permissions"`
		} `json:"data"`
	}
	decodeBody(t, rec, &me)
	if me.Data.UserID < 1 || me.Data.IsSuperAdmin {
		t.Fatalf("dev me payload = %+v", me.Data)
	}
	permissions := map[string]bool{}
	for _, name := range me.Data.Permissions {
		permissions[name] = true
	}
	if !permissions[authz.PermDevView] || !permissions[authz.PermDevManage] {
		t.Fatalf("developer permissions missing dev.*: %v", me.Data.Permissions)
	}

	// ── super admin also reaches the surface and is flagged ─────────────
	rec = apiRequest(t, router, http.MethodPost, "/api/v1/login",
		`{"email":"superadmin@ims.com","password":"secret123","device":"dev-suite"}`, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("super admin login status = %d body = %s", rec.Code, rec.Body.String())
	}
	var superLogin struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	decodeBody(t, rec, &superLogin)
	superToken := superLogin.Data.Token
	if superToken == "" {
		t.Fatal("super admin token missing")
	}
	rec = apiRequest(t, router, http.MethodGet, "/dev/api/me", "", superToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("super admin dev me status = %d body = %s", rec.Code, rec.Body.String())
	}
	me = struct {
		Data struct {
			UserID       int64    `json:"user_id"`
			IsSuperAdmin bool     `json:"is_super_admin"`
			Permissions  []string `json:"permissions"`
		} `json:"data"`
	}{}
	decodeBody(t, rec, &me)
	if !me.Data.IsSuperAdmin {
		t.Fatalf("super admin me payload = %+v", me.Data)
	}

	// ── the hidden role is absent from role listings ────────────────────
	acting := map[string]string{"X-Acting-Tenant": "1"}
	rec = apiRequest(t, router, http.MethodGet, "/api/v1/role", "", superToken, acting)
	if rec.Code != http.StatusOK {
		t.Fatalf("role list status = %d body = %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), authz.DeveloperRole) {
		t.Fatalf("hidden developer role leaked in role list: %s", rec.Body.String())
	}

	// ── direct tenant-surface fetch of the hidden role 404s ─────────────
	one := int64(1)
	role, err := deps.Tx.Queries().GetRoleByName(ctx, dbgen.GetRoleByNameParams{
		Name:     authz.DeveloperRole,
		TenantID: &one,
	})
	if err != nil {
		t.Fatalf("lookup developer role: %v", err)
	}
	if !role.Hidden || !role.IsSystem {
		t.Fatalf("developer role flags = hidden:%v system:%v", role.Hidden, role.IsSystem)
	}
	rec = apiRequest(t, router, http.MethodGet, "/api/v1/role/"+strconv.FormatInt(role.ID, 10), "", superToken, acting)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("hidden role fetch status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}

	// ── the hidden role holds every catalog permission ─────────────────────────
	var assigned int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM role_has_permissions WHERE role_id = $1", role.ID,
	).Scan(&assigned); err != nil {
		t.Fatalf("count role permissions: %v", err)
	}
	if assigned != len(authz.Catalog) {
		t.Fatalf("developer role permissions = %d, want %d", assigned, len(authz.Catalog))
	}

	// ── the action audit log is reachable and starts empty ────────────────────
	rec = apiRequest(t, router, http.MethodGet, "/dev/api/actions", "", devToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("dev actions status = %d body = %s", rec.Code, rec.Body.String())
	}
	var actions struct {
		Data []struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	decodeBody(t, rec, &actions)
	if len(actions.Data) != 0 {
		t.Fatalf("actions = %+v, want empty", actions.Data)
	}

	// ── captured request detail returns arrays, never null ───────────────────
	time.Sleep(1200 * time.Millisecond) // let the telemetry collector flush
	rec = apiRequest(t, router, http.MethodGet, "/dev/api/requests?limit=1", "", devToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("dev requests status = %d body = %s", rec.Code, rec.Body.String())
	}
	var requests struct {
		Data []struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	decodeBody(t, rec, &requests)
	if len(requests.Data) == 0 {
		t.Fatal("no captured requests after login")
	}
	rec = apiRequest(t, router, http.MethodGet,
		"/dev/api/requests/"+strconv.FormatInt(requests.Data[0].ID, 10), "", devToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("dev request detail status = %d body = %s", rec.Code, rec.Body.String())
	}
	var detail struct {
		Data struct {
			Queries     []json.RawMessage `json:"queries"`
			CacheEvents []json.RawMessage `json:"cache_events"`
			QueueEvents []json.RawMessage `json:"queue_events"`
			Logs        []json.RawMessage `json:"logs"`
			Exceptions  []json.RawMessage `json:"exceptions"`
		} `json:"data"`
	}
	decodeBody(t, rec, &detail)
	if detail.Data.Queries == nil || detail.Data.CacheEvents == nil ||
		detail.Data.QueueEvents == nil || detail.Data.Logs == nil || detail.Data.Exceptions == nil {
		t.Fatalf("correlated arrays must be [] not null: %+v", detail.Data)
	}

	// ── a background export records its queue dispatch on the request ────────
	rec = apiRequest(t, router, http.MethodPost, "/api/v1/internal/user/export-background",
		`{"order_by":"created_at","is_active":true,"order_direction":"desc"}`, superToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("export-background status = %d body = %s", rec.Code, rec.Body.String())
	}
	exportRequestID := rec.Header().Get("X-Request-ID")
	if exportRequestID == "" {
		t.Fatal("export-background response is missing X-Request-ID")
	}
	time.Sleep(1200 * time.Millisecond) // let the telemetry collector flush
	rec = apiRequest(t, router, http.MethodGet,
		"/dev/api/requests?limit=1&request_id="+exportRequestID, "", devToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("export request lookup status = %d body = %s", rec.Code, rec.Body.String())
	}
	var exportRequests struct {
		Data []struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	decodeBody(t, rec, &exportRequests)
	if len(exportRequests.Data) != 1 {
		t.Fatalf("export request lookup = %+v, want one captured request", exportRequests.Data)
	}
	rec = apiRequest(t, router, http.MethodGet,
		"/dev/api/requests/"+strconv.FormatInt(exportRequests.Data[0].ID, 10), "", devToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("export detail status = %d body = %s", rec.Code, rec.Body.String())
	}
	var exportDetail struct {
		Data struct {
			QueueEvents []struct {
				Event string `json:"event"`
				Kind  string `json:"kind"`
				JobID *int64 `json:"job_id"`
			} `json:"queue_events"`
		} `json:"data"`
	}
	decodeBody(t, rec, &exportDetail)
	if len(exportDetail.Data.QueueEvents) == 0 {
		t.Fatal("background export did not record a queue dispatch")
	}
	found := false
	for _, event := range exportDetail.Data.QueueEvents {
		if event.Kind == "user.export" && event.Event == "dispatched" {
			if event.JobID == nil || *event.JobID < 1 {
				t.Fatalf("dispatched user.export event has no job id: %+v", event)
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("queue events = %+v, want a dispatched user.export", exportDetail.Data.QueueEvents)
	}

	// ── telemetry simulator produces correlated logs and an exception ────────
	rec = apiRequest(t, router, http.MethodPost, "/dev/api/simulate-events",
		`{"logs":2,"level":"error","exception":true}`, devToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("simulate-events status = %d body = %s", rec.Code, rec.Body.String())
	}
	var simulated struct {
		Data struct {
			RequestID string `json:"request_id"`
		} `json:"data"`
	}
	decodeBody(t, rec, &simulated)
	if simulated.Data.RequestID == "" {
		t.Fatal("simulate-events did not return a request id")
	}
	time.Sleep(1300 * time.Millisecond) // let the telemetry collector flush
	rec = apiRequest(t, router, http.MethodGet,
		"/dev/api/logs?limit=10&request_id="+simulated.Data.RequestID, "", devToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("simulated logs status = %d body = %s", rec.Code, rec.Body.String())
	}
	var simulatedLogs struct {
		Data []struct {
			Level string `json:"level"`
		} `json:"data"`
	}
	decodeBody(t, rec, &simulatedLogs)
	if len(simulatedLogs.Data) < 2 {
		t.Fatalf("simulated logs = %+v, want at least 2", simulatedLogs.Data)
	}
	rec = apiRequest(t, router, http.MethodGet,
		"/dev/api/exceptions?limit=10&request_id="+simulated.Data.RequestID, "", devToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("simulated exceptions status = %d body = %s", rec.Code, rec.Body.String())
	}
	var simulatedExceptions struct {
		Data []struct {
			Class string `json:"class"`
		} `json:"data"`
	}
	decodeBody(t, rec, &simulatedExceptions)
	foundException := false
	for _, item := range simulatedExceptions.Data {
		if item.Class == "SimulatedException" {
			foundException = true
			break
		}
	}
	if !foundException {
		t.Fatalf("simulated exceptions = %+v, want SimulatedException", simulatedExceptions.Data)
	}

	// ── per-entity detail endpoints exist and 404 on unknown ids ─────────────
	for _, path := range []string{
		"/dev/api/logs/99999999",
		"/dev/api/exceptions/99999999",
		"/dev/api/queue/jobs/99999999",
	} {
		rec = apiRequest(t, router, http.MethodGet, path, "", devToken, nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404 (body %s)", path, rec.Code, rec.Body.String())
		}
	}
}
