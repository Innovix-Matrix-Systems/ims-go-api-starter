package app

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database/dbtest"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/seeder"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestNoSecretsInLogs is the redaction audit: passwords (success and failure)
// and issued tokens must never appear in application logs.
func TestNoSecretsInLogs(t *testing.T) {
	pool := dbtest.Pool(t)
	t.Setenv("DATABASE_DSN", dbtest.DSN(t))
	t.Setenv("APP_ENV_FILE", "")
	t.Setenv("BCRYPT_ROUNDS", "4")
	t.Setenv("AUTH_DEFAULT_PASSWORD", "secret123")
	t.Setenv("APP_DEBUG", "false")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	dbtest.Clean(t, pool,
		"personal_access_tokens", "role_has_permissions", "model_has_roles",
		"users", "roles", "permissions", "branches", "tenants",
	)

	captured := &syncBuffer{}
	logger := slog.New(slog.NewTextHandler(captured, &slog.HandlerOptions{Level: slog.LevelDebug}))

	deps, cleanup, err := NewDependencies(cfg, logger)
	if err != nil {
		t.Fatalf("NewDependencies() error = %v", err)
	}
	t.Cleanup(cleanup)
	if err := seeder.Run(t.Context(), cfg, deps.Tx.Queries(), deps.Tx, deps.Checker, logger); err != nil {
		t.Fatalf("seeder.Run() error = %v", err)
	}

	router := deps.Router()

	// Successful login (issued token must not be logged).
	rec := doRequest(t, router, http.MethodPost, "/api/v1/login",
		`{"email":"superadmin@ims.com","password":"secret123","device":"redact-device"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d body = %s", rec.Code, rec.Body.String())
	}
	token := extractToken(t, rec.Body.Bytes())

	// Failed login with a distinctive wrong password.
	const wrongPassword = "wrongpass-9f3a1b"
	fail := doRequest(t, router, http.MethodPost, "/api/v1/login",
		`{"email":"superadmin@ims.com","password":"`+wrongPassword+`","device":"redact-device"}`)
	if fail.Code == http.StatusOK {
		t.Fatalf("failed login unexpectedly succeeded")
	}

	logs := captured.String()
	if logs == "" {
		t.Fatal("no logs captured; test is not exercising logging")
	}
	for name, secret := range map[string]string{
		"configured password": "secret123",
		"wrong password":      wrongPassword,
		"issued token":        token,
	} {
		if secret == "" {
			continue
		}
		if bytes.Contains([]byte(logs), []byte(secret)) {
			t.Errorf("%s leaked into logs", name)
		}
	}
}

func extractToken(t *testing.T, body []byte) string {
	t.Helper()
	var parsed struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode login body: %v", err)
	}
	return parsed.Data.Token
}
