package service

import (
	"context"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/billing/domain"
)

// PackageService manages billing plans and their feature definitions.
type PackageService struct {
	packages domain.PackageRepository
}

// NewPackageService creates the package service.
func NewPackageService(packages domain.PackageRepository) *PackageService {
	return &PackageService{packages: packages}
}

// ListPublic returns active packages with their features.
func (s *PackageService) ListPublic(ctx context.Context) ([]domain.Package, error) {
	items, err := s.packages.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	return s.attachFeatures(ctx, items)
}

// List returns all packages with their features (admin surface).
func (s *PackageService) List(ctx context.Context) ([]domain.Package, error) {
	items, err := s.packages.List(ctx)
	if err != nil {
		return nil, err
	}
	return s.attachFeatures(ctx, items)
}

// Get returns one package with features.
func (s *PackageService) Get(ctx context.Context, id int64) (*domain.Package, error) {
	pkg, err := s.packages.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	features, err := s.packages.FeaturesFor(ctx, id)
	if err != nil {
		return nil, err
	}
	pkg.Features = features
	return pkg, nil
}

// Create adds a package and its features.
func (s *PackageService) Create(ctx context.Context, input domain.PackageInput, features []domain.FeatureInput) (*domain.Package, error) {
	validated, err := validateFeatures(features)
	if err != nil {
		return nil, err
	}
	pkg, err := s.packages.Create(ctx, input)
	if err != nil {
		return nil, err
	}
	if len(validated) > 0 {
		if err := s.packages.ReplaceFeatures(ctx, pkg.ID, validated); err != nil {
			return nil, err
		}
	}
	return s.Get(ctx, pkg.ID)
}

// Update modifies a package; features are replaced only when supplied.
func (s *PackageService) Update(ctx context.Context, id int64, input domain.PackageInput, features []domain.FeatureInput) (*domain.Package, error) {
	validated, err := validateFeatures(features)
	if err != nil {
		return nil, err
	}
	if _, err := s.packages.Update(ctx, id, input); err != nil {
		return nil, err
	}
	if features != nil {
		if err := s.packages.ReplaceFeatures(ctx, id, validated); err != nil {
			return nil, err
		}
	}
	return s.Get(ctx, id)
}

// Delete removes a package; subscriptions referencing it block deletion (409).
func (s *PackageService) Delete(ctx context.Context, id int64) error {
	return s.packages.Delete(ctx, id)
}

func (s *PackageService) attachFeatures(ctx context.Context, items []domain.Package) ([]domain.Package, error) {
	if len(items) == 0 {
		return items, nil
	}
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	features, err := s.packages.FeaturesForPackages(ctx, ids)
	if err != nil {
		return nil, err
	}
	grouped := make(map[int64][]domain.PackageFeature, len(ids))
	for _, feature := range features {
		grouped[feature.PackageID] = append(grouped[feature.PackageID], feature)
	}
	for i := range items {
		items[i].Features = grouped[items[i].ID]
		if items[i].Features == nil {
			items[i].Features = []domain.PackageFeature{}
		}
	}
	return items, nil
}

func validateFeatures(features []domain.FeatureInput) ([]domain.FeatureInput, error) {
	seen := map[string]struct{}{}
	validated := make([]domain.FeatureInput, 0, len(features))
	for _, feature := range features {
		typed := domain.Feature(feature.FeatureKey)
		if !typed.IsKnown() {
			return nil, apperr.Validation(map[string][]string{
				"features": {"Unknown feature key '" + feature.FeatureKey + "'."},
			})
		}
		if _, duplicate := seen[feature.FeatureKey]; duplicate {
			continue
		}
		seen[feature.FeatureKey] = struct{}{}
		validated = append(validated, feature)
	}
	return validated, nil
}
