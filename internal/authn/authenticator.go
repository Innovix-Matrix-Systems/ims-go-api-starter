package authn

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/config"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database"
)

// ModelTypeUser is the tokenable/RBAC model type used for user rows.
const ModelTypeUser = "user"

// Token is an issued access token; PlainText is the only time the secret is
// available and has the wire format "id|secret".
type Token struct {
	ID        int64
	PlainText string
	ExpiresAt *time.Time
}

// Authenticator issues, verifies and revokes access tokens. The opaque
// implementation ships today; a JWT driver can replace it without touching
// middleware, handlers or services.
type Authenticator interface {
	Issue(ctx context.Context, userID int64, device string) (Token, error)
	Verify(ctx context.Context, raw string) (*Identity, error)
	Revoke(ctx context.Context, userID int64, device string) error
	RevokeAll(ctx context.Context, userID int64) error
}

// OpaqueAuthenticator stores SHA-256 hashed secrets in personal_access_tokens.
type OpaqueAuthenticator struct {
	queries *dbgen.Queries
	users   UserLookup
	cfg     config.Auth
	now     func() time.Time
}

// NewOpaqueAuthenticator creates the database-backed token engine.
func NewOpaqueAuthenticator(queries *dbgen.Queries, users UserLookup, cfg config.Auth) *OpaqueAuthenticator {
	return &OpaqueAuthenticator{queries: queries, users: users, cfg: cfg, now: time.Now}
}

// Issue creates a per-device token and returns its plaintext once.
func (a *OpaqueAuthenticator) Issue(ctx context.Context, userID int64, device string) (Token, error) {
	secret, err := randomSecret()
	if err != nil {
		return Token{}, apperr.Wrap(apperr.InternalServerError, "generate token secret", err)
	}
	sum := sha256.Sum256([]byte(secret))

	var expiresAt pgtype.Timestamptz
	var expires *time.Time
	if a.cfg.TokenExpiration > 0 {
		t := a.now().Add(a.cfg.TokenExpiration)
		expiresAt = pgtype.Timestamptz{Time: t, Valid: true}
		expires = &t
	}

	row, err := a.queries.CreateToken(ctx, dbgen.CreateTokenParams{
		TokenableType: ModelTypeUser,
		TokenableID:   userID,
		Name:          fmt.Sprintf("user_%d_%s", userID, device),
		Device:        device,
		Token:         hex.EncodeToString(sum[:]),
		ExpiresAt:     expiresAt,
	})
	if err != nil {
		return Token{}, database.MapError("authn.issue_token", err)
	}
	return Token{ID: row.ID, PlainText: fmt.Sprintf("%d|%s", row.ID, secret), ExpiresAt: expires}, nil
}

// Verify resolves a raw token into an Identity.
func (a *OpaqueAuthenticator) Verify(ctx context.Context, raw string) (*Identity, error) {
	parts := strings.SplitN(strings.TrimSpace(raw), "|", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, apperr.New(apperr.TokenInvalid, "The provided access token is invalid.")
	}
	tokenID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return nil, apperr.New(apperr.TokenInvalid, "The provided access token is invalid.")
	}

	row, err := a.queries.GetToken(ctx, tokenID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.New(apperr.TokenInvalid, "The provided access token is invalid.")
		}
		return nil, database.MapError("authn.get_token", err)
	}

	sum := sha256.Sum256([]byte(parts[1]))
	expected, err := hex.DecodeString(row.Token)
	if err != nil || subtle.ConstantTimeCompare(sum[:], expected) != 1 {
		return nil, apperr.New(apperr.TokenInvalid, "The provided access token is invalid.")
	}
	if row.ExpiresAt.Valid && a.now().After(row.ExpiresAt.Time) {
		return nil, apperr.New(apperr.TokenExpired, "The access token has expired.")
	}

	record, err := a.users.FindByID(ctx, row.TokenableID)
	if err != nil {
		if apperr.Is(err, apperr.ResourceNotFound) {
			return nil, apperr.New(apperr.TokenInvalid, "The provided access token is invalid.")
		}
		return nil, err
	}

	a.touchLastUsed(ctx, row)

	return &Identity{
		UserID:         record.ID,
		TokenID:        row.ID,
		Name:           record.Name,
		Email:          record.Email,
		TenantID:       record.TenantID,
		BranchID:       record.BranchID,
		ActiveBranchID: record.ActiveBranchID,
		IsActive:       record.IsActive,
	}, nil
}

// Revoke deletes the token row for one device.
func (a *OpaqueAuthenticator) Revoke(ctx context.Context, userID int64, device string) error {
	if _, err := a.queries.DeleteTokenByDevice(ctx, dbgen.DeleteTokenByDeviceParams{
		TokenableType: ModelTypeUser,
		TokenableID:   userID,
		Device:        device,
	}); err != nil {
		return database.MapError("authn.revoke_token", err)
	}
	return nil
}

// RevokeAll deletes every token for the user (password change, ban).
func (a *OpaqueAuthenticator) RevokeAll(ctx context.Context, userID int64) error {
	if _, err := a.queries.DeleteTokensForUser(ctx, dbgen.DeleteTokensForUserParams{
		TokenableType: ModelTypeUser,
		TokenableID:   userID,
	}); err != nil {
		return database.MapError("authn.revoke_all_tokens", err)
	}
	return nil
}

func (a *OpaqueAuthenticator) touchLastUsed(ctx context.Context, row dbgen.PersonalAccessToken) {
	if row.LastUsedAt.Valid && a.now().Sub(row.LastUsedAt.Time) < time.Minute {
		return
	}
	_, _ = a.queries.TouchToken(ctx, dbgen.TouchTokenParams{
		ID:         row.ID,
		LastUsedAt: pgtype.Timestamptz{Time: a.now(), Valid: true},
	})
}

func randomSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
