package oauth

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/mcp"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/activity"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database"
)

// Client types a registered client can have.
const (
	TypePublic       = "public"
	TypeConfidential = "confidential"
)

// ClientInput is a registered client's editable fields.
type ClientInput struct {
	Name         string
	Type         string // create only
	RedirectURIs []string
	Scopes       []string
	TenantID     int64
	IsActive     bool
}

// RegisteredClient is a registered client with its secret, which is only
// ever present right after it was created or rotated.
type RegisteredClient struct {
	dbgen.OauthClient
	Secret string
}

func (in *ClientInput) normalise(create bool) error {
	in.Name = strings.TrimSpace(in.Name)
	fields := map[string][]string{}
	if in.Name == "" {
		fields["name"] = append(fields["name"], "The name is required.")
	}
	if create && in.Type != TypePublic && in.Type != TypeConfidential {
		fields["type"] = append(fields["type"], "The type must be public or confidential.")
	}
	if len(in.RedirectURIs) == 0 {
		fields["redirect_uris"] = append(fields["redirect_uris"], "At least one redirect URI is required.")
	}
	for i, uri := range in.RedirectURIs {
		in.RedirectURIs[i] = strings.TrimSpace(uri)
		if err := validRedirect(in.RedirectURIs[i]); err != nil {
			fields["redirect_uris"] = append(fields["redirect_uris"], in.RedirectURIs[i]+": "+err.Error()+".")
		}
	}
	if len(in.Scopes) == 0 {
		in.Scopes = []string{mcp.ScopeRead, mcp.ScopeWrite}
	}
	for _, scope := range in.Scopes {
		if scope != mcp.ScopeRead && scope != mcp.ScopeWrite {
			fields["scopes"] = append(fields["scopes"], "Scopes must be mcp:read or mcp:write.")
			break
		}
	}
	if !slices.Contains(in.Scopes, mcp.ScopeRead) {
		in.Scopes = append([]string{mcp.ScopeRead}, in.Scopes...)
	}
	in.Scopes = slices.Compact(slices.Sorted(slices.Values(in.Scopes)))
	if len(fields) > 0 {
		return apperr.Validation(fields)
	}
	return nil
}

// CreateClient registers a client. A confidential client's secret is in the
// result, once.
func (s *Service) CreateClient(ctx context.Context, actorID int64, in ClientInput) (*RegisteredClient, error) {
	if err := in.normalise(true); err != nil {
		return nil, err
	}
	id, err := randomToken()
	if err != nil {
		return nil, apperr.Wrap(apperr.InternalServerError, "generate client id", err)
	}
	clientID := RegisteredPrefix + id[:32]

	var secret string
	var secretHash *string
	if in.Type == TypeConfidential {
		if secret, err = randomToken(); err != nil {
			return nil, apperr.Wrap(apperr.InternalServerError, "generate client secret", err)
		}
		secretHash = ptr(hashSecret(secret))
	}
	row, err := s.queries.CreateOAuthClient(ctx, dbgen.CreateOAuthClientParams{
		ClientID:     clientID,
		Name:         in.Name,
		Type:         in.Type,
		SecretHash:   secretHash,
		RedirectUris: in.RedirectURIs,
		Scopes:       in.Scopes,
		TenantID:     optional(in.TenantID),
		IsActive:     true,
		CreatedBy:    optional(actorID),
	})
	if err != nil {
		return nil, database.MapError("oauth.create_client", err)
	}
	s.recordClient(ctx, actorID, ActionClientCreated, row)
	return &RegisteredClient{OauthClient: row, Secret: secret}, nil
}

// ListClients pages through registered clients.
func (s *Service) ListClients(ctx context.Context, search string, limit, offset int32) ([]dbgen.OauthClient, int64, error) {
	var filter *string
	if search = strings.TrimSpace(search); search != "" {
		filter = &search
	}
	rows, err := s.queries.ListOAuthClients(ctx, dbgen.ListOAuthClientsParams{Search: filter, RowLimit: limit, RowOffset: offset})
	if err != nil {
		return nil, 0, database.MapError("oauth.list_clients", err)
	}
	total, err := s.queries.CountOAuthClients(ctx, filter)
	if err != nil {
		return nil, 0, database.MapError("oauth.count_clients", err)
	}
	return rows, total, nil
}

// GetClient returns one registered client.
func (s *Service) GetClient(ctx context.Context, id int64) (*dbgen.OauthClient, error) {
	row, err := s.queries.GetOAuthClient(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.New(apperr.ResourceNotFound, "The OAuth client was not found.")
	}
	if err != nil {
		return nil, database.MapError("oauth.get_client", err)
	}
	return &row, nil
}

// UpdateClient changes a registered client. Deactivating it revokes every
// connection made with it.
func (s *Service) UpdateClient(ctx context.Context, actorID, id int64, in ClientInput) (*dbgen.OauthClient, error) {
	current, err := s.GetClient(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := in.normalise(false); err != nil {
		return nil, err
	}
	row, err := s.queries.UpdateOAuthClient(ctx, dbgen.UpdateOAuthClientParams{
		ID:           id,
		Name:         in.Name,
		RedirectUris: in.RedirectURIs,
		Scopes:       in.Scopes,
		TenantID:     optional(in.TenantID),
		IsActive:     in.IsActive,
	})
	if err != nil {
		return nil, database.MapError("oauth.update_client", err)
	}
	if current.IsActive && !row.IsActive {
		s.revokeClientGrants(ctx, row.ClientID)
	}
	s.recordClient(ctx, actorID, ActionClientUpdated, row)
	return &row, nil
}

// RotateSecret gives a confidential client a new secret; the old one keeps
// working for the grace period.
func (s *Service) RotateSecret(ctx context.Context, actorID, id int64) (*RegisteredClient, error) {
	current, err := s.GetClient(ctx, id)
	if err != nil {
		return nil, err
	}
	if current.Type != TypeConfidential {
		return nil, apperr.New(apperr.Conflict, "A public client has no secret to rotate.")
	}
	secret, err := randomToken()
	if err != nil {
		return nil, apperr.Wrap(apperr.InternalServerError, "generate client secret", err)
	}
	row, err := s.queries.RotateOAuthClientSecret(ctx, dbgen.RotateOAuthClientSecretParams{
		ID:                id,
		SecretHash:        ptr(hashSecret(secret)),
		PreviousExpiresAt: timestamptz(s.now().Add(s.settings.SecretGrace)),
	})
	if err != nil {
		return nil, database.MapError("oauth.rotate_secret", err)
	}
	s.recordClient(ctx, actorID, ActionClientSecretRotated, row)
	return &RegisteredClient{OauthClient: row, Secret: secret}, nil
}

// DeleteClient removes a registered client and revokes its connections.
func (s *Service) DeleteClient(ctx context.Context, actorID, id int64) error {
	current, err := s.GetClient(ctx, id)
	if err != nil {
		return err
	}
	s.revokeClientGrants(ctx, current.ClientID)
	if _, err := s.queries.DeleteOAuthClient(ctx, id); err != nil {
		return database.MapError("oauth.delete_client", err)
	}
	s.recordClient(ctx, actorID, ActionClientDeleted, *current)
	return nil
}

func (s *Service) revokeClientGrants(ctx context.Context, clientID string) {
	ids, err := s.queries.RevokeClientOAuthGrants(ctx, clientID)
	if err == nil {
		s.dropGrantTokens(ctx, ids)
	}
}

// recordClient writes a system audit entry; secrets never go in it.
func (s *Service) recordClient(ctx context.Context, actorID int64, action string, row dbgen.OauthClient) {
	changes := map[string]any{
		"client_id": row.ClientID, "name": row.Name, "type": row.Type,
		"redirect_uris": row.RedirectUris, "scopes": row.Scopes, "is_active": row.IsActive,
	}
	if row.TenantID != nil {
		changes["tenant_id"] = *row.TenantID
	}
	s.activity.Record(ctx, activity.Entry{
		ActorID: actorID, Action: action, Subject: "oauth_client", SubjectID: row.ID, Changes: changes,
	})
}
