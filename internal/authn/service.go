package authn

import (
	"context"
	"fmt"
	"math"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/config"
)

// Login status values returned by the login flow.
const (
	StatusAuthSuccess = "AUTH_SUCCESS"
	StatusOTPRequired = "OTP_REQUIRED"
)

// LoginResult is the outcome of a successful login or OTP dispatch.
type LoginResult struct {
	User    *UserRecord
	Status  string
	Message string
	Token   *Token
}

// Service implements the login, OTP verification and logout workflows.
type Service struct {
	users    UserLookup
	auth     Authenticator
	otp      *OTPService
	throttle *Throttle
	cfg      config.Auth
}

// NewService creates the authentication workflow service.
func NewService(users UserLookup, auth Authenticator, otp *OTPService, throttle *Throttle, cfg config.Auth) *Service {
	return &Service{users: users, auth: auth, otp: otp, throttle: throttle, cfg: cfg}
}

// Login authenticates credentials and issues a device token (or dispatches an
// OTP when enabled).
func (s *Service) Login(ctx context.Context, email, password, device, ip string) (*LoginResult, error) {
	key := loginThrottleKey(email, ip)
	if err := s.checkThrottle(ctx, key); err != nil {
		return nil, err
	}

	record, err := s.findUser(ctx, email)
	if err != nil {
		return nil, err
	}
	if err := s.verifyBeforeLogin(record); err != nil {
		return nil, err
	}

	if bcrypt.CompareHashAndPassword([]byte(record.Password), []byte(password)) != nil {
		_ = s.throttle.Hit(ctx, key)
		return nil, apperr.New(apperr.InvalidCredentials, "The provided password is incorrect.")
	}
	if s.cfg.OTPEnabled {
		if _, err := s.otp.Send(ctx, record.ID, record.Email); err != nil {
			return nil, err
		}
		return &LoginResult{
			User:    record,
			Status:  StatusOTPRequired,
			Message: "An OTP has been sent to your registered contact.",
		}, nil
	}
	return s.completeLogin(ctx, record, device, key)
}

// VerifyOTP completes a login started with an OTP challenge.
func (s *Service) VerifyOTP(ctx context.Context, email, code, device, ip string) (*LoginResult, error) {
	key := loginThrottleKey(email, ip)
	if err := s.checkThrottle(ctx, key); err != nil {
		return nil, err
	}

	record, err := s.findUser(ctx, email)
	if err != nil {
		return nil, err
	}
	if err := s.verifyBeforeLogin(record); err != nil {
		return nil, err
	}

	if err := s.otp.Verify(ctx, record.ID, code); err != nil {
		_ = s.throttle.Hit(ctx, key)
		return nil, err
	}
	return s.completeLogin(ctx, record, device, key)
}

// Logout revokes the caller's token for one device.
func (s *Service) Logout(ctx context.Context, userID int64, device string) error {
	return s.auth.Revoke(ctx, userID, device)
}

func (s *Service) completeLogin(ctx context.Context, record *UserRecord, device, throttleKey string) (*LoginResult, error) {
	token, err := s.auth.Issue(ctx, record.ID, device)
	if err != nil {
		return nil, err
	}
	_ = s.throttle.Clear(ctx, throttleKey)
	if err := s.users.TouchLogin(ctx, record.ID, device); err != nil {
		return nil, err
	}
	return &LoginResult{
		User:    record,
		Status:  StatusAuthSuccess,
		Message: "Login is successful.",
		Token:   &token,
	}, nil
}

func (s *Service) checkThrottle(ctx context.Context, key string) error {
	tooMany, retryAfter, err := s.throttle.TooMany(ctx, key)
	if err != nil {
		return apperr.Wrap(apperr.InternalServerError, "check login attempts", err)
	}
	if !tooMany {
		return nil
	}
	seconds := int(math.Ceil(retryAfter.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	return apperr.New(apperr.AccountLocked, "Too many login attempts. Please try again later.").WithRetryAfter(seconds)
}

func (s *Service) findUser(ctx context.Context, email string) (*UserRecord, error) {
	record, err := s.users.FindByEmail(ctx, email)
	if err != nil {
		if apperr.Is(err, apperr.ResourceNotFound) {
			return nil, apperr.Validation(map[string][]string{
				"email": {"The selected email is invalid."},
			})
		}
		return nil, err
	}
	return record, nil
}

func (s *Service) verifyBeforeLogin(record *UserRecord) error {
	if s.cfg.CheckIsActive && !record.IsActive {
		return apperr.New(apperr.AccountDeactivated, "Your account is deactivated.")
	}
	if s.cfg.CheckEmailVerified && !record.EmailVerified {
		return apperr.New(apperr.AccountUnverified, "Your email address is not verified.")
	}
	if s.cfg.CheckPhoneVerified && !record.PhoneVerified {
		return apperr.New(apperr.AccountUnverified, "Your phone number is not verified.")
	}
	return nil
}

func loginThrottleKey(email, ip string) string {
	return fmt.Sprintf("login:%s|%s", strings.ToLower(strings.TrimSpace(email)), ip)
}
