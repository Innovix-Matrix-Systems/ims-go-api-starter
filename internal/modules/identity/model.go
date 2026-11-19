package identity

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
)

// User is the domain representation of a user.
type User struct {
	ID               int64
	TenantID         *int64
	BranchID         *int64
	ActiveBranchID   *int64
	FirstName        *string
	LastName         *string
	UserName         *string
	Name             string
	Email            string
	EmailVerifiedAt  *time.Time
	Phone            *string
	PhoneVerifiedAt  *time.Time
	Password         string
	IsActive         bool
	LastLoginAt      *time.Time
	LastActiveDevice *string
	CreatedBy        *int64
	UpdatedBy        *int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DeletedAt        *time.Time
}

func mapUser(row dbgen.User) User {
	return User{
		ID:               row.ID,
		TenantID:         row.TenantID,
		BranchID:         row.BranchID,
		ActiveBranchID:   row.ActiveBranchID,
		FirstName:        row.FirstName,
		LastName:         row.LastName,
		UserName:         row.UserName,
		Name:             row.Name,
		Email:            row.Email,
		EmailVerifiedAt:  timePtr(row.EmailVerifiedAt),
		Phone:            row.Phone,
		PhoneVerifiedAt:  timePtr(row.PhoneVerifiedAt),
		Password:         row.Password,
		IsActive:         row.IsActive == 1,
		LastLoginAt:      timePtr(row.LastLoginAt),
		LastActiveDevice: row.LastActiveDevice,
		CreatedBy:        row.CreatedBy,
		UpdatedBy:        row.UpdatedBy,
		CreatedAt:        row.CreatedAt.Time,
		UpdatedAt:        row.UpdatedAt.Time,
		DeletedAt:        timePtr(row.DeletedAt),
	}
}

func (u User) TenantIDValue() int64 {
	if u.TenantID == nil {
		return 0
	}
	return *u.TenantID
}

func (u User) BranchIDValue() int64 {
	if u.BranchID == nil {
		return 0
	}
	return *u.BranchID
}

func (u User) ActiveBranchIDValue() int64 {
	if u.ActiveBranchID == nil {
		return 0
	}
	return *u.ActiveBranchID
}

func (u User) EmailVerified() bool {
	return u.EmailVerifiedAt != nil
}

func (u User) PhoneVerified() bool {
	return u.PhoneVerifiedAt != nil
}

func (u User) toRecord() *authn.UserRecord {
	return &authn.UserRecord{
		ID:             u.ID,
		Name:           u.Name,
		Email:          u.Email,
		Password:       u.Password,
		IsActive:       u.IsActive,
		EmailVerified:  u.EmailVerified(),
		PhoneVerified:  u.PhoneVerified(),
		TenantID:       u.TenantIDValue(),
		BranchID:       u.BranchIDValue(),
		ActiveBranchID: u.ActiveBranchIDValue(),
	}
}

func timePtr(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}
	t := ts.Time
	return &t
}

func timestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// optionalTimestamptz maps the zero time to SQL NULL.
func optionalTimestamptz(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return timestamptz(t)
}
