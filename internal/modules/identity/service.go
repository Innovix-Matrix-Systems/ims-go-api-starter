package identity

import (
	"context"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/access"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/branches"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/tenancy"
)

// LimitChecker enforces billing plan limits for tenant resources.
type LimitChecker interface {
	RequireWithinLimitByName(ctx context.Context, tenantID int64, feature string, current int64) error
}

// Service implements user business rules.
type Service struct {
	repo       Repository
	roles      access.RoleRepository
	branchRepo branches.Repository
	checker    *authz.Checker
	auth       authn.Authenticator
	cfg        config.Auth
	limits     LimitChecker
}

// NewService creates the user service.
func NewService(repo Repository, roles access.RoleRepository, branchRepo branches.Repository, checker *authz.Checker, auth authn.Authenticator, cfg config.Auth, limits LimitChecker) *Service {
	return &Service{repo: repo, roles: roles, branchRepo: branchRepo, checker: checker, auth: auth, cfg: cfg, limits: limits}
}

// List returns users within the effective scope.
func (s *Service) List(ctx context.Context, scope tenancy.Scope, filter ListFilter) ([]User, int64, error) {
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

// Get returns one user visible in the effective scope.
func (s *Service) Get(ctx context.Context, scope tenancy.Scope, id int64) (*User, error) {
	user, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !scope.AllTenants() && user.TenantIDValue() != scope.EffectiveTenantID() {
		return nil, apperr.Newf(apperr.ResourceNotFound, "User with ID '%d' was not found.", id)
	}
	return user, nil
}

// CreateParams carries user creation input from handlers.
type CreateParams struct {
	TenantID  *int64
	BranchID  *int64
	FirstName *string
	LastName  *string
	UserName  *string
	Name      string
	Email     string
	Password  string
	Phone     *string
	IsActive  *bool
	Roles     []int64
}

// Create adds a user with role assignments.
func (s *Service) Create(ctx context.Context, scope tenancy.Scope, actorID int64, input CreateParams) (*User, error) {
	tenantID := scope.EffectiveTenantID()
	if scope.Internal {
		tenantID = 0
		if input.TenantID != nil {
			tenantID = *input.TenantID
		}
	}
	if !scope.Internal && tenantID == 0 {
		return nil, apperr.New(apperr.TenantContextNotResolved, "A tenant context is required.")
	}
	if tenantID != 0 && s.limits != nil {
		count, err := s.repo.Count(ctx, ListFilter{TenantID: tenantID})
		if err != nil {
			return nil, err
		}
		if err := s.limits.RequireWithinLimitByName(ctx, tenantID, "users.max", count); err != nil {
			return nil, err
		}
	}

	if input.Password == "" {
		return nil, apperr.Validation(map[string][]string{"password": {"The password field is required."}})
	}
	if err := s.validateEmailUnique(ctx, input.Email, 0); err != nil {
		return nil, err
	}
	if err := s.validatePhoneUnique(ctx, input.Phone, 0); err != nil {
		return nil, err
	}
	if scope.Internal && tenantID == 0 {
		if err := s.validateSystemRoles(ctx, input.Roles); err != nil {
			return nil, err
		}
	} else if err := s.validateRoles(ctx, tenantID, input.Roles); err != nil {
		return nil, err
	}

	branchID := input.BranchID
	if err := s.validateBranch(ctx, tenantID, branchID); err != nil {
		return nil, err
	}

	hash, err := s.hashPassword(input.Password)
	if err != nil {
		return nil, err
	}

	isActive := true
	if input.IsActive != nil {
		isActive = *input.IsActive
	}
	var tenant *int64
	if tenantID != 0 {
		tenant = &tenantID
	}
	activeBranch := branchID

	user, err := s.repo.Create(ctx, CreateInput{
		TenantID:        tenant,
		BranchID:        branchID,
		ActiveBranchID:  activeBranch,
		FirstName:       input.FirstName,
		LastName:        input.LastName,
		UserName:        input.UserName,
		Name:            input.Name,
		Email:           input.Email,
		Password:        hash,
		Phone:           input.Phone,
		IsActive:        isActive,
		EmailVerifiedAt: time.Now(),
		ActorID:         &actorID,
	})
	if err != nil {
		return nil, err
	}
	if len(input.Roles) > 0 {
		if err := s.repo.AssignRoles(ctx, user.ID, input.Roles); err != nil {
			return nil, err
		}
	}
	s.checker.InvalidateUser(ctx, user.ID)
	return user, nil
}

// UpdateParams carries user update input from handlers.
type UpdateParams struct {
	FirstName *string
	LastName  *string
	Name      string
	Phone     *string
	IsActive  *bool
	BranchID  *int64
	Roles     []int64
}

// Update modifies a user; super admins are protected from non-super actors.
func (s *Service) Update(ctx context.Context, scope tenancy.Scope, actorID, id int64, input UpdateParams) (*User, error) {
	target, err := s.Get(ctx, scope, id)
	if err != nil {
		return nil, err
	}
	targetIsSuper, err := s.checker.IsSuperAdmin(ctx, target.ID)
	if err != nil {
		return nil, err
	}
	if targetIsSuper {
		actorIsSuper, err := s.checker.IsSuperAdmin(ctx, actorID)
		if err != nil {
			return nil, err
		}
		if !actorIsSuper {
			return nil, apperr.New(apperr.Forbidden, "The super admin user cannot be modified.")
		}
	}

	if err := s.validatePhoneUnique(ctx, input.Phone, target.ID); err != nil {
		return nil, err
	}
	tenantID := target.TenantIDValue()
	if err := s.validateBranch(ctx, tenantID, input.BranchID); err != nil {
		return nil, err
	}
	if err := s.validateRoles(ctx, tenantID, input.Roles); err != nil {
		return nil, err
	}

	isActive := target.IsActive
	if input.IsActive != nil {
		isActive = *input.IsActive
	}
	branchID := input.BranchID
	if branchID == nil {
		branchID = target.BranchID
	}

	updated, err := s.repo.Update(ctx, UpdateInput{
		ID:        target.ID,
		FirstName: input.FirstName,
		LastName:  input.LastName,
		Name:      input.Name,
		Phone:     input.Phone,
		IsActive:  isActive,
		BranchID:  branchID,
		ActorID:   &actorID,
	})
	if err != nil {
		return nil, err
	}
	if input.Roles != nil {
		if err := s.repo.ClearRoles(ctx, target.ID); err != nil {
			return nil, err
		}
		if err := s.repo.AssignRoles(ctx, target.ID, input.Roles); err != nil {
			return nil, err
		}
	}
	s.checker.InvalidateUser(ctx, target.ID)
	return updated, nil
}

// Delete soft-deletes a user, protecting super admins and self-deletion.
func (s *Service) Delete(ctx context.Context, scope tenancy.Scope, actorID, id int64) error {
	target, err := s.Get(ctx, scope, id)
	if err != nil {
		return err
	}
	if target.ID == actorID {
		return apperr.New(apperr.Forbidden, "You cannot delete your own account.")
	}
	targetIsSuper, err := s.checker.IsSuperAdmin(ctx, target.ID)
	if err != nil {
		return err
	}
	if targetIsSuper {
		return apperr.New(apperr.Forbidden, "The super admin user cannot be deleted.")
	}
	if err := s.repo.Delete(ctx, target.ID, &actorID); err != nil {
		return err
	}
	if err := s.auth.RevokeAll(ctx, target.ID); err != nil {
		return err
	}
	s.checker.InvalidateUser(ctx, target.ID)
	return nil
}

// AssignRoles replaces a user's roles.
func (s *Service) AssignRoles(ctx context.Context, scope tenancy.Scope, actorID, targetID int64, roleIDs []int64) (*User, error) {
	if len(roleIDs) == 0 {
		return nil, apperr.Validation(map[string][]string{
			"roles": {"The roles field must have at least one item."},
		})
	}
	target, err := s.Get(ctx, scope, targetID)
	if err != nil {
		return nil, err
	}
	targetIsSuper, err := s.checker.IsSuperAdmin(ctx, target.ID)
	if err != nil {
		return nil, err
	}
	if targetIsSuper {
		actorIsSuper, err := s.checker.IsSuperAdmin(ctx, actorID)
		if err != nil {
			return nil, err
		}
		if !actorIsSuper {
			return nil, apperr.New(apperr.Forbidden, "The super admin user cannot be modified.")
		}
	}
	if scope.Internal && target.TenantIDValue() == 0 {
		if err := s.validateSystemRoles(ctx, roleIDs); err != nil {
			return nil, err
		}
	} else if err := s.validateRoles(ctx, target.TenantIDValue(), roleIDs); err != nil {
		return nil, err
	}
	if err := s.repo.ClearRoles(ctx, target.ID); err != nil {
		return nil, err
	}
	if err := s.repo.AssignRoles(ctx, target.ID, roleIDs); err != nil {
		return nil, err
	}
	s.checker.InvalidateUser(ctx, target.ID)
	return target, nil
}

// GrantRole adds one role without replacing the user's existing roles.
func (s *Service) GrantRole(ctx context.Context, scope tenancy.Scope, targetID, roleID int64) (*User, error) {
	target, err := s.Get(ctx, scope, targetID)
	if err != nil {
		return nil, err
	}
	if err := s.validateRoleForTarget(ctx, scope, target, roleID); err != nil {
		return nil, err
	}
	if err := s.repo.AddRole(ctx, target.ID, roleID); err != nil {
		return nil, err
	}
	s.checker.InvalidateUser(ctx, target.ID)
	return target, nil
}

// RevokeRole removes one role without touching the user's other roles.
func (s *Service) RevokeRole(ctx context.Context, scope tenancy.Scope, targetID, roleID int64) (*User, error) {
	target, err := s.Get(ctx, scope, targetID)
	if err != nil {
		return nil, err
	}
	if err := s.validateRoleForTarget(ctx, scope, target, roleID); err != nil {
		return nil, err
	}
	removed, err := s.repo.RemoveRole(ctx, target.ID, roleID)
	if err != nil {
		return nil, err
	}
	if !removed {
		return nil, apperr.Newf(apperr.ResourceNotFound, "User '%d' does not hold role '%d'.", targetID, roleID)
	}
	s.checker.InvalidateUser(ctx, target.ID)
	return target, nil
}

func (s *Service) validateRoleForTarget(ctx context.Context, scope tenancy.Scope, target *User, roleID int64) error {
	if scope.Internal && target.TenantIDValue() == 0 {
		return s.validateSystemRoles(ctx, []int64{roleID})
	}
	return s.validateRoles(ctx, target.TenantIDValue(), []int64{roleID})
}

// ChangePassword sets a new password (admin flow) and revokes active tokens.
func (s *Service) ChangePassword(ctx context.Context, scope tenancy.Scope, actorID, targetID int64, password string) error {
	target, err := s.Get(ctx, scope, targetID)
	if err != nil {
		return err
	}
	targetIsSuper, err := s.checker.IsSuperAdmin(ctx, target.ID)
	if err != nil {
		return err
	}
	if targetIsSuper {
		actorIsSuper, err := s.checker.IsSuperAdmin(ctx, actorID)
		if err != nil {
			return err
		}
		if !actorIsSuper {
			return apperr.New(apperr.Forbidden, "The super admin user cannot be modified.")
		}
	}
	hash, err := s.hashPassword(password)
	if err != nil {
		return err
	}
	if err := s.repo.UpdatePassword(ctx, target.ID, hash, &actorID); err != nil {
		return err
	}
	return s.auth.RevokeAll(ctx, target.ID)
}

// ProfileUpdate updates the authenticated user's own profile.
func (s *Service) ProfileUpdate(ctx context.Context, userID int64, input UpdateParams) (*User, error) {
	target, err := s.repo.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	if err := s.validatePhoneUnique(ctx, input.Phone, target.ID); err != nil {
		return nil, err
	}
	return s.repo.Update(ctx, UpdateInput{
		ID:        target.ID,
		FirstName: input.FirstName,
		LastName:  input.LastName,
		Name:      input.Name,
		Phone:     input.Phone,
		IsActive:  target.IsActive,
		BranchID:  target.BranchID,
		ActorID:   &userID,
	})
}

// ProfileChangePassword verifies the current password before updating.
func (s *Service) ProfileChangePassword(ctx context.Context, userID int64, currentPassword, newPassword string) error {
	target, err := s.repo.Get(ctx, userID)
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(target.Password), []byte(currentPassword)) != nil {
		return apperr.Validation(map[string][]string{
			"current_password": {"The provided current password is incorrect."},
		})
	}
	hash, err := s.hashPassword(newPassword)
	if err != nil {
		return err
	}
	return s.repo.UpdatePassword(ctx, userID, hash, &userID)
}

// SwitchBranch changes the user's active branch.
func (s *Service) SwitchBranch(ctx context.Context, scope tenancy.Scope, userID, branchID int64) (*User, error) {
	tenantID := scope.EffectiveTenantID()
	if tenantID == 0 {
		return nil, apperr.New(apperr.TenantContextNotResolved, "A tenant context is required.")
	}
	belongs, err := s.branchRepo.BelongsToTenant(ctx, tenantID, branchID)
	if err != nil {
		return nil, err
	}
	if !belongs {
		return nil, apperr.Validation(map[string][]string{
			"branch_id": {"The selected branch is invalid."},
		})
	}
	if err := s.repo.SwitchBranch(ctx, userID, branchID); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, userID)
}

// GetRecord loads a user without scope checks (internal consumers only).
func (s *Service) GetRecord(ctx context.Context, id int64) (*User, error) {
	return s.repo.Get(ctx, id)
}

func (s *Service) validateEmailUnique(ctx context.Context, email string, excludeID int64) error {
	exists, err := s.repo.EmailExists(ctx, email, excludeID)
	if err != nil {
		return err
	}
	if exists {
		return apperr.Validation(map[string][]string{"email": {"The email has already been taken."}})
	}
	return nil
}

func (s *Service) validatePhoneUnique(ctx context.Context, phone *string, excludeID int64) error {
	if phone == nil || *phone == "" {
		return nil
	}
	exists, err := s.repo.PhoneExists(ctx, *phone, excludeID)
	if err != nil {
		return err
	}
	if exists {
		return apperr.Validation(map[string][]string{"phone": {"The phone has already been taken."}})
	}
	return nil
}

func (s *Service) validateRoles(ctx context.Context, tenantID int64, roleIDs []int64) error {
	if len(roleIDs) == 0 {
		return nil
	}
	if tenantID == 0 {
		return apperr.Validation(map[string][]string{"roles": {"Roles cannot be assigned without a tenant context."}})
	}
	for _, roleID := range roleIDs {
		exists, err := s.roles.ExistsForTenant(ctx, tenantID, roleID)
		if err != nil {
			return err
		}
		if !exists {
			return apperr.Validation(map[string][]string{"roles": {"One or more selected roles are invalid."}})
		}
	}
	return nil
}

// validateSystemRoles allows only global system roles (tenantless) to be
// assigned to a tenantless user, e.g. the super-admin from the internal API.
func (s *Service) validateSystemRoles(ctx context.Context, roleIDs []int64) error {
	for _, roleID := range roleIDs {
		role, err := s.roles.GetByID(ctx, roleID)
		if err != nil {
			return apperr.Validation(map[string][]string{"roles": {"One or more selected roles are invalid."}})
		}
		if !role.IsSystem || role.TenantID != nil {
			return apperr.Validation(map[string][]string{"roles": {"Only system roles can be assigned to a user without a tenant."}})
		}
	}
	return nil
}

func (s *Service) validateBranch(ctx context.Context, tenantID int64, branchID *int64) error {
	if branchID == nil {
		return nil
	}
	if tenantID == 0 {
		return apperr.Validation(map[string][]string{"branch_id": {"A branch cannot be assigned without a tenant context."}})
	}
	belongs, err := s.branchRepo.BelongsToTenant(ctx, tenantID, *branchID)
	if err != nil {
		return err
	}
	if !belongs {
		return apperr.Validation(map[string][]string{"branch_id": {"The selected branch is invalid."}})
	}
	return nil
}

func (s *Service) hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), s.cfg.BcryptRounds)
	if err != nil {
		return "", apperr.Wrap(apperr.InternalServerError, "hash password", err)
	}
	return string(hash), nil
}
