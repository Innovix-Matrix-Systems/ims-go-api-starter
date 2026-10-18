package identity

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database"
)

// Repository is the persistence contract for users.
type Repository interface {
	Get(ctx context.Context, id int64) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	List(ctx context.Context, filter ListFilter) ([]User, error)
	Count(ctx context.Context, filter ListFilter) (int64, error)
	Create(ctx context.Context, input CreateInput) (*User, error)
	Update(ctx context.Context, input UpdateInput) (*User, error)
	Delete(ctx context.Context, id int64, actorID *int64) error
	UpdatePassword(ctx context.Context, id int64, hash string, actorID *int64) error
	SwitchBranch(ctx context.Context, userID, branchID int64) error
	EmailExists(ctx context.Context, email string, excludeID int64) (bool, error)
	PhoneExists(ctx context.Context, phone string, excludeID int64) (bool, error)
	ClearRoles(ctx context.Context, userID int64) error
	AssignRoles(ctx context.Context, userID int64, roleIDs []int64) error
	ListForExport(ctx context.Context, filter ExportFilter) ([]ExportRow, error)

	// authn.UserLookup
	FindByEmail(ctx context.Context, email string) (*authn.UserRecord, error)
	FindByID(ctx context.Context, id int64) (*authn.UserRecord, error)
	TouchLogin(ctx context.Context, userID int64, device string) error

	// branches.UserChecker
	ExistsInTenant(ctx context.Context, tenantID, userID int64) (bool, error)
}

// ListFilter carries user list filters.
type ListFilter struct {
	AllTenants bool
	TenantID   int64
	Search     string
	IsActive   *bool
	RoleName   string
	OrderBy    string
	OrderDir   string
	Limit      int32
	Offset     int32
}

// ExportFilter carries user export filters (a snapshot of the request).
type ExportFilter struct {
	AllTenants bool
	TenantID   int64
	Search     string
	IsActive   *bool
	RoleName   string
	OrderBy    string
	OrderDir   string
	Limit      int32
	Offset     int32
}

// ExportRow is one export-ready user row (roles/branch pre-joined).
type ExportRow struct {
	ID         int64
	Name       string
	Email      string
	Phone      *string
	IsActive   bool
	BranchName string
	RoleNames  string
	CreatedAt  time.Time
}

// CreateInput carries user creation input.
type CreateInput struct {
	TenantID        *int64
	BranchID        *int64
	ActiveBranchID  *int64
	FirstName       *string
	LastName        *string
	UserName        *string
	Name            string
	Email           string
	Password        string
	Phone           *string
	IsActive        bool
	EmailVerifiedAt time.Time
	ActorID         *int64
}

// UpdateInput carries user update input.
type UpdateInput struct {
	ID        int64
	FirstName *string
	LastName  *string
	Name      string
	Phone     *string
	IsActive  bool
	BranchID  *int64
	ActorID   *int64
}

type repository struct {
	queries *dbgen.Queries
}

// NewRepository creates the sqlc-backed user repository.
func NewRepository(queries *dbgen.Queries) Repository {
	return &repository{queries: queries}
}

func (r *repository) Get(ctx context.Context, id int64) (*User, error) {
	return r.findByID(ctx, id, "users.get")
}

func (r *repository) GetByEmail(ctx context.Context, email string) (*User, error) {
	row, err := r.queries.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "User with email '%s' was not found.", email)
		}
		return nil, database.MapError("users.get_by_email", err)
	}
	user := mapUser(row)
	return &user, nil
}

func (r *repository) List(ctx context.Context, filter ListFilter) ([]User, error) {
	rows, err := r.queries.ListUsers(ctx, dbgen.ListUsersParams{
		AllTenants: filter.AllTenants,
		TenantID:   optionalInt64(filter.TenantID),
		Search:     optionalString(filter.Search),
		IsActive:   boolToSmallint(filter.IsActive),
		RoleName:   optionalString(filter.RoleName),
		OrderBy:    filter.OrderBy,
		OrderDir:   filter.OrderDir,
		PageLimit:  filter.Limit,
		PageOffset: filter.Offset,
	})
	if err != nil {
		return nil, database.MapError("users.list", err)
	}
	items := make([]User, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapUser(row))
	}
	return items, nil
}

func (r *repository) Count(ctx context.Context, filter ListFilter) (int64, error) {
	total, err := r.queries.CountUsers(ctx, dbgen.CountUsersParams{
		AllTenants: filter.AllTenants,
		TenantID:   optionalInt64(filter.TenantID),
		Search:     optionalString(filter.Search),
		IsActive:   boolToSmallint(filter.IsActive),
		RoleName:   optionalString(filter.RoleName),
	})
	if err != nil {
		return 0, database.MapError("users.count", err)
	}
	return total, nil
}

// ListForExport streams export-ready user rows (roles and branch pre-joined).
func (r *repository) ListForExport(ctx context.Context, filter ExportFilter) ([]ExportRow, error) {
	rows, err := r.queries.ListUsersForExport(ctx, dbgen.ListUsersForExportParams{
		AllTenants: filter.AllTenants,
		TenantID:   filter.TenantID,
		Search:     optionalString(filter.Search),
		IsActive:   filter.IsActive,
		RoleName:   optionalString(filter.RoleName),
		OrderBy:    filter.OrderBy,
		OrderDir:   filter.OrderDir,
		PageLimit:  filter.Limit,
		PageOffset: filter.Offset,
	})
	if err != nil {
		return nil, database.MapError("users.list_export", err)
	}
	items := make([]ExportRow, 0, len(rows))
	for _, row := range rows {
		items = append(items, ExportRow{
			ID:         row.ID,
			Name:       row.Name,
			Email:      row.Email,
			Phone:      row.Phone,
			IsActive:   row.IsActive == 1,
			BranchName: row.BranchName,
			RoleNames:  row.RoleNames,
			CreatedAt:  row.CreatedAt.Time,
		})
	}
	return items, nil
}

func (r *repository) Create(ctx context.Context, input CreateInput) (*User, error) {
	row, err := r.queries.CreateUser(ctx, dbgen.CreateUserParams{
		TenantID:        input.TenantID,
		BranchID:        input.BranchID,
		ActiveBranchID:  input.ActiveBranchID,
		FirstName:       input.FirstName,
		LastName:        input.LastName,
		UserName:        input.UserName,
		Name:            input.Name,
		Email:           input.Email,
		EmailVerifiedAt: timestamptz(input.EmailVerifiedAt),
		Phone:           input.Phone,
		Password:        input.Password,
		IsActive:        boolToInt16(input.IsActive),
		CreatedBy:       input.ActorID,
		UpdatedBy:       input.ActorID,
	})
	if err != nil {
		return nil, database.MapError("users.create", err)
	}
	user := mapUser(row)
	return &user, nil
}

func (r *repository) Update(ctx context.Context, input UpdateInput) (*User, error) {
	row, err := r.queries.UpdateUser(ctx, dbgen.UpdateUserParams{
		ID:        input.ID,
		FirstName: input.FirstName,
		LastName:  input.LastName,
		Name:      input.Name,
		Phone:     input.Phone,
		IsActive:  boolToInt16(input.IsActive),
		BranchID:  input.BranchID,
		UpdatedBy: input.ActorID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "User with ID '%d' was not found.", input.ID)
		}
		return nil, database.MapError("users.update", err)
	}
	user := mapUser(row)
	return &user, nil
}

func (r *repository) Delete(ctx context.Context, id int64, actorID *int64) error {
	affected, err := r.queries.SoftDeleteUser(ctx, dbgen.SoftDeleteUserParams{ID: id, UpdatedBy: actorID})
	if err != nil {
		return database.MapError("users.delete", err)
	}
	if affected == 0 {
		return apperr.Newf(apperr.ResourceNotFound, "User with ID '%d' was not found.", id)
	}
	return nil
}

func (r *repository) UpdatePassword(ctx context.Context, id int64, hash string, actorID *int64) error {
	affected, err := r.queries.UpdateUserPassword(ctx, dbgen.UpdateUserPasswordParams{
		ID:        id,
		Password:  hash,
		UpdatedBy: actorID,
	})
	if err != nil {
		return database.MapError("users.update_password", err)
	}
	if affected == 0 {
		return apperr.Newf(apperr.ResourceNotFound, "User with ID '%d' was not found.", id)
	}
	return nil
}

func (r *repository) SwitchBranch(ctx context.Context, userID, branchID int64) error {
	affected, err := r.queries.SwitchUserBranch(ctx, dbgen.SwitchUserBranchParams{
		ID:       userID,
		BranchID: &branchID,
	})
	if err != nil {
		return database.MapError("users.switch_branch", err)
	}
	if affected == 0 {
		return apperr.Newf(apperr.ResourceNotFound, "User with ID '%d' was not found.", userID)
	}
	return nil
}

func (r *repository) EmailExists(ctx context.Context, email string, excludeID int64) (bool, error) {
	exists, err := r.queries.EmailExists(ctx, dbgen.EmailExistsParams{Email: email, ExcludeID: excludeID})
	if err != nil {
		return false, database.MapError("users.email_exists", err)
	}
	return exists, nil
}

func (r *repository) PhoneExists(ctx context.Context, phone string, excludeID int64) (bool, error) {
	exists, err := r.queries.PhoneExists(ctx, dbgen.PhoneExistsParams{Phone: optionalString(phone), ExcludeID: excludeID})
	if err != nil {
		return false, database.MapError("users.phone_exists", err)
	}
	return exists, nil
}

func (r *repository) ClearRoles(ctx context.Context, userID int64) error {
	if err := r.queries.ClearUserRoles(ctx, dbgen.ClearUserRolesParams{
		ModelType: authz.ModelTypeUser,
		ModelID:   userID,
	}); err != nil {
		return database.MapError("users.clear_roles", err)
	}
	return nil
}

func (r *repository) AssignRoles(ctx context.Context, userID int64, roleIDs []int64) error {
	if len(roleIDs) == 0 {
		return nil
	}
	if err := r.queries.AssignUserRoles(ctx, dbgen.AssignUserRolesParams{
		RoleIds:   roleIDs,
		ModelType: authz.ModelTypeUser,
		ModelID:   userID,
	}); err != nil {
		return database.MapError("users.assign_roles", err)
	}
	return nil
}

func (r *repository) FindByEmail(ctx context.Context, email string) (*authn.UserRecord, error) {
	user, err := r.GetByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	return user.toRecord(), nil
}

func (r *repository) FindByID(ctx context.Context, id int64) (*authn.UserRecord, error) {
	user, err := r.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return user.toRecord(), nil
}

func (r *repository) TouchLogin(ctx context.Context, userID int64, device string) error {
	if _, err := r.queries.TouchUserLogin(ctx, dbgen.TouchUserLoginParams{
		ID:     userID,
		Device: optionalString(device),
	}); err != nil {
		return database.MapError("users.touch_login", err)
	}
	return nil
}

func (r *repository) ExistsInTenant(ctx context.Context, tenantID, userID int64) (bool, error) {
	user, err := r.Get(ctx, userID)
	if err != nil {
		if apperr.Is(err, apperr.ResourceNotFound) {
			return false, nil
		}
		return false, err
	}
	return user.TenantIDValue() == tenantID, nil
}

func (r *repository) findByID(ctx context.Context, id int64, op string) (*User, error) {
	row, err := r.queries.GetUser(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "User with ID '%d' was not found.", id)
		}
		return nil, database.MapError(op, err)
	}
	user := mapUser(row)
	return &user, nil
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func optionalInt64(value int64) *int64 {
	if value == 0 {
		return nil
	}
	return &value
}

func boolToInt16(value bool) int16 {
	if value {
		return 1
	}
	return 0
}

func boolToSmallint(value *bool) *int16 {
	if value == nil {
		return nil
	}
	converted := boolToInt16(*value)
	return &converted
}
