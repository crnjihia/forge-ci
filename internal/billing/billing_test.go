// path: internal/billing/billing_test.go
package billing

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestNormalizeKenyanPhone(t *testing.T) {
	cases := []struct {
		input    string
		expected string
		err      bool
	}{
		{"+254712345678", "254712345678", false},
		{"0712345678", "254712345678", false},
		{"712345678", "254712345678", false},
		{"+254 712 345 678", "254712345678", false},
		{"+254112345678", "254112345678", false},
		{"0112345678", "254112345678", false},
		{"112345678", "254112345678", false},
		{"+14155552671", "", true}, // Non-Kenyan (US)
		{"020123456", "", true},    // Nairobi landline
		{"invalid", "", true},
		{"", "", true},
	}

	for _, c := range cases {
		t.Run(c.input, func(t *testing.T) {
			got, err := NormalizeKenyanPhone(c.input)
			if c.err && err == nil {
				t.Errorf("input %q: expected error, got %q", c.input, got)
			}
			if !c.err && err != nil {
				t.Errorf("input %q: unexpected error: %v", c.input, err)
			}
			if !c.err && got != c.expected {
				t.Errorf("input %q: expected %q, got %q", c.input, c.expected, got)
			}
		})
	}
}

func TestInitiateTopup_Success(t *testing.T) {
	mockClient := NewMockMpesaClient()
	repo := NewMemoryBillingRepository()
	service := NewService(mockClient, repo, nil)

	ctx := context.Background()
	resp, err := service.InitiateTopup(ctx, "user-chris-01", "+254708374149", 500.00)
	if err != nil {
		t.Fatalf("unexpected error initiating topup: %v", err)
	}

	if resp.CheckoutRequestID == "" {
		t.Fatalf("expected non-empty CheckoutRequestID")
	}

	tx, err := repo.GetTransaction(ctx, resp.CheckoutRequestID)
	if err != nil {
		t.Fatalf("finding created transaction: %v", err)
	}
	if tx.AmountKES != 500.00 || tx.UserID != "user-chris-01" || tx.Status != "pending" {
		t.Errorf("unexpected transaction record: %+v", tx)
	}
	if tx.PhoneNumber != "254708374149" {
		t.Errorf("expected normalized phone 254708374149, got %q", tx.PhoneNumber)
	}
}

func TestProcessCallback_Success(t *testing.T) {
	mockClient := NewMockMpesaClient()
	repo := NewMemoryBillingRepository()
	service := NewService(mockClient, repo, nil)

	ctx := context.Background()
	resp, err := service.InitiateTopup(ctx, "user-chris-01", "0712345678", 250.00)
	if err != nil {
		t.Fatalf("initiate topup failed: %v", err)
	}

	// Craft synthetic Daraja success callback
	cbJSON := GenerateSuccessCallback(resp.CheckoutRequestID, 250.00, "254712345678", "WS87654321")
	cb, err := service.ProcessCallback(ctx, cbJSON)
	if err != nil {
		t.Fatalf("process callback failed: %v", err)
	}

	if !cb.Success {
		t.Errorf("expected callback success to be true")
	}
	if cb.MpesaReceipt != "WS87654321" {
		t.Errorf("expected receipt 'WS87654321', got %q", cb.MpesaReceipt)
	}

	// Verify user balance was incremented
	bal, err := service.GetBalance(ctx, "user-chris-01")
	if err != nil {
		t.Fatalf("fetching balance: %v", err)
	}
	if bal != 250.00 {
		t.Errorf("expected balance 250.00 KES, got %.2f KES", bal)
	}

	// Verify transaction status was updated
	tx, _ := repo.GetTransaction(ctx, resp.CheckoutRequestID)
	if tx.Status != "completed" || tx.MpesaReceipt != "WS87654321" {
		t.Errorf("unexpected updated transaction: %+v", tx)
	}
}

func TestProcessCallback_CancelledByUser(t *testing.T) {
	mockClient := NewMockMpesaClient()
	repo := NewMemoryBillingRepository()
	service := NewService(mockClient, repo, nil)

	ctx := context.Background()
	resp, _ := service.InitiateTopup(ctx, "user-chris-01", "0712345678", 100.00)

	// User pressed cancel on M-Pesa phone prompt (ResultCode 1032)
	cbJSON := GenerateFailedCallback(resp.CheckoutRequestID, 1032, "Request cancelled by user")
	cb, err := service.ProcessCallback(ctx, cbJSON)
	if err != nil {
		t.Fatalf("process callback failed: %v", err)
	}

	if cb.Success {
		t.Errorf("expected callback success to be false")
	}

	// Balance should remain 0
	bal, _ := service.GetBalance(ctx, "user-chris-01")
	if bal != 0.00 {
		t.Errorf("expected balance 0.00 KES, got %.2f KES", bal)
	}

	// Transaction should be marked cancelled
	tx, _ := repo.GetTransaction(ctx, resp.CheckoutRequestID)
	if tx.Status != "cancelled" || tx.ResultCode != 1032 {
		t.Errorf("unexpected transaction status: %+v", tx)
	}
}

func TestChargeBuildUsage(t *testing.T) {
	mockClient := NewMockMpesaClient()
	repo := NewMemoryBillingRepository()
	service := NewService(mockClient, repo, nil)

	ctx := context.Background()
	_, _ = repo.AddCredits(ctx, "user-chris-01", 100.00) // Initial 100.00 KES balance

	// 2 minutes 15 seconds -> rounds up to 3 minutes = 30.00 KES
	charge, err := service.ChargeBuildUsage(ctx, "user-chris-01", 135*time.Second)
	if err != nil {
		t.Fatalf("unexpected charge error: %v", err)
	}
	if charge != 30.00 {
		t.Errorf("expected charge 30.00 KES, got %.2f KES", charge)
	}

	rem, _ := service.GetBalance(ctx, "user-chris-01")
	if rem != 70.00 {
		t.Errorf("expected remaining balance 70.00 KES, got %.2f KES", rem)
	}

	// Attempting to charge 80.00 KES should fail (8 minutes = 80 KES > 70 KES)
	_, err = service.ChargeBuildUsage(ctx, "user-chris-01", 8*time.Minute)
	if err == nil || !strings.Contains(err.Error(), "insufficient credits") {
		t.Errorf("expected insufficient credits error, got: %v", err)
	}
}

