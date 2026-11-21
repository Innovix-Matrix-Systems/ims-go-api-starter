// Package oauth is the OAuth 2.1 authorization server behind the MCP
// connector (phase 22): a client (Claude, ChatGPT, Cursor…) sends the user to
// /oauth/authorize, the user signs in and consents, and the client gets an
// MCP access token for /mcp.
//
// Clients identify themselves with a Client ID Metadata Document (no
// registration) or are registered by an admin. One grant type
// (authorization code with PKCE S256), refresh tokens rotated on every use.
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/mcp"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/activity"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database"
)

// Activity actions written by this module.
const (
	ActionAuthorized          = "oauth.authorized"
	ActionRevoked             = "oauth.revoked"
	ActionClientCreated       = "oauth_client.created"
	ActionClientUpdated       = "oauth_client.updated"
	ActionClientSecretRotated = "oauth_client.secret_rotated"
	ActionClientDeleted       = "oauth_client.deleted"
)

const codeTTL = time.Minute

// Settings is what the service needs from the configuration.
type Settings struct {
	Issuer          string // APP_URL
	Resource        string // APP_URL + /mcp
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	SecretGrace     time.Duration
}

// Service is the authorization server's logic.
type Service struct {
	queries  *dbgen.Queries
	tx       *database.TxManager
	clients  *Clients
	auth     authn.Authenticator
	users    authn.UserLookup
	activity *activity.Recorder
	settings Settings
	now      func() time.Time
}

// NewService creates the service.
func NewService(queries *dbgen.Queries, tx *database.TxManager, clients *Clients, auth authn.Authenticator,
	users authn.UserLookup, recorder *activity.Recorder, settings Settings) *Service {
	return &Service{queries: queries, tx: tx, clients: clients, auth: auth, users: users,
		activity: recorder, settings: settings, now: time.Now}
}

// Clients is the client resolver.
func (s *Service) Clients() *Clients { return s.clients }

// Settings returns the service's settings.
func (s *Service) Settings() Settings { return s.settings }

// ── Authorization ──────────────────────────────────────────────────

// Approval is what the user allowed on the consent page.
type Approval struct {
	UserID        int64
	UserTenantID  int64
	Client        *Client
	RedirectURI   string
	Scopes        []string
	TenantID      int64 // the tenant a platform account picked (0 = none)
	BranchID      int64
	CodeChallenge string
	Resource      string
}

// Approve stores the grant and returns a one-time authorization code.
func (s *Service) Approve(ctx context.Context, a Approval) (string, error) {
	code, err := randomToken()
	if err != nil {
		return "", apperr.Wrap(apperr.InternalServerError, "generate authorization code", err)
	}
	var grant dbgen.OauthGrant
	err = s.tx.WithTx(ctx, func(q *dbgen.Queries) error {
		var err error
		grant, err = q.CreateOAuthGrant(ctx, dbgen.CreateOAuthGrantParams{
			UserID:      a.UserID,
			ClientID:    a.Client.ID,
			ClientName:  a.Client.Name,
			RedirectUri: a.RedirectURI,
			Scopes:      a.Scopes,
			TenantID:    optional(a.TenantID),
			BranchID:    optional(a.BranchID),
		})
		if err != nil {
			return err
		}
		return q.CreateOAuthCode(ctx, dbgen.CreateOAuthCodeParams{
			CodeHash:      hashSecret(code),
			GrantID:       grant.ID,
			RedirectUri:   a.RedirectURI,
			CodeChallenge: a.CodeChallenge,
			Resource:      a.Resource,
			ExpiresAt:     timestamptz(s.now().Add(codeTTL)),
		})
	})
	if err != nil {
		return "", database.MapError("oauth.approve", err)
	}

	// A tenant user's connection is in their tenant's log; a platform
	// account's is in the system log, whatever tenant it picked.
	s.activity.Record(ctx, activity.Entry{
		TenantID: a.UserTenantID, ActorID: a.UserID, Action: ActionAuthorized,
		Subject: "oauth_grant", SubjectID: grant.ID,
		Changes: map[string]any{
			"client_id": a.Client.ID, "client_name": a.Client.Name,
			"redirect_uri": a.RedirectURI, "scopes": a.Scopes,
			"tenant_id": a.TenantID, "registered": a.Client.Registered,
		},
	})
	return code, nil
}

// ── Token endpoint ─────────────────────────────────────────────────

// TokenRequest is a /oauth/token request with the client already
// authenticated.
type TokenRequest struct {
	Client       *Client
	GrantType    string
	Code         string
	RedirectURI  string
	CodeVerifier string
	RefreshToken string
	Resource     string
}

// TokenResponse is the RFC 6749 §5.1 success body.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// Error is an RFC 6749 §5.2 error.
type Error struct {
	Code        string `json:"error"`
	Description string `json:"error_description,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Description }

func oauthErr(code, description string) *Error { return &Error{Code: code, Description: description} }

// Exchange redeems an authorization code. A code presented a second time
// revokes the grant it made (OAuth 2.1 §4.1.3).
func (s *Service) Exchange(ctx context.Context, req TokenRequest) (*TokenResponse, error) {
	if req.Code == "" || req.CodeVerifier == "" {
		return nil, oauthErr("invalid_request", "code and code_verifier are required.")
	}
	row, err := s.queries.UseOAuthCode(ctx, hashSecret(req.Code))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oauthErr("invalid_grant", "The authorization code is not valid.")
	}
	if err != nil {
		return nil, database.MapError("oauth.use_code", err)
	}
	if row.FirstUsedAt.Valid {
		s.revokeGrant(ctx, row.GrantID, "code_reused")
		return nil, oauthErr("invalid_grant", "The authorization code was already used.")
	}
	if s.now().After(row.ExpiresAt.Time) {
		return nil, oauthErr("invalid_grant", "The authorization code has expired.")
	}
	grant, err := s.queries.GetOAuthGrant(ctx, row.GrantID)
	if err != nil {
		return nil, database.MapError("oauth.get_grant", err)
	}
	switch {
	case grant.RevokedAt.Valid:
		return nil, oauthErr("invalid_grant", "The authorization was revoked.")
	case grant.ClientID != req.Client.ID:
		return nil, oauthErr("invalid_grant", "The code was issued to another client.")
	case req.RedirectURI != row.RedirectUri:
		return nil, oauthErr("invalid_grant", "redirect_uri does not match the authorization request.")
	case !pkceMatches(req.CodeVerifier, row.CodeChallenge):
		return nil, oauthErr("invalid_grant", "code_verifier does not match the code_challenge.")
	case req.Resource != "" && canonical(req.Resource) != row.Resource:
		return nil, oauthErr("invalid_target", "resource does not match the authorization request.")
	}
	return s.issue(ctx, grant)
}

// Refresh rotates a refresh token. A rotated token presented again is reuse:
// the grant is revoked, so a stolen copy stops working for both parties.
func (s *Service) Refresh(ctx context.Context, req TokenRequest) (*TokenResponse, error) {
	if req.RefreshToken == "" {
		return nil, oauthErr("invalid_request", "refresh_token is required.")
	}
	if req.Resource != "" && canonical(req.Resource) != s.settings.Resource {
		return nil, oauthErr("invalid_target", "This server issues tokens only for "+s.settings.Resource+".")
	}
	row, err := s.queries.RotateOAuthRefreshToken(ctx, hashSecret(req.RefreshToken))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oauthErr("invalid_grant", "The refresh token is not valid.")
	}
	if err != nil {
		return nil, database.MapError("oauth.rotate_refresh", err)
	}
	if row.FirstRotatedAt.Valid {
		s.revokeGrant(ctx, row.GrantID, "refresh_token_reused")
		return nil, oauthErr("invalid_grant", "The refresh token was already used.")
	}
	if s.now().After(row.ExpiresAt.Time) {
		return nil, oauthErr("invalid_grant", "The refresh token has expired.")
	}
	grant, err := s.queries.GetOAuthGrant(ctx, row.GrantID)
	if err != nil {
		return nil, database.MapError("oauth.get_grant", err)
	}
	if grant.RevokedAt.Valid {
		return nil, oauthErr("invalid_grant", "The authorization was revoked.")
	}
	if grant.ClientID != req.Client.ID {
		return nil, oauthErr("invalid_grant", "The refresh token was issued to another client.")
	}
	return s.issue(ctx, grant)
}

// issue mints an access token (a personal access token tied to the grant)
// and a refresh token.
func (s *Service) issue(ctx context.Context, grant dbgen.OauthGrant) (*TokenResponse, error) {
	user, err := s.users.FindByID(ctx, grant.UserID)
	if err != nil || !user.IsActive {
		s.revokeGrant(ctx, grant.ID, "user_inactive")
		return nil, oauthErr("invalid_grant", "The account is not active.")
	}

	secret, err := randomToken()
	if err != nil {
		return nil, apperr.Wrap(apperr.InternalServerError, "generate token", err)
	}
	refresh, err := randomToken()
	if err != nil {
		return nil, apperr.Wrap(apperr.InternalServerError, "generate token", err)
	}
	expires := s.now().Add(s.settings.AccessTokenTTL)

	var tokenID int64
	err = s.tx.WithTx(ctx, func(q *dbgen.Queries) error {
		row, err := q.CreateOAuthAccessToken(ctx, dbgen.CreateOAuthAccessTokenParams{
			UserID:    grant.UserID,
			Device:    "mcp:" + strconv.FormatInt(grant.ID, 10),
			Token:     hashSecret(secret),
			Abilities: ptr(authn.EncodeAbilities(grant.Scopes, s.settings.Resource)),
			ExpiresAt: timestamptz(expires),
			GrantID:   &grant.ID,
		})
		if err != nil {
			return err
		}
		tokenID = row.ID
		_, err = q.CreateOAuthRefreshToken(ctx, dbgen.CreateOAuthRefreshTokenParams{
			TokenHash: hashSecret(refresh),
			GrantID:   grant.ID,
			ExpiresAt: timestamptz(s.now().Add(s.settings.RefreshTokenTTL)),
		})
		return err
	})
	if err != nil {
		return nil, database.MapError("oauth.issue", err)
	}
	return &TokenResponse{
		AccessToken:  fmt.Sprintf("%d|%s", tokenID, secret),
		TokenType:    "Bearer",
		ExpiresIn:    int64(s.settings.AccessTokenTTL.Seconds()),
		RefreshToken: refresh,
		Scope:        strings.Join(grant.Scopes, " "),
	}, nil
}

// Revoke is RFC 7009: an access token is deleted; a refresh token revokes
// its whole grant. Unknown tokens are not an error.
func (s *Service) Revoke(ctx context.Context, client *Client, token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	if strings.Contains(token, "|") {
		identity, err := s.auth.Verify(ctx, token)
		if err == nil && identity.OAuthGrantID != 0 {
			if grant, err := s.queries.GetOAuthGrant(ctx, identity.OAuthGrantID); err == nil && grant.ClientID == client.ID {
				_, _ = s.queries.DeleteOAuthGrantTokens(ctx, []int64{grant.ID})
			}
		}
		return
	}
	row, err := s.queries.RotateOAuthRefreshToken(ctx, hashSecret(token))
	if err != nil {
		return
	}
	if grant, err := s.queries.GetOAuthGrant(ctx, row.GrantID); err == nil && grant.ClientID == client.ID {
		s.revokeGrant(ctx, grant.ID, "client_revoked")
	}
}

// ── /mcp token check ───────────────────────────────────────────────

var _ mcp.TokenVerifier = (*Service)(nil)

// VerifyMCP checks an MCP access token: an MCP token (not a session token),
// issued for this server, for an active user, on a grant still in force.
func (s *Service) VerifyMCP(ctx context.Context, raw string) (*mcp.Connection, time.Time, error) {
	identity, err := s.auth.Verify(ctx, raw)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("%w: %v", mcp.ErrInvalidToken, err)
	}
	if !identity.IsMCP() || identity.OAuthGrantID == 0 || identity.Audience != s.settings.Resource {
		return nil, time.Time{}, fmt.Errorf("%w: not an MCP token for this server", mcp.ErrInvalidToken)
	}
	if !identity.IsActive || identity.ExpiresAt.IsZero() {
		return nil, time.Time{}, fmt.Errorf("%w: inactive account or token without expiry", mcp.ErrInvalidToken)
	}
	grant, err := s.queries.GetOAuthGrant(ctx, identity.OAuthGrantID)
	if err != nil || grant.RevokedAt.Valid {
		return nil, time.Time{}, fmt.Errorf("%w: connection revoked", mcp.ErrInvalidToken)
	}
	_ = s.queries.TouchOAuthGrant(ctx, grant.ID)

	conn := &mcp.Connection{Identity: identity, Client: grant.ClientName}
	if grant.TenantID != nil {
		conn.TenantID = *grant.TenantID
	}
	if grant.BranchID != nil {
		conn.BranchID = *grant.BranchID
	}
	return conn, identity.ExpiresAt, nil
}

// ── Connections (the user's grants) ────────────────────────────────

// Connections lists the user's grants in force.
func (s *Service) Connections(ctx context.Context, userID int64) ([]dbgen.OauthGrant, error) {
	grants, err := s.queries.ListUserOAuthGrants(ctx, userID)
	if err != nil {
		return nil, database.MapError("oauth.list_grants", err)
	}
	return grants, nil
}

// Disconnect revokes one of the user's grants.
func (s *Service) Disconnect(ctx context.Context, userID, grantID int64) error {
	grant, err := s.queries.GetOAuthGrant(ctx, grantID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (grant.UserID != userID || grant.RevokedAt.Valid)) {
		return apperr.New(apperr.ResourceNotFound, "The connection was not found.")
	}
	if err != nil {
		return database.MapError("oauth.get_grant", err)
	}
	s.revokeGrant(ctx, grantID, "user")
	return nil
}

func (s *Service) revokeGrant(ctx context.Context, grantID int64, reason string) {
	grant, err := s.queries.RevokeOAuthGrant(ctx, grantID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			_, _ = s.queries.DeleteOAuthGrantTokens(ctx, []int64{grantID})
		}
		return
	}
	s.dropGrantTokens(ctx, []int64{grantID})
	s.activity.Record(ctx, activity.Entry{
		TenantID: grantTenant(ctx, s.users, grant), ActorID: grant.UserID, Action: ActionRevoked,
		Subject: "oauth_grant", SubjectID: grant.ID,
		Changes: map[string]any{"client_id": grant.ClientID, "client_name": grant.ClientName, "reason": reason},
	})
}

func (s *Service) dropGrantTokens(ctx context.Context, ids []int64) {
	if len(ids) == 0 {
		return
	}
	_, _ = s.queries.DeleteOAuthGrantRefreshTokens(ctx, ids)
	_, _ = s.queries.DeleteOAuthGrantTokens(ctx, ids)
}

// grantTenant is where a grant's activity goes: the user's tenant, or the
// system log for a platform account.
func grantTenant(ctx context.Context, users authn.UserLookup, grant dbgen.OauthGrant) int64 {
	if user, err := users.FindByID(ctx, grant.UserID); err == nil {
		return user.TenantID
	}
	return 0
}

// ConsentTenants are the tenants a platform account can pick.
func (s *Service) ConsentTenants(ctx context.Context) ([]dbgen.ListOAuthConsentTenantsRow, error) {
	rows, err := s.queries.ListOAuthConsentTenants(ctx)
	if err != nil {
		return nil, database.MapError("oauth.consent_tenants", err)
	}
	return rows, nil
}

// FirstBranch is the branch a connection works in when a platform account
// picked a tenant.
func (s *Service) FirstBranch(ctx context.Context, tenantID int64) (int64, error) {
	row, err := s.queries.FirstBranchOfTenant(ctx, tenantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, apperr.New(apperr.ResourceNotFound, "The tenant has no branch.")
	}
	if err != nil {
		return 0, database.MapError("oauth.first_branch", err)
	}
	return row.ID, nil
}

// ── helpers ────────────────────────────────────────────────────────

// GrantScopes is what a grant gets: the requested scopes the user allowed,
// capped by a registered client's maximum.
func GrantScopes(client *Client, allowWrite bool) []string {
	scopes := []string{mcp.ScopeRead}
	if allowWrite {
		scopes = append(scopes, mcp.ScopeWrite)
	}
	if client.Registered && len(client.MaxScopes) > 0 {
		scopes = slices.DeleteFunc(scopes, func(s string) bool { return !slices.Contains(client.MaxScopes, s) })
	}
	return scopes
}

func pkceMatches(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:]) == challenge
}

// canonical is a resource URI as compared here: no trailing slash.
func canonical(uri string) string { return strings.TrimRight(strings.TrimSpace(uri), "/") }

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func optional(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}

func ptr[T any](v T) *T { return &v }

func timestamptz(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }
