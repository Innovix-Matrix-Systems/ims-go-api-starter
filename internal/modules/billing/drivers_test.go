package billing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/billing/domain"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/cache/memory"
)

func testBkashConfig() config.Bkash {
	return config.Bkash{
		BaseURL:   "https://bkash.invalid",
		AppKey:    "key",
		AppSecret: "secret",
		Username:  "user",
		Password:  "pass",
	}
}

func TestBkashDriverAgainstStub(t *testing.T) {
	calls := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls[r.URL.Path]++
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/tokenized/checkout/token/grant":
			_ = json.NewEncoder(w).Encode(map[string]any{"id_token": "token-1", "statusCode": "0000"})
		case "/tokenized/checkout/create":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"statusCode":    "0000",
				"statusMessage": "Successful",
				"paymentID":     "PAY-123",
				"bkashURL":      "https://bkash.test/redirect",
			})
		case "/tokenized/checkout/execute":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"statusCode":        "0000",
				"transactionStatus": "Completed",
				"trxID":             "TRX-9",
			})
		case "/tokenized/checkout/payment/status":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"transactionStatus": "Completed",
				"trxID":             "TRX-9",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := testBkashConfig()
	cfg.BaseURL = server.URL
	store := memory.New("bkash-test-")
	defer store.Close()
	driver := NewBkashDriver(cfg, store)
	ctx := context.Background()

	payment := &domain.Payment{
		ID: 1, TenantID: 5, Amount: 125050, Currency: "BDT", InvoiceNumber: "INV-1",
		Status: domain.PaymentPending, PaymentMethod: domain.MethodBkash,
	}

	result, err := driver.Initiate(ctx, payment, nil)
	if err != nil {
		t.Fatalf("Initiate() error = %v", err)
	}
	if !result.Success || result.Reference == nil || *result.Reference != "PAY-123" {
		t.Fatalf("initiate result = %+v", result)
	}
	if result.RedirectURL == nil || *result.RedirectURL != "https://bkash.test/redirect" {
		t.Fatalf("redirect = %v", result.RedirectURL)
	}

	// Second initiate reuses the cached token (grant called once).
	if _, err := driver.Initiate(ctx, payment, nil); err != nil {
		t.Fatalf("second Initiate() error = %v", err)
	}
	if calls["/tokenized/checkout/token/grant"] != 1 {
		t.Fatalf("token grant calls = %d, want 1 (cached)", calls["/tokenized/checkout/token/grant"])
	}

	payment.GatewayRef = result.Reference
	verification, err := driver.Verify(ctx, payment)
	if err != nil || !verification.IsCompleted {
		t.Fatalf("Verify() = %+v, %v", verification, err)
	}

	callback, err := driver.HandleCallback(ctx, map[string]any{"status": "success", "paymentID": "PAY-123"})
	if err != nil {
		t.Fatalf("HandleCallback() error = %v", err)
	}
	if !callback.IsSuccessful || callback.GatewayRef == nil || *callback.GatewayRef != "TRX-9" {
		t.Fatalf("callback = %+v", callback)
	}

	// Failed callback short-circuits without executing.
	callback, err = driver.HandleCallback(ctx, map[string]any{"status": "failure", "paymentID": "PAY-123"})
	if err != nil || callback.IsSuccessful {
		t.Fatalf("failed callback = %+v, %v", callback, err)
	}
}

func TestBkashDriverWithoutConfig(t *testing.T) {
	store := memory.New("bkash-unconfigured-")
	defer store.Close()
	driver := NewBkashDriver(config.Bkash{}, store)

	_, err := driver.Initiate(context.Background(), &domain.Payment{Amount: 100, Currency: "BDT"}, nil)
	if err == nil {
		t.Fatal("Initiate() without config = nil error")
	}
}
