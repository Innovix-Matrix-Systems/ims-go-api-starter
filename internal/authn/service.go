package authn

import (
	"context"
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
	users     UserLookup
	auth      Authenticator
	otp       *OTPService
	throttle  *Throttle
	cfg       config.Auth
	dummyHash []byte
}

// NewService creates the authentication workflow service.
func NewService(users UserLookup, auth Authenticator, otp *OTPService, throttle *Throttle, cfg config.Auth) *Service {
	// A throwaway hash at the configured cost: comparing against it on the
	// unknown-email path equalises timing with the wrong-password path.
	dummy, _ := bcrypt.GenerateFromPassword([]byte("ims-go-api-starter:dummy:credentials"), cfg.BcryptRounds)
	return &Service{users: users, auth: auth, otp: otp, throttle: throttle, cfg: cfg, dummyHash: dummy}
}

// Login authenticates credentials and issues a device token (or dispatches an
// OTP when enabled).
//
// The response for "unknown email" and "wrong password" is identical in
// status, body and (via the dummy hash) timing; account-status checks run
// only after the password matches, so login cannot be used to enumerate
// accounts or their state.
func (s *Service) Login(ctx context.Context, email, password, device, ip string) (*LoginResult, error) {
	record, err := s.checkPassword(ctx, email, password, ip)
	if err != nil {
		return nil, err
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
	return s.completeLogin(ctx, record, device, email, ip)
}

// SignIn checks credentials like Login, with the same throttles and account
// checks, but issues no token: the OAuth sign-in page (MCP connectors) uses
// it and then issues its own. otpRequired is true when an OTP was sent and
// SignInOTP must follow.
func (s *Service) SignIn(ctx context.Context, email, password, ip string) (record *UserRecord, otpRequired bool, err error) {
	record, err = s.checkPassword(ctx, email, password, ip)
	if err != nil {
		return nil, false, err
	}
	if s.cfg.OTPEnabled {
		if _, err := s.otp.Send(ctx, record.ID, record.Email); err != nil {
			return nil, false, err
		}
		return record, true, nil
	}
	s.clearThrottles(ctx, email, ip)
	return record, false, nil
}

// SignInOTP completes SignIn when an OTP was required. It issues no token.
func (s *Service) SignInOTP(ctx context.Context, email, code, ip string) (*UserRecord, error) {
	record, err := s.checkOTP(ctx, email, code, ip)
	if err != nil {
		return nil, err
	}
	s.clearThrottles(ctx, email, ip)
	return record, nil
}

// OTPEnabled reports whether sign-in asks for a one-time code.
func (s *Service) OTPEnabled() bool { return s.cfg.OTPEnabled }

func (s *Service) checkPassword(ctx context.Context, email, password, ip string) (*UserRecord, error) {
	if err := s.checkThrottles(ctx, email, ip); err != nil {
		return nil, err
	}

	record, err := s.users.FindByEmail(ctx, email)
	if err != nil && !apperr.Is(err, apperr.ResourceNotFound) {
		return nil, err
	}
	if record == nil || bcrypt.CompareHashAndPassword([]byte(record.Password), []byte(password)) != nil {
		if record == nil {
			_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(password))
		}
		s.hitThrottles(ctx, email, ip)
		return nil, apperr.New(apperr.InvalidCredentials, "The provided credentials are incorrect.")
	}
	if err := s.verifyBeforeLogin(record); err != nil {
		return nil, err
	}
	return record, nil
}

// VerifyOTP completes a login started with an OTP challenge.
func (s *Service) VerifyOTP(ctx context.Context, email, code, device, ip string) (*LoginResult, error) {
	record, err := s.checkOTP(ctx, email, code, ip)
	if err != nil {
		return nil, err
	}
	return s.completeLogin(ctx, record, device, email, ip)
}

func (s *Service) checkOTP(ctx context.Context, email, code, ip string) (*UserRecord, error) {
	if err := s.checkThrottles(ctx, email, ip); err != nil {
		return nil, err
	}

	record, err := s.users.FindByEmail(ctx, email)
	if err != nil && !apperr.Is(err, apperr.ResourceNotFound) {
		return nil, err
	}
	if record == nil {
		_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(code))
		s.hitThrottles(ctx, email, ip)
		return nil, apperr.New(apperr.InvalidCredentials, "The provided credentials are incorrect.")
	}
	if err := s.verifyBeforeLogin(record); err != nil {
		return nil, err
	}

	if err := s.otp.Verify(ctx, record.ID, code); err != nil {
		s.hitThrottles(ctx, email, ip)
		return nil, err
	}
	return record, nil
}

// Logout revokes the caller's token for one device.
func (s *Service) Logout(ctx context.Context, userID int64, device string) error {
	return s.auth.Revoke(ctx, userID, device)
}

func (s *Service) completeLogin(ctx context.Context, record *UserRecord, device, email, ip string) (*LoginResult, error) {
	token, err := s.auth.Issue(ctx, record.ID, device)
	if err != nil {
		return nil, err
	}
	s.clearThrottles(ctx, email, ip)
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

// Throttling keeps two independent counters — one per account, one per IP —
// and locks on either. IP rotation no longer resets a targeted account's
// budget, and password spraying one account from many IPs is bounded by the
// per-IP counter.
func (s *Service) checkThrottles(ctx context.Context, email, ip string) error {
	tooMany, retryAfter, err := s.throttle.TooMany(ctx, accountThrottleKey(email))
	if err != nil {
		return apperr.Wrap(apperr.InternalServerError, "check login attempts", err)
	}
	if !tooMany {
		tooMany, retryAfter, err = s.throttle.TooMany(ctx, ipThrottleKey(ip))
		if err != nil {
			return apperr.Wrap(apperr.InternalServerError, "check login attempts", err)
		}
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

func (s *Service) hitThrottles(ctx context.Context, email, ip string) {
	_ = s.throttle.Hit(ctx, accountThrottleKey(email))
	_ = s.throttle.Hit(ctx, ipThrottleKey(ip))
}

func (s *Service) clearThrottles(ctx context.Context, email, ip string) {
	_ = s.throttle.Clear(ctx, accountThrottleKey(email))
	_ = s.throttle.Clear(ctx, ipThrottleKey(ip))
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

func accountThrottleKey(email string) string {
	return "login:acct:" + strings.ToLower(strings.TrimSpace(email))
}

func ipThrottleKey(ip string) string {
	return "login:ip:" + ip
}
