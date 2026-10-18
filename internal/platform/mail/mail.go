// Package mail provides the mail driver abstraction (log and smtp drivers)
// and the mail.send queue job used for OTP delivery and reminders.
package mail

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/config"
)

// Message is one outbound mail.
type Message struct {
	To      string
	Subject string
	Text    string
}

// Driver delivers mail messages.
type Driver interface {
	Send(ctx context.Context, message Message) error
	Name() string
}

// New selects the configured mail driver.
func New(cfg config.Mail, logger *slog.Logger, debug bool) (Driver, error) {
	switch cfg.Driver {
	case "log", "":
		return &LogDriver{logger: logger, debug: debug, from: cfg.FromAddress}, nil
	case "smtp":
		return NewSMTPDriver(cfg), nil
	default:
		return nil, fmt.Errorf("mail: unsupported driver %q", cfg.Driver)
	}
}
