// path: internal/notify/africas_talking_mock.go
package notify

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// SentMessageRecord represents a recorded SMS delivery in the mock notifier.
type SentMessageRecord struct {
	To         string
	Message    string
	Alert      *BuildAlert
	SentAt     time.Time
	MessageID  string
	Cost       string
	Status     string
	StatusCode int
}

// MockNotifier provides an in-memory, thread-safe test implementation of Notifier.
type MockNotifier struct {
	mu            sync.RWMutex
	messages      []SentMessageRecord
	simulatedErr  error
	simulatedCost string
	failNumbers   map[string]bool
	counter       int
}

// NewMockNotifier initializes a fresh MockNotifier for offline testing.
func NewMockNotifier() *MockNotifier {
	return &MockNotifier{
		messages:      make([]SentMessageRecord, 0),
		simulatedCost: "KES 0.8000",
		failNumbers:   make(map[string]bool),
	}
}

// SendSMS records the outgoing message and returns a simulated success response.
func (m *MockNotifier) SendSMS(ctx context.Context, to string, message string) (*SMSResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	normalizedTo, err := NormalizeKenyanPhoneE164(to)
	if err != nil {
		return nil, fmt.Errorf("invalid recipient phone: %w", err)
	}

	trimmedMsg := strings.TrimSpace(message)
	if trimmedMsg == "" {
		return nil, fmt.Errorf("message content cannot be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.simulatedErr != nil {
		return nil, m.simulatedErr
	}

	m.counter++
	msgID := fmt.Sprintf("ATXid_mock_%d", m.counter)

	if m.failNumbers[normalizedTo] {
		rec := SentMessageRecord{
			To:         normalizedTo,
			Message:    trimmedMsg,
			SentAt:     time.Now(),
			MessageID:  msgID,
			Cost:       "KES 0.0000",
			Status:     "Failed",
			StatusCode: 102,
		}
		m.messages = append(m.messages, rec)

		var resp SMSResponse
		resp.SMSMessageData.Message = "Sent to 0/1 Total Cost: KES 0.0000"
		resp.SMSMessageData.Recipients = []RecipientResult{
			{
				StatusCode: 102,
				Number:     normalizedTo,
				Status:     "Failed",
				Cost:       "KES 0.0000",
				MessageID:  msgID,
			},
		}
		return &resp, nil
	}

	rec := SentMessageRecord{
		To:         normalizedTo,
		Message:    trimmedMsg,
		SentAt:     time.Now(),
		MessageID:  msgID,
		Cost:       m.simulatedCost,
		Status:     "Success",
		StatusCode: 101,
	}
	m.messages = append(m.messages, rec)

	var resp SMSResponse
	resp.SMSMessageData.Message = fmt.Sprintf("Sent to 1/1 Total Cost: %s", m.simulatedCost)
	resp.SMSMessageData.Recipients = []RecipientResult{
		{
			StatusCode: 101,
			Number:     normalizedTo,
			Status:     "Success",
			Cost:       m.simulatedCost,
			MessageID:  msgID,
		},
	}

	return &resp, nil
}

// SendBuildAlert formats a bilingual SMS notification and records it.
func (m *MockNotifier) SendBuildAlert(ctx context.Context, to string, alert BuildAlert) (*SMSResponse, error) {
	msg := FormatBuildMessage(alert)
	resp, err := m.SendSMS(ctx, to, msg)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.messages) > 0 {
		// Attach alert metadata to the recorded entry
		alertCopy := alert
		m.messages[len(m.messages)-1].Alert = &alertCopy
	}

	return resp, nil
}

// GetSentMessages returns a thread-safe copy of all recorded messages.
func (m *MockNotifier) GetSentMessages() []SentMessageRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]SentMessageRecord, len(m.messages))
	copy(out, m.messages)
	return out
}

// GetLastMessage returns the most recent recorded message, or nil if none sent.
func (m *MockNotifier) GetLastMessage() *SentMessageRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.messages) == 0 {
		return nil
	}
	rec := m.messages[len(m.messages)-1]
	return &rec
}

// Count returns the number of recorded messages.
func (m *MockNotifier) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.messages)
}

// Reset clears recorded messages, counters, and failure simulations.
func (m *MockNotifier) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.messages = make([]SentMessageRecord, 0)
	m.simulatedErr = nil
	m.failNumbers = make(map[string]bool)
	m.counter = 0
}

// SetSimulatedError configures an error to return on future Send calls.
func (m *MockNotifier) SetSimulatedError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.simulatedErr = err
}

// SetFailNumber registers a phone number to simulate delivery failure.
func (m *MockNotifier) SetFailNumber(phone string, fail bool) {
	normalized, err := NormalizeKenyanPhoneE164(phone)
	if err != nil {
		normalized = phone
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if fail {
		m.failNumbers[normalized] = true
	} else {
		delete(m.failNumbers, normalized)
	}
}

// SetSimulatedCost overrides the cost string returned on simulated success.
func (m *MockNotifier) SetSimulatedCost(cost string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.simulatedCost = cost
}

