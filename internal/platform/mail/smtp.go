package mail

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/config"
)

// SMTPDriver delivers mail through an SMTP server (plain, STARTTLS or TLS).
type SMTPDriver struct {
	cfg config.Mail
}

// NewSMTPDriver creates the SMTP driver.
func NewSMTPDriver(cfg config.Mail) *SMTPDriver {
	return &SMTPDriver{cfg: cfg}
}

// Name implements Driver.
func (d *SMTPDriver) Name() string { return "smtp" }

// Send implements Driver.
func (d *SMTPDriver) Send(ctx context.Context, message Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	host := d.cfg.Host
	addr := net.JoinHostPort(host, strconv.Itoa(d.cfg.Port))
	serverName := strings.Split(host, ":")[0]

	var client *smtp.Client
	var err error
	if strings.EqualFold(d.cfg.Encryption, "tls") {
		conn, dialErr := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", addr, &tls.Config{ServerName: serverName})
		if dialErr != nil {
			return fmt.Errorf("mail: smtp dial: %w", dialErr)
		}
		// A stalled SMTP server must not hang the mail worker forever (M52).
		_ = conn.SetDeadline(smtpDeadline(ctx))
		client, err = smtp.NewClient(conn, serverName)
	} else {
		conn, dialErr := net.DialTimeout("tcp", addr, 10*time.Second)
		if dialErr != nil {
			return fmt.Errorf("mail: smtp dial: %w", dialErr)
		}
		_ = conn.SetDeadline(smtpDeadline(ctx))
		client, err = smtp.NewClient(conn, serverName)
	}
	if err != nil {
		return fmt.Errorf("mail: smtp connect: %w", err)
	}
	defer client.Close()

	if strings.EqualFold(d.cfg.Encryption, "starttls") {
		if err := client.StartTLS(&tls.Config{ServerName: serverName}); err != nil {
			return fmt.Errorf("mail: smtp starttls: %w", err)
		}
	}
	if d.cfg.Username != "" {
		auth := smtp.PlainAuth("", d.cfg.Username, d.cfg.Password, serverName)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("mail: smtp auth: %w", err)
		}
	}
	if err := client.Mail(d.cfg.FromAddress); err != nil {
		return fmt.Errorf("mail: smtp from: %w", err)
	}
	if err := client.Rcpt(message.To); err != nil {
		return fmt.Errorf("mail: smtp rcpt: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("mail: smtp data: %w", err)
	}
	if _, err := writer.Write([]byte(d.render(message))); err != nil {
		return fmt.Errorf("mail: smtp write: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("mail: smtp close: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("mail: smtp quit: %w", err)
	}
	return nil
}

func (d *SMTPDriver) render(message Message) string {
	var builder strings.Builder
	fromName := sanitizeHeader(d.cfg.FromName)
	if fromName != "" {
		fmt.Fprintf(&builder, "From: %s <%s>\r\n", fromName, sanitizeHeader(d.cfg.FromAddress))
	} else {
		fmt.Fprintf(&builder, "From: %s\r\n", sanitizeHeader(d.cfg.FromAddress))
	}
	fmt.Fprintf(&builder, "To: %s\r\n", sanitizeHeader(message.To))
	fmt.Fprintf(&builder, "Subject: %s\r\n", sanitizeHeader(message.Subject))
	fmt.Fprintf(&builder, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	builder.WriteString("MIME-Version: 1.0\r\n")
	builder.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	builder.WriteString("\r\n")
	builder.WriteString(message.Text)
	return builder.String()
}

// sanitizeHeader strips CR/LF (and NUL) so caller-supplied values cannot inject
// extra SMTP headers or recipients.
func sanitizeHeader(value string) string {
	return strings.NewReplacer("\r", "", "\n", "", "\x00", "").Replace(value)
}

// smtpDeadline bounds the whole SMTP conversation: the context deadline when
// there is one, otherwise two minutes.
func smtpDeadline(ctx context.Context) time.Time {
	if deadline, ok := ctx.Deadline(); ok {
		return deadline
	}
	return time.Now().Add(2 * time.Minute)
}
