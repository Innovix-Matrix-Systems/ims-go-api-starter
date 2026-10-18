package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/billing/domain"
)

// SubscriptionService implements the subscription lifecycle.
type SubscriptionService struct {
	subscriptions domain.SubscriptionRepository
	packages      domain.PackageRepository
	payments      domain.PaymentRepository
	approvals     domain.ApprovalRepository
	orchestrator  Gateway
	notifier      Notifier
	now           func() time.Time
}

// NewSubscriptionService creates the subscription service.
func NewSubscriptionService(
	subscriptions domain.SubscriptionRepository,
	packages domain.PackageRepository,
	payments domain.PaymentRepository,
	approvals domain.ApprovalRepository,
	orchestrator Gateway,
) *SubscriptionService {
	return &SubscriptionService{
		subscriptions: subscriptions,
		packages:      packages,
		payments:      payments,
		approvals:     approvals,
		orchestrator:  orchestrator,
		now:           time.Now,
	}
}

// StatusResult is the tenant's current subscription view.
type StatusResult struct {
	Subscription *domain.Subscription `json:"subscription"`
	Package      *domain.Package      `json:"package"`
	Active       bool                 `json:"active"`
}

// Status returns the tenant's active subscription, falling back to the latest
// one (a pending/cancelled row) when no plan is currently active.
func (s *SubscriptionService) Status(ctx context.Context, tenantID int64) (*StatusResult, error) {
	subscription, found, err := s.subscriptions.GetActiveForTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if !found {
		subscription, found, err = s.subscriptions.GetLatestForTenant(ctx, tenantID)
		if err != nil {
			return nil, err
		}
	}
	result := &StatusResult{}
	if !found {
		return result, nil
	}
	result.Subscription = subscription
	pkg, err := s.packages.Get(ctx, subscription.PackageID)
	if err != nil {
		return nil, err
	}
	result.Package = pkg
	result.Active = (subscription.Status == domain.StatusActive || subscription.Status == domain.StatusTrial) &&
		subscription.ExpiresAt != nil && subscription.ExpiresAt.After(s.now())
	return result, nil
}

// GetForTenant loads a subscription and verifies it belongs to the tenant.
func (s *SubscriptionService) GetForTenant(ctx context.Context, tenantID, subscriptionID int64) (*domain.Subscription, error) {
	subscription, err := s.subscriptions.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	if subscription.TenantID != tenantID {
		return nil, apperr.Newf(apperr.ResourceNotFound, "Subscription with ID '%d' was not found.", subscriptionID)
	}
	return subscription, nil
}

// StartTrial starts a trial subscription when the package offers one.
func (s *SubscriptionService) StartTrial(ctx context.Context, tenantID, packageID int64) (*domain.Subscription, error) {
	pkg, err := s.packages.Get(ctx, packageID)
	if err != nil {
		return nil, err
	}
	if pkg.TrialDays <= 0 {
		return nil, apperr.New(apperr.BusinessLogicError, "This package does not offer a trial period.")
	}

	now := s.now()
	expires := now.AddDate(0, 0, int(pkg.TrialDays))
	if err := s.subscriptions.DeactivateActive(ctx, tenantID); err != nil {
		return nil, err
	}
	return s.subscriptions.Create(ctx, domain.SubscriptionInput{
		TenantID:    tenantID,
		PackageID:   pkg.ID,
		Status:      domain.StatusTrial,
		StartsAt:    &now,
		TrialEndsAt: &expires,
		ExpiresAt:   &expires,
	})
}

// Subscribe creates a pending subscription, charges the selected gateway and
// (for manual methods) creates the staff approval request.
func (s *SubscriptionService) Subscribe(ctx context.Context, tenantID, packageID int64, method string, paymentData map[string]any, actorID int64) (*domain.Subscription, *domain.Payment, error) {
	switch method {
	case domain.MethodBkash, domain.MethodNagad, domain.MethodCash, domain.MethodBankTransfer:
	default:
		return nil, nil, apperr.Validation(map[string][]string{
			"payment_method": {"The selected payment method is invalid."},
		})
	}

	pkg, err := s.packages.Get(ctx, packageID)
	if err != nil {
		return nil, nil, err
	}
	if !pkg.IsActive {
		return nil, nil, apperr.New(apperr.BusinessLogicError, "This package is not available.")
	}

	now := s.now()
	expires := nextExpiry(pkg.BillingCycle, now)
	subscription, err := s.subscriptions.Create(ctx, domain.SubscriptionInput{
		TenantID:  tenantID,
		PackageID: pkg.ID,
		Status:    domain.StatusPending,
		StartsAt:  &now,
		ExpiresAt: &expires,
	})
	if err != nil {
		return nil, nil, err
	}

	payment, err := s.orchestrator.Charge(ctx, tenantID, &subscription.ID, method, pkg.Price, paymentData, &actorID)
	if err != nil {
		reason := "Payment initiation failed."
		_, _ = s.subscriptions.UpdateStatus(ctx, subscription.ID, domain.StatusCancelled, &now, &reason)
		return nil, nil, err
	}

	if domain.IsManualMethod(method) {
		encoded, _ := json.Marshal(paymentData)
		if _, err := s.approvals.Create(ctx, domain.ApprovalInput{
			TenantID:       tenantID,
			SubscriptionID: subscription.ID,
			PaymentID:      payment.ID,
			PaymentMethod:  method,
			PaymentData:    encoded,
			CreatedBy:      &actorID,
		}); err != nil {
			reason := "Payment approval could not be created."
			_, _ = s.subscriptions.UpdateStatus(ctx, subscription.ID, domain.StatusCancelled, &now, &reason)
			_, _ = s.payments.UpdateStatus(ctx, payment.ID, domain.PaymentFailed, nil, nil)
			return nil, nil, err
		}
	}

	return subscription, payment, nil
}

// Activate marks a subscription active (renewals, callbacks, approvals).
func (s *SubscriptionService) Activate(ctx context.Context, subscriptionID int64) (*domain.Subscription, error) {
	subscription, err := s.subscriptions.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	expires := s.expiryForActivation(ctx, subscription)
	if err := s.subscriptions.DeactivateActive(ctx, subscription.TenantID); err != nil {
		return nil, err
	}
	return s.subscriptions.Activate(ctx, subscriptionID, expires)
}

// Cancel ends a subscription; access continues until expires_at.
func (s *SubscriptionService) Cancel(ctx context.Context, subscriptionID int64, reason *string) (*domain.Subscription, error) {
	subscription, err := s.subscriptions.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	if subscription.Status == domain.StatusCancelled || subscription.Status == domain.StatusExpired {
		return nil, apperr.New(apperr.InvalidState, "This subscription has already ended.")
	}
	if reason == nil || *reason == "" {
		value := "Cancelled by administrator."
		reason = &value
	}
	now := s.now()
	return s.subscriptions.UpdateStatus(ctx, subscriptionID, domain.StatusCancelled, &now, reason)
}

// ApplyOverrides merges per-tenant feature overrides (super-admin only).
func (s *SubscriptionService) ApplyOverrides(ctx context.Context, subscriptionID int64, overrides map[string]any) (*domain.Subscription, error) {
	subscription, err := s.subscriptions.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	for key, value := range overrides {
		if !domain.Feature(key).IsKnown() {
			return nil, apperr.Validation(map[string][]string{
				"overrides": {"Unknown feature key '" + key + "'."},
			})
		}
		switch value.(type) {
		case bool, float64, int, int64:
		default:
			return nil, apperr.Validation(map[string][]string{
				"overrides": {"Override values must be booleans or numbers."},
			})
		}
	}
	merged := make(map[string]any, len(subscription.Overrides)+len(overrides))
	for key, value := range subscription.Overrides {
		merged[key] = value
	}
	for key, value := range overrides {
		merged[key] = value
	}
	encoded, err := json.Marshal(merged)
	if err != nil {
		return nil, apperr.Wrap(apperr.InternalServerError, "encode overrides", err)
	}
	return s.subscriptions.SetOverrides(ctx, subscriptionID, encoded)
}

// HandleGatewayCallback finalizes a gateway payment and activates its
// subscription on success. Completed or refunded payments are returned
// unchanged so repeated callbacks are idempotent.
func (s *SubscriptionService) HandleGatewayCallback(ctx context.Context, paymentID int64, method string, payload map[string]any) (*domain.Payment, error) {
	payment, err := s.payments.Get(ctx, paymentID)
	if err != nil {
		return nil, err
	}
	if payment.PaymentMethod != method {
		return nil, apperr.New(apperr.InvalidInput, "The callback gateway does not match the payment method.")
	}
	if payment.Status == domain.PaymentCompleted || payment.Status == domain.PaymentRefunded {
		return payment, nil
	}

	result, err := s.orchestrator.HandleCallback(ctx, method, payload)
	if err != nil {
		return nil, err
	}

	response, _ := json.Marshal(result.RawResponse)
	var gatewayStatus *string
	if result.GatewayStatus != nil && *result.GatewayStatus != "" {
		gatewayStatus = result.GatewayStatus
	}
	// Keep the original gateway reference (the value the callback lookup and
	// verify/refund calls rely on); the gateway transaction id lives in the
	// stored response payload.
	gatewayRef := result.GatewayRef
	if payment.GatewayRef != nil {
		gatewayRef = nil
	}
	_ = s.payments.UpdateGateway(ctx, payment.ID, gatewayRef, gatewayStatus, response)

	if !result.IsSuccessful {
		return s.payments.UpdateStatus(ctx, payment.ID, domain.PaymentFailed, nil, gatewayStatus)
	}

	if payment.SubscriptionID != nil {
		if _, err := s.Activate(ctx, *payment.SubscriptionID); err != nil {
			return nil, err
		}
	}
	return s.payments.UpdateStatus(ctx, payment.ID, domain.PaymentCompleted, nil, gatewayStatus)
}

// List returns subscriptions with pagination (admin surface).
func (s *SubscriptionService) List(ctx context.Context, status string, limit, offset int32) ([]domain.Subscription, int64, error) {
	return s.subscriptions.List(ctx, status, limit, offset)
}

// ExpireOverdue marks overdue subscriptions as expired.
func (s *SubscriptionService) ExpireOverdue(ctx context.Context) (int64, error) {
	return s.subscriptions.ExpireOverdue(ctx)
}

// Notifier delivers in-app notifications for billing events (implemented by
// the notifications module; nil keeps the log-only behavior).
type Notifier interface {
	NotifyRenewalReminder(ctx context.Context, tenantID, subscriptionID int64, expiresAt time.Time, daysLeft int) error
}

// WithNotifier attaches the in-app notification notifier.
func (s *SubscriptionService) WithNotifier(notifier Notifier) *SubscriptionService {
	s.notifier = notifier
	return s
}

// SendRenewalReminders creates in-app reminders for subscriptions expiring
// within the configured day windows (logs when no notifier is attached).
func (s *SubscriptionService) SendRenewalReminders(ctx context.Context, logger *slog.Logger, days []int) (int, error) {
	if len(days) == 0 {
		days = []int{7, 3, 1}
	}
	maxDays := 0
	for _, day := range days {
		if day > maxDays {
			maxDays = day
		}
	}
	now := s.now()
	subscriptions, err := s.subscriptions.Expiring(ctx, now.AddDate(0, 0, maxDays))
	if err != nil {
		return 0, err
	}

	sent := 0
	for _, subscription := range subscriptions {
		if subscription.ExpiresAt == nil {
			continue
		}
		daysLeft := int(math.Ceil(subscription.ExpiresAt.Sub(now).Hours() / 24))
		for _, day := range days {
			if daysLeft != day {
				continue
			}
			if s.notifier != nil {
				if err := s.notifier.NotifyRenewalReminder(ctx, subscription.TenantID, subscription.ID, *subscription.ExpiresAt, daysLeft); err != nil {
					logger.Error("renewal reminder notification failed",
						"tenant_id", subscription.TenantID,
						"subscription_id", subscription.ID,
						"error", err,
					)
				} else {
					sent++
				}
			} else {
				logger.Info("renewal reminder queued",
					"tenant_id", subscription.TenantID,
					"subscription_id", subscription.ID,
					"days_left", daysLeft,
				)
				sent++
			}
			break
		}
	}
	return sent, nil
}

// Expiring returns subscriptions expiring before the given time.
func (s *SubscriptionService) Expiring(ctx context.Context, before time.Time) ([]domain.Subscription, error) {
	return s.subscriptions.Expiring(ctx, before)
}

func (s *SubscriptionService) expiryForActivation(ctx context.Context, subscription *domain.Subscription) time.Time {
	now := s.now()
	if subscription.ExpiresAt != nil && subscription.ExpiresAt.After(now) {
		return *subscription.ExpiresAt
	}
	pkg, err := s.packages.Get(ctx, subscription.PackageID)
	if err != nil {
		return now.AddDate(0, 1, 0)
	}
	return nextExpiry(pkg.BillingCycle, now)
}

func nextExpiry(cycle string, from time.Time) time.Time {
	switch cycle {
	case domain.CycleYearly:
		return from.AddDate(1, 0, 0)
	case domain.CycleOneTime:
		return from.AddDate(100, 0, 0)
	default:
		return from.AddDate(0, 1, 0)
	}
}
