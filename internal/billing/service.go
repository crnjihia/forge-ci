// path: internal/billing/service.go
package billing

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"
)

// TransactionRecord represents a tracked M-Pesa payment attempt in the database.
type TransactionRecord struct {
	CheckoutRequestID string    `json:"checkout_request_id"`
	MerchantRequestID string    `json:"merchant_request_id"`
	UserID            string    `json:"user_id"`
	PhoneNumber       string    `json:"phone_number"`
	AmountKES         float64   `json:"amount_kes"`
	Status            string    `json:"status"` // pending, completed, failed, cancelled
	MpesaReceipt      string    `json:"mpesa_receipt,omitempty"`
	ResultCode        int       `json:"result_code"`
	ResultDesc        string    `json:"result_desc,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// BillingRepository abstracts database operations for credits and M-Pesa transactions.
type BillingRepository interface {
	GetBalance(ctx context.Context, userID string) (float64, error)
	AddCredits(ctx context.Context, userID string, amountKES float64) (float64, error)
	DeductCredits(ctx context.Context, userID string, amountKES float64) (float64, error)
	CreateTransaction(ctx context.Context, tx TransactionRecord) error
	UpdateTransaction(ctx context.Context, checkoutID, status, receipt string, resultCode int, resultDesc string) error
	GetTransaction(ctx context.Context, checkoutID string) (*TransactionRecord, error)
}

// MemoryBillingRepository provides an in-memory double for tests and offline runs.
type MemoryBillingRepository struct {
	mu           sync.RWMutex
	balances     map[string]float64
	transactions map[string]TransactionRecord
}

// NewMemoryBillingRepository creates an in-memory billing store.
func NewMemoryBillingRepository() *MemoryBillingRepository {
	return &MemoryBillingRepository{
		balances:     make(map[string]float64),
		transactions: make(map[string]TransactionRecord),
	}
}

func (m *MemoryBillingRepository) GetBalance(ctx context.Context, userID string) (float64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.balances[userID], nil
}

func (m *MemoryBillingRepository) AddCredits(ctx context.Context, userID string, amountKES float64) (float64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.balances[userID] += amountKES
	return m.balances[userID], nil
}

func (m *MemoryBillingRepository) DeductCredits(ctx context.Context, userID string, amountKES float64) (float64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.balances[userID] < amountKES {
		return m.balances[userID], fmt.Errorf("insufficient credits: balance %.2f KES is below charge of %.2f KES", m.balances[userID], amountKES)
	}
	m.balances[userID] -= amountKES
	return m.balances[userID], nil
}

func (m *MemoryBillingRepository) CreateTransaction(ctx context.Context, tx TransactionRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.transactions[tx.CheckoutRequestID] = tx
	return nil
}

func (m *MemoryBillingRepository) UpdateTransaction(ctx context.Context, checkoutID, status, receipt string, resultCode int, resultDesc string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	tx, exists := m.transactions[checkoutID]
	if !exists {
		return fmt.Errorf("transaction %q not found", checkoutID)
	}
	tx.Status = status
	tx.MpesaReceipt = receipt
	tx.ResultCode = resultCode
	tx.ResultDesc = resultDesc
	tx.UpdatedAt = time.Now().UTC()
	m.transactions[checkoutID] = tx
	return nil
}

func (m *MemoryBillingRepository) GetTransaction(ctx context.Context, checkoutID string) (*TransactionRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	tx, exists := m.transactions[checkoutID]
	if !exists {
		return nil, fmt.Errorf("transaction %q not found", checkoutID)
	}
	return &tx, nil
}

// Service coordinates top-ups, callbacks, credit balances, and metered build charges.
type Service struct {
	client            MpesaClient
	repo              BillingRepository
	ratePerMinuteKES  float64
	logger            *slog.Logger
}

// NewService creates a new billing business service.
func NewService(client MpesaClient, repo BillingRepository, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		client:           client,
		repo:             repo,
		ratePerMinuteKES: 10.00, // Standard 10.00 KES per build compute minute
		logger:           logger,
	}
}

// InitiateTopup starts an M-Pesa STK push and records a pending transaction.
func (s *Service) InitiateTopup(ctx context.Context, userID, phone string, amountKES float64) (*STKPushResponse, error) {
	if amountKES < 1.0 {
		return nil, fmt.Errorf("minimum top-up amount is 1.00 KES")
	}

	normPhone, err := NormalizeKenyanPhone(phone)
	if err != nil {
		return nil, err
	}

	stkResp, err := s.client.InitiateSTKPush(ctx, STKPushRequest{
		PhoneNumber: normPhone,
		AmountKES:   amountKES,
		AccountRef:  "ForgeCI-" + userID,
		Description: "Forge CI Compute Credits",
	})
	if err != nil {
		return nil, fmt.Errorf("initiating stk push: %w", err)
	}

	now := time.Now().UTC()
	tx := TransactionRecord{
		CheckoutRequestID: stkResp.CheckoutRequestID,
		MerchantRequestID: stkResp.MerchantRequestID,
		UserID:            userID,
		PhoneNumber:       normPhone,
		AmountKES:         amountKES,
		Status:            "pending",
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	if err := s.repo.CreateTransaction(ctx, tx); err != nil {
		return nil, fmt.Errorf("saving pending transaction: %w", err)
	}

	s.logger.Info("initiated m-pesa top-up",
		"user_id", userID,
		"phone", normPhone,
		"amount_kes", amountKES,
		"checkout_id", stkResp.CheckoutRequestID,
	)

	return stkResp, nil
}

// ProcessCallback parses the Daraja callback, updates the transaction, and credits the user account on success.
func (s *Service) ProcessCallback(ctx context.Context, rawBody []byte) (*ParsedCallback, error) {
	cb, err := ParseCallback(rawBody)
	if err != nil {
		return nil, fmt.Errorf("parsing m-pesa callback: %w", err)
	}

	tx, err := s.repo.GetTransaction(ctx, cb.CheckoutRequestID)
	if err != nil {
		return nil, fmt.Errorf("finding transaction for checkout ID %q: %w", cb.CheckoutRequestID, err)
	}

	status := "completed"
	if !cb.Success {
		status = "failed"
		if cb.ResultCode == 1032 {
			status = "cancelled"
		}
	}

	if err := s.repo.UpdateTransaction(ctx, cb.CheckoutRequestID, status, cb.MpesaReceipt, cb.ResultCode, cb.ResultDesc); err != nil {
		return nil, fmt.Errorf("updating transaction: %w", err)
	}

	if cb.Success {
		amount := cb.AmountKES
		if amount <= 0 {
			amount = tx.AmountKES
		}
		newBalance, creditErr := s.repo.AddCredits(ctx, tx.UserID, amount)
		if creditErr != nil {
			return nil, fmt.Errorf("crediting balance for user %q: %w", tx.UserID, creditErr)
		}

		s.logger.Info("m-pesa top-up completed successfully",
			"user_id", tx.UserID,
			"receipt", cb.MpesaReceipt,
			"amount_kes", amount,
			"new_balance_kes", newBalance,
		)
	} else {
		s.logger.Warn("m-pesa transaction failed or cancelled by user",
			"user_id", tx.UserID,
			"checkout_id", cb.CheckoutRequestID,
			"result_code", cb.ResultCode,
			"result_desc", cb.ResultDesc,
		)
	}

	return cb, nil
}

// ChargeBuildUsage debits compute credits based on build duration (rounded up to nearest minute).
func (s *Service) ChargeBuildUsage(ctx context.Context, userID string, duration time.Duration) (float64, error) {
	minutes := math.Ceil(duration.Minutes())
	if minutes < 1.0 {
		minutes = 1.0
	}
	chargeKES := minutes * s.ratePerMinuteKES

	newBalance, err := s.repo.DeductCredits(ctx, userID, chargeKES)
	if err != nil {
		return 0, err
	}

	s.logger.Info("debited compute usage",
		"user_id", userID,
		"minutes", minutes,
		"charge_kes", chargeKES,
		"remaining_balance_kes", newBalance,
	)

	return chargeKES, nil
}

// GetBalance returns the user's current compute credit balance in KES.
func (s *Service) GetBalance(ctx context.Context, userID string) (float64, error) {
	return s.repo.GetBalance(ctx, userID)
}

