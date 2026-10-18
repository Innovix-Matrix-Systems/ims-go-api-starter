package authz

import (
	"context"
	"fmt"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/cache"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database"
)

// DefaultPermissionCacheTTL bounds permission-set staleness. Role/permission
// mutations invalidate the affected users eagerly.
const DefaultPermissionCacheTTL = 10 * time.Minute

// RoleRef is a slim role reference used in response DTOs.
type RoleRef struct {
	ID       int64
	Name     string
	IsSystem bool
}

// Checker answers authorization questions and owns the permission cache.
type Checker struct {
	queries *dbgen.Queries
	cache   cache.Store
	ttl     time.Duration
}

// NewChecker creates the authorization checker.
func NewChecker(queries *dbgen.Queries, store cache.Store) *Checker {
	return &Checker{queries: queries, cache: store, ttl: DefaultPermissionCacheTTL}
}

// IsSuperAdmin reports whether the user holds the Super-Admin system role.
func (c *Checker) IsSuperAdmin(ctx context.Context, userID int64) (bool, error) {
	ok, err := c.queries.UserHasRole(ctx, dbgen.UserHasRoleParams{
		ModelType: ModelTypeUser,
		ModelID:   userID,
		RoleName:  SuperAdminRole,
	})
	if err != nil {
		return false, database.MapError("authz.super_admin", err)
	}
	return ok, nil
}

// Can reports whether the user holds the permission (Super-Admin bypasses).
func (c *Checker) Can(ctx context.Context, userID int64, permission string) (bool, error) {
	super, err := c.IsSuperAdmin(ctx, userID)
	if err != nil {
		return false, err
	}
	if super {
		return true, nil
	}
	permissions, err := c.permissionsFor(ctx, userID)
	if err != nil {
		return false, err
	}
	_, ok := permissions[permission]
	return ok, nil
}

// MustCan returns FORBIDDEN unless the user holds the permission.
func (c *Checker) MustCan(ctx context.Context, userID int64, permission string) error {
	ok, err := c.Can(ctx, userID, permission)
	if err != nil {
		return err
	}
	if !ok {
		return apperr.New(apperr.InsufficientPermissions, "You do not have permission to perform this action.")
	}
	return nil
}

// MustBeSuperAdmin returns FORBIDDEN unless the user holds Super-Admin.
func (c *Checker) MustBeSuperAdmin(ctx context.Context, userID int64) error {
	ok, err := c.IsSuperAdmin(ctx, userID)
	if err != nil {
		return err
	}
	if !ok {
		return apperr.New(apperr.InsufficientPermissions, "You do not have permission to perform this action.")
	}
	return nil
}

// RoleRefs returns the user's roles for response DTOs.
func (c *Checker) RoleRefs(ctx context.Context, userID int64) ([]RoleRef, error) {
	rows, err := c.queries.UserRoleRefs(ctx, dbgen.UserRoleRefsParams{
		ModelType: ModelTypeUser,
		ModelID:   userID,
	})
	if err != nil {
		return nil, database.MapError("authz.role_refs", err)
	}
	refs := make([]RoleRef, 0, len(rows))
	for _, row := range rows {
		refs = append(refs, RoleRef{ID: row.ID, Name: row.Name, IsSystem: row.IsSystem})
	}
	return refs, nil
}

// InvalidateUser drops the cached permission set for one user.
func (c *Checker) InvalidateUser(ctx context.Context, userID int64) {
	_ = c.cache.Delete(ctx, permissionCacheKey(userID))
}

// InvalidateRole drops cached permission sets for every user holding the role.
func (c *Checker) InvalidateRole(ctx context.Context, roleID int64) error {
	userIDs, err := c.queries.ListUserIDsWithRole(ctx, dbgen.ListUserIDsWithRoleParams{
		ModelType: ModelTypeUser,
		RoleID:    roleID,
	})
	if err != nil {
		return database.MapError("authz.invalidate_role", err)
	}
	for _, userID := range userIDs {
		c.InvalidateUser(ctx, userID)
	}
	return nil
}

// InvalidatePermission drops cached permission sets for every user granted the
// permission through one of their roles.
func (c *Checker) InvalidatePermission(ctx context.Context, permissionID int64) error {
	userIDs, err := c.queries.UserIDsWithPermission(ctx, dbgen.UserIDsWithPermissionParams{
		ModelType:    ModelTypeUser,
		PermissionID: permissionID,
	})
	if err != nil {
		return database.MapError("authz.invalidate_permission", err)
	}
	for _, userID := range userIDs {
		c.InvalidateUser(ctx, userID)
	}
	return nil
}

func (c *Checker) permissionsFor(ctx context.Context, userID int64) (map[string]struct{}, error) {
	key := permissionCacheKey(userID)
	var names []string
	found, err := c.cache.Get(ctx, key, &names)
	if err != nil {
		return nil, apperr.Wrap(apperr.InternalServerError, "read permission cache", err)
	}
	if !found {
		names, err = c.queries.UserPermissionNames(ctx, dbgen.UserPermissionNamesParams{
			ModelType: ModelTypeUser,
			ModelID:   userID,
		})
		if err != nil {
			return nil, database.MapError("authz.permissions", err)
		}
		_ = c.cache.Set(ctx, key, names, c.ttl)
	}
	set := make(map[string]struct{}, len(names))
	for _, name := range names {
		set[name] = struct{}{}
	}
	return set, nil
}

func permissionCacheKey(userID int64) string {
	return fmt.Sprintf("perm:user:%d", userID)
}
