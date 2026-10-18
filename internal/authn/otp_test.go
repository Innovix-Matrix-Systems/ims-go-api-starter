package authn

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/config"
	cachememory "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/cache/memory"
)

type recordingMailer struct {
	mu      sync.Mutex
	to      string
	subject string
	text    string
	calls   int
}

func (m *recordingMailer) EnqueueMail(_ context.Context, to, subject, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.to, m.subject, m.text = to, subject, text
	m.calls++
	return nil
}

func (m *recordingMailer) snapshot() (string, string, string, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.to, m.subject, m.text, m.calls
}

func TestOTPSendEnqueuesMailDelivery(t *testing.T) {
	store := cachememory.New("otp-test-")
	t.Cleanup(func() { _ = store.Clear(context.Background()) })

	service := NewOTPService(store, config.Auth{OTPLength: 6, OTPExpiry: 5 * time.Minute}, slog.Default(), false)
	mailer := &recordingMailer{}
	service.WithMailer(mailer)

	code, err := service.Send(context.Background(), 7, "user@example.com")
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if len(code) != 6 {
		t.Fatalf("code = %q, want 6 digits", code)
	}

	// The stored code verifies.
	if err := service.Verify(context.Background(), 7, code); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}

	to, subject, text, calls := mailer.snapshot()
	if calls != 1 {
		t.Fatalf("mail enqueued %d times, want 1", calls)
	}
	if to != "user@example.com" {
		t.Errorf("mail to = %q", to)
	}
	if subject == "" {
		t.Error("mail subject is empty")
	}
	if !strings.Contains(text, code) {
		t.Errorf("mail body %q does not contain the code %q", text, code)
	}
	if !strings.Contains(text, "OTP") {
		t.Errorf("mail body %q does not mention OTP", text)
	}
}

func TestOTPSendWithoutMailerStillStores(t *testing.T) {
	store := cachememory.New("otp-test-")
	t.Cleanup(func() { _ = store.Clear(context.Background()) })

	service := NewOTPService(store, config.Auth{OTPLength: 6, OTPExpiry: 5 * time.Minute}, slog.Default(), false)
	code, err := service.Send(context.Background(), 8, "")
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if err := service.Verify(context.Background(), 8, code); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
}
