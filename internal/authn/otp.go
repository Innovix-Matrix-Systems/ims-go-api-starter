package authn

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"math/big"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/cache"
)

// MailEnqueuer delivers transactional mail through the queue (implemented by
// the app composition root; breaks the authn→mail dependency cycle).
type MailEnqueuer interface {
	EnqueueMail(ctx context.Context, to, subject, text string) error
}

// OTPService stores short-lived one-time codes in the cache and delivers them
// through the queue when a mail enqueuer is attached.
type OTPService struct {
	store  cache.Store
	cfg    config.Auth
	logger *slog.Logger
	debug  bool
	mailer MailEnqueuer
}

// NewOTPService creates the OTP service. When debug is true the generated code
// is included in logs for local development.
func NewOTPService(store cache.Store, cfg config.Auth, logger *slog.Logger, debug bool) *OTPService {
	return &OTPService{store: store, cfg: cfg, logger: logger, debug: debug}
}

// WithMailer delivers OTPs through the queue using the mail.send job.
func (s *OTPService) WithMailer(mailer MailEnqueuer) *OTPService {
	s.mailer = mailer
	return s
}

// OTPKey namespaces codes by user.
func OTPKey(userID int64) string {
	return fmt.Sprintf("otp:user:%d", userID)
}

// Send generates a fresh OTP, stores it with the configured expiry and queues
// its delivery. The code is returned for tests and debug logging.
func (s *OTPService) Send(ctx context.Context, userID int64, email string) (string, error) {
	code, err := randomDigits(s.cfg.OTPLength)
	if err != nil {
		return "", apperr.Wrap(apperr.InternalServerError, "generate otp", err)
	}
	if err := s.store.Set(ctx, OTPKey(userID), code, s.cfg.OTPExpiry); err != nil {
		return "", apperr.Wrap(apperr.InternalServerError, "store otp", err)
	}

	if s.mailer != nil && email != "" {
		if err := s.mailer.EnqueueMail(ctx, email, "Your OTP code", fmt.Sprintf("%s is your OTP.", code)); err != nil {
			return "", apperr.Wrap(apperr.ServiceUnavailable, "The OTP could not be sent.", err)
		}
	}

	attrs := []any{"user_id", userID, "expires_in", s.cfg.OTPExpiry.String()}
	if s.debug {
		attrs = append(attrs, "code", code)
	}
	s.logger.Info("otp generated", attrs...)
	return code, nil
}

// Verify compares the provided code in constant time and clears it on success.
func (s *OTPService) Verify(ctx context.Context, userID int64, code string) error {
	var expected string
	found, err := s.store.Get(ctx, OTPKey(userID), &expected)
	if err != nil {
		return apperr.Wrap(apperr.InternalServerError, "read otp", err)
	}
	if !found {
		return apperr.New(apperr.OTPExpired, "The OTP code has expired.")
	}
	if subtle.ConstantTimeCompare([]byte(expected), []byte(code)) != 1 {
		return apperr.New(apperr.OTPInvalid, "The OTP code is invalid.")
	}
	_ = s.store.Delete(ctx, OTPKey(userID))
	return nil
}

// Clear removes any stored code.
func (s *OTPService) Clear(ctx context.Context, userID int64) {
	_ = s.store.Delete(ctx, OTPKey(userID))
}

func randomDigits(length int) (string, error) {
	max := big.NewInt(10)
	digits := make([]byte, 0, length)
	for len(digits) < length {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		digits = append(digits, byte('0'+n.Int64()))
	}
	if digits[0] == '0' {
		digits[0] = '1'
	}
	return string(digits), nil
}
