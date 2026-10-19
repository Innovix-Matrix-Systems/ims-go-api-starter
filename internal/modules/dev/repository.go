package dev

import (
	"context"
	"encoding/json"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database"
)

// InsertActionInput carries one audit row.
type InsertActionInput struct {
	ActorUserID  int64
	Action       string
	Target       *string
	Params       json.RawMessage
	Status       string
	ErrorMessage *string
	DurationMS   int32
}

// ActionRepository persists the developer action audit log.
type ActionRepository interface {
	Insert(ctx context.Context, input InsertActionInput) (*Action, error)
	List(ctx context.Context, beforeID int64, limit int32) ([]Action, error)
}

type actionRepository struct {
	queries *dbgen.Queries
}

// NewActionRepository creates the sqlc-backed action audit repository.
func NewActionRepository(queries *dbgen.Queries) ActionRepository {
	return &actionRepository{queries: queries}
}

func (r *actionRepository) Insert(ctx context.Context, input InsertActionInput) (*Action, error) {
	params := input.Params
	if len(params) == 0 {
		params = json.RawMessage("{}")
	}
	row, err := r.queries.InsertDevAction(ctx, dbgen.InsertDevActionParams{
		ActorUserID:  input.ActorUserID,
		Action:       input.Action,
		Target:       input.Target,
		Params:       params,
		Status:       input.Status,
		ErrorMessage: input.ErrorMessage,
		DurationMs:   input.DurationMS,
	})
	if err != nil {
		return nil, database.MapError("dev.insert_action", err)
	}
	action := mapAction(row)
	return &action, nil
}

func (r *actionRepository) List(ctx context.Context, beforeID int64, limit int32) ([]Action, error) {
	rows, err := r.queries.ListDevActions(ctx, dbgen.ListDevActionsParams{
		BeforeID: beforeID,
		RowLimit: limit,
	})
	if err != nil {
		return nil, database.MapError("dev.list_actions", err)
	}
	actions := make([]Action, 0, len(rows))
	for _, row := range rows {
		actions = append(actions, mapAction(row))
	}
	return actions, nil
}
