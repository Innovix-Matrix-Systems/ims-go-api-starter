package mail

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/config"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardWriter{}, nil))
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestLogDriverSends(t *testing.T) {
	driver, err := New(config.Mail{Driver: "log", FromAddress: "from@example.com"}, discardLogger(), false)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if driver.Name() != "log" {
		t.Fatalf("driver name = %q", driver.Name())
	}
	if err := driver.Send(context.Background(), Message{To: "to@example.com", Subject: "Hi", Text: "Body"}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
}

func TestSMTPDriverSendsAgainstListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	messages := make(chan string, 1)
	go serveFakeSMTP(t, listener, messages)

	port := listener.Addr().(*net.TCPAddr).Port
	driver := NewSMTPDriver(config.Mail{
		Host:        "127.0.0.1",
		Port:        port,
		FromAddress: "from@example.com",
		FromName:    "IMS Test",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := driver.Send(ctx, Message{To: "to@example.com", Subject: "OTP code", Text: "123456 is your OTP."}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	select {
	case captured := <-messages:
		if !strings.Contains(captured, "Subject: OTP code") {
			t.Errorf("captured mail missing subject:\n%s", captured)
		}
		if !strings.Contains(captured, "123456 is your OTP.") {
			t.Errorf("captured mail missing body:\n%s", captured)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fake SMTP server captured no message")
	}
}

func serveFakeSMTP(t *testing.T, listener net.Listener, messages chan<- string) {
	t.Helper()
	conn, err := listener.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)
	fmt.Fprint(conn, "220 fake ESMTP\r\n")
	var body strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		command := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(command, "EHLO"), strings.HasPrefix(command, "HELO"):
			fmt.Fprint(conn, "250-fake\r\n250 OK\r\n")
		case strings.HasPrefix(command, "MAIL FROM"), strings.HasPrefix(command, "RCPT TO"):
			fmt.Fprint(conn, "250 OK\r\n")
		case command == "DATA":
			fmt.Fprint(conn, "354 End data with <CR><LF>.<CR><LF>\r\n")
			for {
				dataLine, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimSpace(dataLine) == "." {
					break
				}
				body.WriteString(dataLine)
			}
			fmt.Fprint(conn, "250 OK\r\n")
			select {
			case messages <- body.String():
			default:
			}
		case command == "QUIT":
			fmt.Fprint(conn, "221 Bye\r\n")
			return
		default:
			fmt.Fprint(conn, "250 OK\r\n")
		}
	}
}
