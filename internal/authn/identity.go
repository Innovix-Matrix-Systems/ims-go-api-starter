// Package authn implements authentication primitives: opaque personal access
// tokens behind the Authenticator interface, OTP flows, login throttling and
// the bearer middleware.
package authn

import "context"

// Identity is the authenticated caller established by the bearer middleware.
type Identity struct {
	UserID         int64
	Name           string
	Email          string
	TenantID       int64
	BranchID       int64
	ActiveBranchID int64
	IsActive       bool
}

// EffectiveBranchID returns the branch the user is currently acting in.
func (i *Identity) EffectiveBranchID() int64 {
	if i.ActiveBranchID != 0 {
		return i.ActiveBranchID
	}
	return i.BranchID
}

// UserRecord is the user data the authentication engine needs.
type UserRecord struct {
	ID             int64
	Name           string
	Email          string
	Password       string
	IsActive       bool
	EmailVerified  bool
	PhoneVerified  bool
	TenantID       int64
	BranchID       int64
	ActiveBranchID int64
}

// UserLookup is the persistence contract the auth engine requires. It is
// implemented by the identity module's repository.
type UserLookup interface {
	FindByEmail(ctx context.Context, email string) (*UserRecord, error)
	FindByID(ctx context.Context, id int64) (*UserRecord, error)
	TouchLogin(ctx context.Context, userID int64, device string) error
}

type contextKey string

const identityKey contextKey = "authn_identity"

// WithIdentity stores the authenticated identity in the context.
func WithIdentity(ctx context.Context, identity *Identity) context.Context {
	return context.WithValue(ctx, identityKey, identity)
}

// IdentityFromContext returns the authenticated identity, if any.
func IdentityFromContext(ctx context.Context) (*Identity, bool) {
	identity, ok := ctx.Value(identityKey).(*Identity)
	return identity, ok
}
