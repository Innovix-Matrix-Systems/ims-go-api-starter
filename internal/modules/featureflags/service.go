package featureflags

import (
	"context"
	"fmt"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/cache"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/tenancy"
)

const flagsTTL = 5 * time.Minute

func globalBlobKey() string               { return "flags:blob:g" }
func tenantBlobKey(tenantID int64) string { return fmt.Sprintf("flags:blob:t:%d", tenantID) }
func userBlobKey(userID int64) string     { return fmt.Sprintf("flags:blob:u:%d", userID) }

// blob is one target's key -> bool cache entry.
type blob map[string]bool

// EffectiveFlag is one catalog flag resolved for a target.
type EffectiveFlag struct {
	Flag   Flag
	Value  bool
	Source string // user | tenant | global | default
}

// Service resolves and persists feature flags.
type Service struct {
	repo  Repository
	cache cache.Store
}

// NewService creates the feature-flag service.
func NewService(repo Repository, store cache.Store) *Service {
	return &Service{repo: repo, cache: store}
}

// Effective resolves every catalog flag for the caller:
// user -> tenant -> global -> default.
func (s *Service) Effective(ctx context.Context, scope tenancy.Scope, userID int64) (map[string]bool, error) {
	result := make(map[string]bool, len(Catalog))
	for _, flag := range Catalog {
		result[flag.Key] = flag.Default
	}

	global, err := s.globalBlob(ctx)
	if err != nil {
		return nil, err
	}
	for key, value := range global {
		result[key] = value
	}

	if tenantID := scope.EffectiveTenantID(); tenantID > 0 {
		tenant, err := s.tenantBlob(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		for key, value := range tenant {
			result[key] = value
		}
	}

	if userID > 0 {
		user, err := s.userBlob(ctx, userID)
		if err != nil {
			return nil, err
		}
		for key, value := range user {
			result[key] = value
		}
	}
	return result, nil
}

// IsEnabled resolves a single flag for the caller (used by the gate middleware).
func (s *Service) IsEnabled(ctx context.Context, scope tenancy.Scope, userID int64, key string) (bool, error) {
	flag, ok := Lookup(key)
	if !ok {
		return false, apperr.Newf(apperr.InternalServerError, "Feature flag %q is not registered.", key)
	}
	values, err := s.Effective(ctx, scope, userID)
	if err != nil {
		return false, err
	}
	if value, ok := values[key]; ok {
		return value, nil
	}
	return flag.Default, nil
}

// EffectiveForTarget resolves every catalog flag for an explicit target.
func (s *Service) EffectiveForTarget(ctx context.Context, target Target) ([]EffectiveFlag, error) {
	global, err := s.globalBlob(ctx)
	if err != nil {
		return nil, err
	}
	tenant, err := s.tenantBlobForTarget(ctx, target)
	if err != nil {
		return nil, err
	}
	user, err := s.userBlobForTarget(ctx, target)
	if err != nil {
		return nil, err
	}

	out := make([]EffectiveFlag, 0, len(Catalog))
	for _, flag := range Catalog {
		value, source := flag.Default, "default"
		if v, ok := global[flag.Key]; ok {
			value, source = v, "global"
		}
		if v, ok := tenant[flag.Key]; ok {
			value, source = v, "tenant"
		}
		if v, ok := user[flag.Key]; ok {
			value, source = v, "user"
		}
		out = append(out, EffectiveFlag{Flag: flag, Value: value, Source: source})
	}
	return out, nil
}

// SetBatch applies a batch of flag values to a target. A nil value clears that
// override (inherit from the next level).
func (s *Service) SetBatch(ctx context.Context, actorID int64, target Target, flags map[string]*bool) ([]EffectiveFlag, error) {
	if err := s.validateTarget(ctx, target); err != nil {
		return nil, err
	}
	for key := range flags {
		if _, ok := Lookup(key); !ok {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Feature flag %q is not registered.", key)
		}
	}

	var actor *int64
	if actorID > 0 {
		actor = &actorID
	}
	for key, value := range flags {
		if value == nil {
			if _, err := s.repo.Delete(ctx, key, target); err != nil {
				return nil, err
			}
			continue
		}
		if target.IsGlobal() {
			if _, err := s.repo.UpsertGlobal(ctx, key, *value, actor); err != nil {
				return nil, err
			}
			continue
		}
		if _, err := s.repo.UpsertTarget(ctx, key, target, *value, actor); err != nil {
			return nil, err
		}
	}
	s.invalidate(ctx, target)
	return s.EffectiveForTarget(ctx, target)
}

// Delete clears a single override at the target.
func (s *Service) Delete(ctx context.Context, target Target, key string) error {
	if _, ok := Lookup(key); !ok {
		return apperr.Newf(apperr.ResourceNotFound, "Feature flag %q is not registered.", key)
	}
	if err := s.validateTarget(ctx, target); err != nil {
		return err
	}
	deleted, err := s.repo.Delete(ctx, key, target)
	if err != nil {
		return err
	}
	if !deleted {
		return apperr.Newf(apperr.ResourceNotFound, "No stored value exists for feature flag %q at the requested target.", key)
	}
	s.invalidate(ctx, target)
	return nil
}

// Sync inserts missing global defaults and returns how many were created.
func (s *Service) Sync(ctx context.Context) (int, error) {
	created := 0
	for _, flag := range Catalog {
		_, inserted, err := s.repo.InsertGlobalIfAbsent(ctx, flag.Key, flag.Default)
		if err != nil {
			return created, err
		}
		if inserted {
			created++
		}
	}
	_ = s.cache.Delete(ctx, globalBlobKey())
	return created, nil
}

func (s *Service) validateTarget(ctx context.Context, target Target) error {
	switch target.Type {
	case TargetGlobal:
		if target.ID != 0 {
			return apperr.Validation(map[string][]string{"target_id": {"The target_id must be omitted for the global target."}})
		}
		return nil
	case TargetTenant:
		if target.ID <= 0 {
			return apperr.Validation(map[string][]string{"target_id": {"The target_id is required for the tenant target."}})
		}
		exists, err := s.repo.TenantExists(ctx, target.ID)
		if err != nil {
			return err
		}
		if !exists {
			return apperr.Validation(map[string][]string{"target_id": {"The selected tenant does not exist."}})
		}
		return nil
	case TargetUser:
		if target.ID <= 0 {
			return apperr.Validation(map[string][]string{"target_id": {"The target_id is required for the user target."}})
		}
		_, exists, err := s.repo.UserTenant(ctx, target.ID)
		if err != nil {
			return err
		}
		if !exists {
			return apperr.Validation(map[string][]string{"target_id": {"The selected user does not exist."}})
		}
		return nil
	default:
		return apperr.Validation(map[string][]string{"target_type": {"The selected target_type is not supported."}})
	}
}

func (s *Service) invalidate(ctx context.Context, target Target) {
	switch target.Type {
	case TargetGlobal:
		_ = s.cache.Delete(ctx, globalBlobKey())
	case TargetTenant:
		_ = s.cache.Delete(ctx, tenantBlobKey(target.ID))
	case TargetUser:
		_ = s.cache.Delete(ctx, userBlobKey(target.ID))
	}
}

func (s *Service) tenantBlobForTarget(ctx context.Context, target Target) (blob, error) {
	switch target.Type {
	case TargetTenant:
		return s.tenantBlob(ctx, target.ID)
	case TargetUser:
		tenantID, exists, err := s.repo.UserTenant(ctx, target.ID)
		if err != nil {
			return nil, err
		}
		if !exists || tenantID == 0 {
			return blob{}, nil
		}
		return s.tenantBlob(ctx, tenantID)
	default:
		return blob{}, nil
	}
}

func (s *Service) userBlobForTarget(ctx context.Context, target Target) (blob, error) {
	if target.Type == TargetUser {
		return s.userBlob(ctx, target.ID)
	}
	return blob{}, nil
}

func (s *Service) globalBlob(ctx context.Context) (blob, error) {
	var cached blob
	found, err := s.cache.Get(ctx, globalBlobKey(), &cached)
	if err != nil {
		return nil, apperr.Wrap(apperr.InternalServerError, "read feature flags cache", err)
	}
	if found {
		return cached, nil
	}
	rows, err := s.repo.ListGlobal(ctx)
	if err != nil {
		return nil, err
	}
	values := make(blob, len(rows))
	for _, row := range rows {
		values[row.Key] = row.Value
	}
	_ = s.cache.Set(ctx, globalBlobKey(), values, flagsTTL)
	return values, nil
}

func (s *Service) tenantBlob(ctx context.Context, tenantID int64) (blob, error) {
	key := tenantBlobKey(tenantID)
	var cached blob
	found, err := s.cache.Get(ctx, key, &cached)
	if err != nil {
		return nil, apperr.Wrap(apperr.InternalServerError, "read feature flags cache", err)
	}
	if found {
		return cached, nil
	}
	rows, err := s.repo.ListByTarget(ctx, Target{Type: TargetTenant, ID: tenantID})
	if err != nil {
		return nil, err
	}
	values := make(blob, len(rows))
	for _, row := range rows {
		values[row.Key] = row.Value
	}
	_ = s.cache.Set(ctx, key, values, flagsTTL)
	return values, nil
}

func (s *Service) userBlob(ctx context.Context, userID int64) (blob, error) {
	key := userBlobKey(userID)
	var cached blob
	found, err := s.cache.Get(ctx, key, &cached)
	if err != nil {
		return nil, apperr.Wrap(apperr.InternalServerError, "read feature flags cache", err)
	}
	if found {
		return cached, nil
	}
	rows, err := s.repo.ListByTarget(ctx, Target{Type: TargetUser, ID: userID})
	if err != nil {
		return nil, err
	}
	values := make(blob, len(rows))
	for _, row := range rows {
		values[row.Key] = row.Value
	}
	_ = s.cache.Set(ctx, key, values, flagsTTL)
	return values, nil
}
