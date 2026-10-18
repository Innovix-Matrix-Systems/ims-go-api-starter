package cachekeys

import (
	"context"
	"strings"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/cache/memory"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/tenancy"
)

func TestKeyShapeAndVersioning(t *testing.T) {
	ctx := context.Background()
	store := memory.New("test-")
	defer store.Close()

	scope := tenancy.Scope{TenantID: 3, BranchID: 7}
	key, err := Key(ctx, store, scope, "users", "filter", 1)
	if err != nil {
		t.Fatalf("Key() error = %v", err)
	}
	if !strings.HasPrefix(key, "tenant:3:branch:7:users:v1:") {
		t.Fatalf("key = %q", key)
	}

	again, err := Key(ctx, store, scope, "users", "filter", 1)
	if err != nil || again != key {
		t.Fatalf("Key() not deterministic: %q vs %q", again, key)
	}

	other, err := Key(ctx, store, scope, "users", "filter", 2)
	if err != nil || other == key {
		t.Fatalf("Key() should differ for different args: %q", other)
	}

	version, err := Bump(ctx, store, "users")
	if err != nil || version != 2 {
		t.Fatalf("Bump() = %d, %v; want 2", version, err)
	}
	bumped, err := Key(ctx, store, scope, "users", "filter", 1)
	if err != nil || bumped == key {
		t.Fatalf("Key() after bump = %q, want different from %q", bumped, key)
	}
	if !strings.Contains(bumped, ":v2:") {
		t.Fatalf("bumped key = %q, want v2", bumped)
	}

	// Other namespaces are unaffected.
	otherNamespace, err := Key(ctx, store, tenancy.Scope{TenantID: 3, BranchID: 7}, "orders", "filter", 1)
	if err != nil || !strings.Contains(otherNamespace, ":v1:") {
		t.Fatalf("other namespace = %q, want untouched v1", otherNamespace)
	}
}

func TestScopeRendering(t *testing.T) {
	cases := []struct {
		scope tenancy.Scope
		want  string
	}{
		{tenancy.Scope{}, "global:"},
		{tenancy.Scope{TenantID: 2}, "tenant:2:"},
		{tenancy.Scope{TenantID: 2, BranchID: 4}, "tenant:2:branch:4:"},
		{tenancy.Scope{Acting: tenancy.ActingTenant, ActingTenantID: 9, TenantID: 1}, "tenant:9:"},
	}
	for _, tc := range cases {
		if got := Scope(tc.scope); got != tc.want {
			t.Errorf("Scope(%+v) = %q, want %q", tc.scope, got, tc.want)
		}
	}
}
