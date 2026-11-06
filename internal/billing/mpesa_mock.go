// path: internal/billing/mpesa_mock.go
package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// MockMpesaClient provides an offline, in-memory Daraja adapter for tests and zero-credential environments.
type MockMpesaClient struct {
	mu          sync.RWMutex
	injectedErr error
	reqCounter  uint64
	requests    []STKPushRequest
}

// NewMockMpesaClient creates an in-memory mock client.
func NewMockMpesaClient() *MockMpesaClient {
	return &MockMpesaClient{
		requests: make([]STKPushRequest, 0),
	}
}

// SetError injects a simulated network or API failure.
func (m *MockMpesaClient) SetError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.injectedErr = err
}

// InitiateSTKPush records the request and generates a synthetic CheckoutRequestID.
func (m *MockMpesaClient) InitiateSTKPush(ctx context.Context, req STKPushRequest) (*STKPushResponse, error) {
	phone, err := NormalizeKenyanPhone(req.PhoneNumber)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.injectedErr != nil {
		return nil, m.injectedErr
	}

	req.PhoneNumber = phone
	m.requests = append(m.requests, req)

	seq := atomic.AddUint64(&m.reqCounter, 1)
	checkoutID := fmt.Sprintf("ws_CO_MOCK_%d_%04d", time.Now().Unix(), seq)
	merchantID := fmt.Sprintf("MOCK_MERCHANT_%04d", seq)

	return &STKPushResponse{
		MerchantRequestID: merchantID,
		CheckoutRequestID: checkoutID,
		ResponseCode:      "0",
		ResponseDesc:      "Success. Request accepted for processing",
		CustomerMessage:   "Success. Request accepted for processing",
	}, nil
}

// LastRequest returns the most recent STK push parameters received.
func (m *MockMpesaClient) LastRequest() *STKPushRequest {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.requests) == 0 {
		return nil
	}
	last := m.requests[len(m.requests)-1]
	return &last
}

// GenerateSuccessCallback crafts a synthetic successful STK push callback payload.
func GenerateSuccessCallback(checkoutID string, amount float64, phone, receipt string) []byte {
	if receipt == "" {
		receipt = "WS12345678" // Synthetic M-Pesa receipt code
	}
	if phone == "" {
		phone = "254708374149"
	}

	payload := STKCallbackPayload{}
	cb := &payload.Body.STKCallback
	cb.MerchantRequestID = "MOCK_MERCHANT_REQ"
	cb.CheckoutRequestID = checkoutID
	cb.ResultCode = 0
	cb.ResultDesc = "The service request is processed successfully."
	cb.CallbackMetadata = &CallbackMetadata{
		Item: []CallbackMetadataItem{
			{Name: "Amount", Value: amount},
			{Name: "MpesaReceiptNumber", Value: receipt},
			{Name: "TransactionDate", Value: float64(20261106120000)},
			{Name: "PhoneNumber", Value: phone},
		},
	}

	b, _ := json.Marshal(payload)
	return b
}

// GenerateFailedCallback crafts a synthetic failed STK callback (e.g. user cancelled prompt).
func GenerateFailedCallback(checkoutID string, resultCode int, desc string) []byte {
	if resultCode == 0 {
		resultCode = 1032 // Default: 1032 Request cancelled by user
	}
	if desc == "" {
		desc = "Request cancelled by user"
	}

	payload := STKCallbackPayload{}
	cb := &payload.Body.STKCallback
	cb.MerchantRequestID = "MOCK_MERCHANT_REQ"
	cb.CheckoutRequestID = checkoutID
	cb.ResultCode = resultCode
	cb.ResultDesc = desc

	b, _ := json.Marshal(payload)
	return b
}

