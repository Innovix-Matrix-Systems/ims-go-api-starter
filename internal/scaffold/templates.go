package scaffold

const migrationTmpl = `-- +goose Up
CREATE TABLE {{.Table}} (
    id         bigserial PRIMARY KEY,
    tenant_id  bigint NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
{{- range .Fields}}
    {{.Column}} {{.SQLType}}{{if not .Optional}} NOT NULL{{end}},
{{- end}}
    created_by bigint,
    updated_by bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);

CREATE INDEX {{.Table}}_tenant_id_idx ON {{.Table}} (tenant_id);

-- +goose Down
DROP TABLE {{.Table}};
`

const queriesTmpl = `-- name: Get{{.Name}} :one
SELECT * FROM {{.Table}}
WHERE id = @id AND tenant_id = @tenant_id AND deleted_at IS NULL;

-- name: List{{.Plural}} :many
SELECT * FROM {{.Table}}
WHERE tenant_id = @tenant_id
  AND deleted_at IS NULL{{if .Search}}
  AND (sqlc.narg('search')::text IS NULL OR {{.Search}} ILIKE '%' || sqlc.narg('search')::text || '%'){{end}}
ORDER BY id DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: Count{{.Plural}} :one
SELECT count(*) FROM {{.Table}}
WHERE tenant_id = @tenant_id
  AND deleted_at IS NULL{{if .Search}}
  AND (sqlc.narg('search')::text IS NULL OR {{.Search}} ILIKE '%' || sqlc.narg('search')::text || '%'){{end}};

-- name: Create{{.Name}} :one
INSERT INTO {{.Table}} (
    tenant_id,
{{- range .Fields}}
    {{.Column}},
{{- end}}
    created_by, updated_by
) VALUES (
    @tenant_id,
{{- range .Fields}}
    @{{.Column}},
{{- end}}
    @created_by, @updated_by
)
RETURNING *;

-- name: Update{{.Name}} :one
UPDATE {{.Table}}
SET
{{- range .Fields}}
    {{.Column}} = @{{.Column}},
{{- end}}
    updated_by = @updated_by,
    updated_at = now()
WHERE id = @id AND tenant_id = @tenant_id AND deleted_at IS NULL
RETURNING *;

-- name: SoftDelete{{.Name}} :execrows
UPDATE {{.Table}}
SET deleted_at = now(), updated_by = @updated_by
WHERE id = @id AND tenant_id = @tenant_id AND deleted_at IS NULL;
`

const modelTmpl = `package {{.Pkg}}

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	dbgen "{{.ModulePath}}/internal/dbgen"
)

// {{.Name}} is the domain representation of a {{.NameLower}}.
type {{.Name}} struct {
	ID        int64
	TenantID  int64
{{- range .Fields}}
	{{.GoName}} {{.DomainType}}
{{- end}}
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

func map{{.Name}}(row dbgen.{{.Name}}) {{.Name}} {
	return {{.Name}}{
		ID:       row.ID,
		TenantID: row.TenantID,
{{- range .Fields}}
		{{.GoName}}: {{.MapFromRow}},
{{- end}}
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
		DeletedAt: timePtr(row.DeletedAt),
	}
}

func timePtr(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}
	t := ts.Time
	return &t
}

func timestamptzPtr(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *value, Valid: true}
}
`

const repositoryTmpl = `package {{.Pkg}}

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"{{.ModulePath}}/internal/apperr"
	dbgen "{{.ModulePath}}/internal/dbgen"
	"{{.ModulePath}}/internal/platform/database"
)

// Repository is the persistence contract for {{.NameLower}}s.
type Repository interface {
	Get(ctx context.Context, tenantID, id int64) (*{{.Name}}, error)
	List(ctx context.Context, filter ListFilter) ([]{{.Name}}, error)
	Count(ctx context.Context, filter ListFilter) (int64, error)
	Create(ctx context.Context, input CreateInput) (*{{.Name}}, error)
	Update(ctx context.Context, input UpdateInput) (*{{.Name}}, error)
	Delete(ctx context.Context, tenantID, id int64, actorID *int64) error
}

// ListFilter carries {{.NameLower}} list filters.
type ListFilter struct {
	TenantID int64
	Search   string
	Limit    int32
	Offset   int32
}

// CreateInput carries {{.NameLower}} creation input.
type CreateInput struct {
	TenantID int64
{{- range .Fields}}
	{{.GoName}} {{.DomainType}}
{{- end}}
	ActorID *int64
}

// UpdateInput carries {{.NameLower}} update input.
type UpdateInput struct {
	ID       int64
	TenantID int64
{{- range .Fields}}
	{{.GoName}} {{.DomainType}}
{{- end}}
	ActorID *int64
}

type repository struct {
	queries *dbgen.Queries
}

// NewRepository creates the sqlc-backed repository.
func NewRepository(queries *dbgen.Queries) Repository {
	return &repository{queries: queries}
}

func (r *repository) Get(ctx context.Context, tenantID, id int64) (*{{.Name}}, error) {
	row, err := r.queries.Get{{.Name}}(ctx, dbgen.Get{{.Name}}Params{ID: id, TenantID: tenantID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "{{.Name}} with ID '%d' was not found.", id)
		}
		return nil, database.MapError("{{.NameLower}}.get", err)
	}
	item := map{{.Name}}(row)
	return &item, nil
}

func (r *repository) List(ctx context.Context, filter ListFilter) ([]{{.Name}}, error) {
	rows, err := r.queries.List{{.Plural}}(ctx, dbgen.List{{.Plural}}Params{
		TenantID:   filter.TenantID,
{{- if .Search}}
		Search:     optionalString(filter.Search),
{{- end}}
		PageLimit:  filter.Limit,
		PageOffset: filter.Offset,
	})
	if err != nil {
		return nil, database.MapError("{{.NameLower}}.list", err)
	}
	items := make([]{{.Name}}, 0, len(rows))
	for _, row := range rows {
		items = append(items, map{{.Name}}(row))
	}
	return items, nil
}

func (r *repository) Count(ctx context.Context, filter ListFilter) (int64, error) {
	total, err := r.queries.Count{{.Plural}}(ctx, dbgen.Count{{.Plural}}Params{
		TenantID: filter.TenantID,
{{- if .Search}}
		Search:   optionalString(filter.Search),
{{- end}}
	})
	if err != nil {
		return 0, database.MapError("{{.NameLower}}.count", err)
	}
	return total, nil
}

func (r *repository) Create(ctx context.Context, input CreateInput) (*{{.Name}}, error) {
	row, err := r.queries.Create{{.Name}}(ctx, dbgen.Create{{.Name}}Params{
		TenantID: input.TenantID,
{{- range .Fields}}
		{{.GoName}}: {{.FromInput}},
{{- end}}
		CreatedBy: input.ActorID,
		UpdatedBy: input.ActorID,
	})
	if err != nil {
		return nil, database.MapError("{{.NameLower}}.create", err)
	}
	item := map{{.Name}}(row)
	return &item, nil
}

func (r *repository) Update(ctx context.Context, input UpdateInput) (*{{.Name}}, error) {
	row, err := r.queries.Update{{.Name}}(ctx, dbgen.Update{{.Name}}Params{
		ID:       input.ID,
		TenantID: input.TenantID,
{{- range .Fields}}
		{{.GoName}}: {{.FromInput}},
{{- end}}
		UpdatedBy: input.ActorID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "{{.Name}} with ID '%d' was not found.", input.ID)
		}
		return nil, database.MapError("{{.NameLower}}.update", err)
	}
	item := map{{.Name}}(row)
	return &item, nil
}

func (r *repository) Delete(ctx context.Context, tenantID, id int64, actorID *int64) error {
	affected, err := r.queries.SoftDelete{{.Name}}(ctx, dbgen.SoftDelete{{.Name}}Params{
		ID:        id,
		TenantID:  tenantID,
		UpdatedBy: actorID,
	})
	if err != nil {
		return database.MapError("{{.NameLower}}.delete", err)
	}
	if affected == 0 {
		return apperr.Newf(apperr.ResourceNotFound, "{{.Name}} with ID '%d' was not found.", id)
	}
	return nil
}
{{if .Search}}
func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
{{end}}`

const serviceTmpl = `package {{.Pkg}}

import (
	"context"

	"{{.ModulePath}}/internal/apperr"
	"{{.ModulePath}}/internal/authn"
	"{{.ModulePath}}/internal/tenancy"
)

// Permissions required by this module. Add them to internal/authz/catalog.go
// and run "ims permission:sync" before exposing the endpoints.
const (
	PermView   = "{{.NameLower}}.view"
	PermCreate = "{{.NameLower}}.create"
	PermUpdate = "{{.NameLower}}.update"
	PermDelete = "{{.NameLower}}.delete"
)

// PermissionChecker is the authorization surface the service requires.
type PermissionChecker interface {
	MustCan(ctx context.Context, userID int64, permission string) error
}

// Service holds the {{.NameLower}} business rules.
type Service struct {
	repo    Repository
	checker PermissionChecker
}

// NewService creates the {{.NameLower}} service.
func NewService(repo Repository, checker PermissionChecker) *Service {
	return &Service{repo: repo, checker: checker}
}

// List returns {{.NameLower}}s within the effective tenant scope.
func (s *Service) List(ctx context.Context, scope tenancy.Scope, filter ListFilter) ([]{{.Name}}, int64, error) {
	if _, err := s.authorize(ctx, PermView); err != nil {
		return nil, 0, err
	}
	filter.TenantID = scope.EffectiveTenantID()
	if filter.TenantID == 0 {
		return nil, 0, apperr.New(apperr.TenantContextNotResolved, "A tenant context is required.")
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

// Get returns one {{.NameLower}} visible in the effective tenant scope.
func (s *Service) Get(ctx context.Context, scope tenancy.Scope, id int64) (*{{.Name}}, error) {
	if _, err := s.authorize(ctx, PermView); err != nil {
		return nil, err
	}
	tenantID := scope.EffectiveTenantID()
	if tenantID == 0 {
		return nil, apperr.New(apperr.TenantContextNotResolved, "A tenant context is required.")
	}
	return s.repo.Get(ctx, tenantID, id)
}

// Create creates a {{.NameLower}} in the effective tenant scope.
func (s *Service) Create(ctx context.Context, scope tenancy.Scope, input CreateInput) (*{{.Name}}, error) {
	identity, err := s.authorize(ctx, PermCreate)
	if err != nil {
		return nil, err
	}
	input.TenantID = scope.EffectiveTenantID()
	if input.TenantID == 0 {
		return nil, apperr.New(apperr.TenantContextNotResolved, "A tenant context is required.")
	}
	actor := identity.UserID
	input.ActorID = &actor
	return s.repo.Create(ctx, input)
}

// Update updates a {{.NameLower}} in the effective tenant scope.
func (s *Service) Update(ctx context.Context, scope tenancy.Scope, id int64, input UpdateInput) (*{{.Name}}, error) {
	identity, err := s.authorize(ctx, PermUpdate)
	if err != nil {
		return nil, err
	}
	input.ID = id
	input.TenantID = scope.EffectiveTenantID()
	if input.TenantID == 0 {
		return nil, apperr.New(apperr.TenantContextNotResolved, "A tenant context is required.")
	}
	actor := identity.UserID
	input.ActorID = &actor
	return s.repo.Update(ctx, input)
}

// Delete soft-deletes a {{.NameLower}} in the effective tenant scope.
func (s *Service) Delete(ctx context.Context, scope tenancy.Scope, id int64) error {
	identity, err := s.authorize(ctx, PermDelete)
	if err != nil {
		return err
	}
	tenantID := scope.EffectiveTenantID()
	if tenantID == 0 {
		return apperr.New(apperr.TenantContextNotResolved, "A tenant context is required.")
	}
	actor := identity.UserID
	return s.repo.Delete(ctx, tenantID, id, &actor)
}

func (s *Service) authorize(ctx context.Context, permission string) (*authn.Identity, error) {
	identity, ok := authn.IdentityFromContext(ctx)
	if !ok {
		return nil, apperr.New(apperr.Unauthorized, "Authentication is required.")
	}
	if err := s.checker.MustCan(ctx, identity.UserID, permission); err != nil {
		return nil, err
	}
	return identity, nil
}
`

const requestTmpl = `package {{.Pkg}}

import (
{{- if .HasJSON}}
	"encoding/json"
{{- end}}
	"time"
)

// InsertUpdateRequest is the {{.NameLower}} create/update payload.
type InsertUpdateRequest struct {
{{- range .Fields}}
	{{.GoName}} {{.RequestType}} ` + "`json:\"{{.Column}}\" validate:\"{{.ValidateTag}}\"`" + `
{{- end}}
}

// Response is the public {{.NameLower}} representation.
type Response struct {
	ID       int64 ` + "`json:\"id\"`" + `
	TenantID int64 ` + "`json:\"tenant_id\"`" + `
{{- range .Fields}}
	{{.GoName}} {{.DomainType}} ` + "`json:\"{{.Column}}\"`" + `
{{- end}}
	CreatedAt time.Time ` + "`json:\"created_at\"`" + `
	UpdatedAt time.Time ` + "`json:\"updated_at\"`" + `
}

// NewResponse maps a {{.NameLower}} to its public representation.
func NewResponse(item {{.Name}}) Response {
	return Response{
		ID:       item.ID,
		TenantID: item.TenantID,
{{- range .Fields}}
		{{.GoName}}: item.{{.GoName}},
{{- end}}
		CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt,
	}
}

func (req InsertUpdateRequest) toCreateInput() CreateInput {
	return CreateInput{
{{- range .Fields}}
		{{.GoName}}: {{.ToInput}},
{{- end}}
	}
}

func (req InsertUpdateRequest) toUpdateInput(id int64) UpdateInput {
	return UpdateInput{
		ID: id,
{{- range .Fields}}
		{{.GoName}}: {{.ToInput}},
{{- end}}
	}
}
`

const handlerTmpl = `package {{.Pkg}}

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"{{.ModulePath}}/internal/apperr"
	"{{.ModulePath}}/internal/platform/httpx"
	"{{.ModulePath}}/internal/tenancy"
)

// Handler serves {{.NameLower}} endpoints.
type Handler struct {
	service   *Service
	binder    *httpx.Binder
	validator *httpx.Validator
	responder *httpx.Responder
}

// NewHandler creates the {{.NameLower}} handler.
func NewHandler(service *Service, binder *httpx.Binder, validator *httpx.Validator, responder *httpx.Responder) *Handler {
	return &Handler{service: service, binder: binder, validator: validator, responder: responder}
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	scope, _ := tenancy.FromContext(r.Context())
	page, perPage, offset := httpx.PageParams(r)
	items, total, err := h.service.List(r.Context(), scope, ListFilter{
		Search: r.URL.Query().Get("search"),
		Limit:  int32(perPage),
		Offset: int32(offset),
	})
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	data := make([]Response, 0, len(items))
	for _, item := range items {
		data = append(data, NewResponse(item))
	}
	meta, links := httpx.NewPagination(r, page, perPage, total)
	h.responder.Paginated(w, r, http.StatusOK, data, meta, links)
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	scope, _ := tenancy.FromContext(r.Context())
	id, err := pathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	item, err := h.service.Get(r.Context(), scope, id)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, NewResponse(*item))
}

func (h *Handler) store(w http.ResponseWriter, r *http.Request) {
	scope, _ := tenancy.FromContext(r.Context())
	var req InsertUpdateRequest
	if err := h.binder.JSON(w, r, &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	item, err := h.service.Create(r.Context(), scope, req.toCreateInput())
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusCreated, NewResponse(*item))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	scope, _ := tenancy.FromContext(r.Context())
	id, err := pathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	var req InsertUpdateRequest
	if err := h.binder.JSON(w, r, &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.validator.Struct(r.Context(), &req); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	item, err := h.service.Update(r.Context(), scope, id, req.toUpdateInput(id))
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.Data(w, r, http.StatusOK, NewResponse(*item))
}

func (h *Handler) destroy(w http.ResponseWriter, r *http.Request) {
	scope, _ := tenancy.FromContext(r.Context())
	id, err := pathID(r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}
	if err := h.service.Delete(r.Context(), scope, id); err != nil {
		h.responder.Error(w, r, err)
		return
	}
	h.responder.NoContent(w)
}

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 1 {
		return 0, apperr.New(apperr.InvalidInput, "The provided identifier is invalid.")
	}
	return id, nil
}
`

const routesTmpl = `package {{.Pkg}}

import "github.com/go-chi/chi/v5"

// RegisterRoutes mounts the {{.NameLower}} routes on the API router.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/{{.NameLower}}", h.index)
	r.Get("/{{.NameLower}}/{id}", h.show)
	r.Post("/{{.NameLower}}", h.store)
	r.Patch("/{{.NameLower}}/{id}", h.update)
	r.Delete("/{{.NameLower}}/{id}", h.destroy)
}
`

const serviceTestTmpl = `package {{.Pkg}}

import (
	"context"
	"testing"

	"{{.ModulePath}}/internal/authn"
	"{{.ModulePath}}/internal/tenancy"
)

type fakeRepo struct {
	created *CreateInput
}

func (f *fakeRepo) Get(_ context.Context, tenantID, id int64) (*{{.Name}}, error) {
	return &{{.Name}}{ID: id, TenantID: tenantID}, nil
}
func (f *fakeRepo) List(_ context.Context, _ ListFilter) ([]{{.Name}}, error) { return nil, nil }
func (f *fakeRepo) Count(_ context.Context, _ ListFilter) (int64, error)      { return 0, nil }
func (f *fakeRepo) Create(_ context.Context, input CreateInput) (*{{.Name}}, error) {
	f.created = &input
	return &{{.Name}}{ID: 1, TenantID: input.TenantID}, nil
}
func (f *fakeRepo) Update(_ context.Context, input UpdateInput) (*{{.Name}}, error) {
	return &{{.Name}}{ID: input.ID, TenantID: input.TenantID}, nil
}
func (f *fakeRepo) Delete(_ context.Context, _, _ int64, _ *int64) error { return nil }

type allowChecker struct{}

func (allowChecker) MustCan(context.Context, int64, string) error { return nil }

func authedContext(userID int64) context.Context {
	return authn.WithIdentity(context.Background(), &authn.Identity{UserID: userID})
}

func TestServiceCreateScopesToTenant(t *testing.T) {
	repo := &fakeRepo{}
	service := NewService(repo, allowChecker{})

	item, err := service.Create(authedContext(7), tenancy.Scope{TenantID: 3}, CreateInput{})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if item.TenantID != 3 {
		t.Errorf("tenant = %d, want 3", item.TenantID)
	}
	if repo.created == nil || repo.created.TenantID != 3 {
		t.Errorf("repo create input was not tenant-scoped: %+v", repo.created)
	}
}

func TestServiceCreateRequiresTenant(t *testing.T) {
	service := NewService(&fakeRepo{}, allowChecker{})
	if _, err := service.Create(authedContext(7), tenancy.Scope{}, CreateInput{}); err == nil {
		t.Error("expected an error when no tenant context is present")
	}
}
`
