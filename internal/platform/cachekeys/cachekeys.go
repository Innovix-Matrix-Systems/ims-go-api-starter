// Package cachekeys implements versioned, scope-aware cache keys. A namespace
// version stored in the cache
// is embedded in every key, so bumping the version invalidates the whole
// namespace on any driver without scanning keys.
package cachekeys

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/cache"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/tenancy"
)

const versionPrefix = "cachekey:version:"

// Scope renders the tenant/branch scope prefix for a key.
func Scope(scope tenancy.Scope) string {
	tenantID := scope.EffectiveTenantID()
	if tenantID == 0 {
		return "global:"
	}
	branchID := scope.EffectiveBranchID()
	if branchID == 0 {
		return fmt.Sprintf("tenant:%d:", tenantID)
	}
	return fmt.Sprintf("tenant:%d:branch:%d:", tenantID, branchID)
}

// Version returns the current version of a namespace (1 when unset).
func Version(ctx context.Context, store cache.Store, namespace string) (int64, error) {
	var version int64
	found, err := store.Get(ctx, versionPrefix+namespace, &version)
	if err != nil {
		return 0, err
	}
	if !found || version < 1 {
		return 1, nil
	}
	return version, nil
}

// Bump invalidates a namespace by moving it to the next version.
func Bump(ctx context.Context, store cache.Store, namespace string) (int64, error) {
	version, err := store.Increment(ctx, versionPrefix+namespace, 1)
	if err != nil {
		return 0, err
	}
	if version < 2 {
		version = 2
		if err := store.Set(ctx, versionPrefix+namespace, version, 0); err != nil {
			return 0, err
		}
	}
	return version, nil
}

// Key renders a versioned, scope-aware cache key for the given namespace and
// argument tuple.
func Key(ctx context.Context, store cache.Store, scope tenancy.Scope, namespace string, args ...any) (string, error) {
	version, err := Version(ctx, store, namespace)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("cachekeys: encode arguments: %w", err)
	}
	sum := md5.Sum(data)
	return Scope(scope) + namespace + ":v" + strconv.FormatInt(version, 10) + ":" + hex.EncodeToString(sum[:]), nil
}
