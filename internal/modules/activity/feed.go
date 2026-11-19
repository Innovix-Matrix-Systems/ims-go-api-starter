package activity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/tenancy"
)

// Item is one activity row.
type Item struct {
	ID          int64
	TenantID    *int64
	ActorID     *int64
	ActorName   *string
	Action      string
	SubjectType string
	SubjectID   *int64
	Changes     json.RawMessage
	IP          *string
	RequestID   *string
	CreatedAt   time.Time
}

// Filter narrows the feed. Cursor is the opaque value from a previous page.
type Filter struct {
	SubjectType string
	SubjectID   int64
	ActorID     int64
	Cursor      string
	PerPage     int
}

// Page is one page of the feed, newest first.
type Page struct {
	Items      []Item
	PerPage    int
	NextCursor *string
}

// Service reads the activity feed.
type Service struct {
	queries *dbgen.Queries
}

// NewService creates the activity feed service.
func NewService(queries *dbgen.Queries) *Service {
	return &Service{queries: queries}
}

// Feed lists activity visible to the scope: one tenant, or every tenant for
// platform accounts.
func (s *Service) Feed(ctx context.Context, scope tenancy.Scope, filter Filter) (*Page, error) {
	perPage := filter.PerPage
	if perPage < 1 || perPage > 100 {
		perPage = 25
	}
	tenant := scope.Filter()
	params := dbgen.ListActivityParams{
		AllTenants:  tenant.All,
		TenantID:    optional(tenant.TenantID),
		SubjectType: optionalString(filter.SubjectType),
		SubjectID:   optional(filter.SubjectID),
		ActorID:     optional(filter.ActorID),
		RowLimit:    int32(perPage + 1),
	}
	if filter.Cursor != "" {
		at, id, err := decodeCursor(filter.Cursor)
		if err != nil {
			return nil, err
		}
		params.BeforeAt = pgtype.Timestamptz{Time: at, Valid: true}
		params.BeforeID = &id
	}
	rows, err := s.queries.ListActivity(ctx, params)
	if err != nil {
		return nil, database.MapError("activity.list", err)
	}
	page := &Page{PerPage: perPage, Items: make([]Item, 0, min(len(rows), perPage))}
	for i, row := range rows {
		if i == perPage {
			last := page.Items[len(page.Items)-1]
			next := encodeCursor(last.CreatedAt, last.ID)
			page.NextCursor = &next
			break
		}
		page.Items = append(page.Items, Item{
			ID: row.ID, TenantID: row.TenantID, ActorID: row.ActorID, ActorName: row.ActorName,
			Action: row.Action, SubjectType: row.SubjectType, SubjectID: row.SubjectID,
			Changes: json.RawMessage(row.Changes), IP: row.Ip, RequestID: row.RequestID,
			CreatedAt: row.CreatedAt.Time,
		})
	}
	return page, nil
}

// Purge deletes activity older than the retention window.
func (s *Service) Purge(ctx context.Context, days int) (int64, error) {
	return s.queries.PurgeActivity(ctx, int32(days))
}

func encodeCursor(at time.Time, id int64) string {
	raw := strconv.FormatInt(at.UnixNano(), 10) + ":" + strconv.FormatInt(id, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(raw string) (time.Time, int64, error) {
	invalid := apperr.Validation(map[string][]string{"cursor": {"The cursor is invalid."}})
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return time.Time{}, 0, invalid
	}
	nanos, id, ok := strings.Cut(string(decoded), ":")
	if !ok {
		return time.Time{}, 0, invalid
	}
	at, err1 := strconv.ParseInt(nanos, 10, 64)
	rowID, err2 := strconv.ParseInt(id, 10, 64)
	if err1 != nil || err2 != nil || rowID < 1 {
		return time.Time{}, 0, invalid
	}
	return time.Unix(0, at), rowID, nil
}
