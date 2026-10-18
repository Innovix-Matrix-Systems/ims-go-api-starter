package featureflags

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/cache/memory"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database/dbtest"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/tenancy"
)

func TestResolutionAndSync(t *testing.T) {
	queries, pool := dbtest.Queries(t)
	dbtest.Clean(t, pool,
		"feature_flags", "personal_access_tokens", "role_has_permissions",
		"model_has_roles", "users", "roles", "permissions", "branches", "tenants",
	)

	store := memory.New("flags-")
	defer store.Close()
	service := NewService(NewRepository(queries), store)
	ctx := context.Background()

	tenantA := createTenant(t, ctx, pool, "Flags A", "flags-a")
	tenantB := createTenant(t, ctx, pool, "Flags B", "flags-b")
	userA := createUser(t, ctx, pool, tenantA, "flag-a@example.com")

	// Sync seeds global rows; running twice is idempotent.
	created, err := service.Sync(ctx)
	if err != nil || created != len(Catalog) {
		t.Fatalf("Sync() = %d, %v; want %d", created, err, len(Catalog))
	}
	if created, err = service.Sync(ctx); err != nil || created != 0 {
		t.Fatalf("second Sync() = %d, %v; want 0", created, err)
	}

	scopeA := tenancy.Scope{TenantID: tenantA, BranchID: 1}
	scopeB := tenancy.Scope{TenantID: tenantB, BranchID: 1}

	// Catalog defaults apply with no overrides.
	if enabled, err := service.IsEnabled(ctx, scopeA, 0, "feature.audit"); err != nil || enabled {
		t.Fatalf("default feature.audit = %v, %v; want false", enabled, err)
	}
	if enabled, err := service.IsEnabled(ctx, scopeA, 0, "feature.data_processing"); err != nil || !enabled {
		t.Fatalf("default feature.data_processing = %v, %v; want true", enabled, err)
	}

	// Global override.
	enabled := true
	if _, err := service.SetBatch(ctx, 0, Target{Type: TargetGlobal}, map[string]*bool{"feature.audit": &enabled}); err != nil {
		t.Fatalf("SetBatch(global): %v", err)
	}
	if on, err := service.IsEnabled(ctx, scopeA, 0, "feature.audit"); err != nil || !on {
		t.Fatalf("global override = %v, %v; want true", on, err)
	}

	// Tenant override beats global and is isolated per tenant.
	disabled := false
	if _, err := service.SetBatch(ctx, 0, Target{Type: TargetTenant, ID: tenantA}, map[string]*bool{"feature.audit": &disabled}); err != nil {
		t.Fatalf("SetBatch(tenant A): %v", err)
	}
	if on, err := service.IsEnabled(ctx, scopeA, 0, "feature.audit"); err != nil || on {
		t.Fatalf("tenant A override = %v, %v; want false", on, err)
	}
	if on, err := service.IsEnabled(ctx, scopeB, 0, "feature.audit"); err != nil || !on {
		t.Fatalf("tenant B = %v, %v; want global true", on, err)
	}

	// User override beats the tenant override.
	if _, err := service.SetBatch(ctx, 0, Target{Type: TargetUser, ID: userA}, map[string]*bool{"feature.audit": &enabled}); err != nil {
		t.Fatalf("SetBatch(user): %v", err)
	}
	if on, err := service.IsEnabled(ctx, scopeA, userA, "feature.audit"); err != nil || !on {
		t.Fatalf("user override = %v, %v; want true", on, err)
	}

	// A nil value clears the user override (inherits the tenant value).
	if _, err := service.SetBatch(ctx, 0, Target{Type: TargetUser, ID: userA}, map[string]*bool{"feature.audit": nil}); err != nil {
		t.Fatalf("SetBatch(user clear): %v", err)
	}
	if on, err := service.IsEnabled(ctx, scopeA, userA, "feature.audit"); err != nil || on {
		t.Fatalf("after clear = %v, %v; want inherited tenant false", on, err)
	}

	// Admin view resolves user -> tenant -> global.
	items, err := service.EffectiveForTarget(ctx, Target{Type: TargetUser, ID: userA})
	if err != nil {
		t.Fatalf("EffectiveForTarget(user): %v", err)
	}
	for _, item := range items {
		if item.Flag.Key == "feature.audit" && item.Source != "tenant" {
			t.Fatalf("feature.audit source = %q; want tenant", item.Source)
		}
	}

	// Unknown keys are rejected.
	if _, err := service.SetBatch(ctx, 0, Target{Type: TargetGlobal}, map[string]*bool{"nope": &enabled}); !apperr.Is(err, apperr.ResourceNotFound) {
		t.Fatalf("SetBatch(unknown) error = %v; want RESOURCE_NOT_FOUND", err)
	}
	// Tenant target without an id is invalid.
	if _, err := service.SetBatch(ctx, 0, Target{Type: TargetTenant}, map[string]*bool{"feature.audit": &enabled}); !apperr.Is(err, apperr.ValidationFailed) {
		t.Fatalf("SetBatch(tenant no id) error = %v; want VALIDATION_FAILED", err)
	}
	// A nonexistent user is invalid.
	if _, err := service.SetBatch(ctx, 0, Target{Type: TargetUser, ID: 999999}, map[string]*bool{"feature.audit": &enabled}); !apperr.Is(err, apperr.ValidationFailed) {
		t.Fatalf("SetBatch(missing user) error = %v; want VALIDATION_FAILED", err)
	}
}

func createTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name, code string) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(ctx,
		`INSERT INTO tenants (name, code, is_active) VALUES ($1, $2, true) RETURNING id`,
		name, code,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert tenant %q: %v", name, err)
	}
	return id
}

func createUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID int64, email string) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(ctx,
		`INSERT INTO users (tenant_id, name, email, password, is_active) VALUES ($1, $2, $3, $4, 1) RETURNING id`,
		tenantID, "Flag User", email, "x",
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert user %q: %v", email, err)
	}
	return id
}
