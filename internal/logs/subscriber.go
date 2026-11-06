// path: internal/logs/subscriber.go
package logs

import (
	"context"
	"sync"
	"time"
)

// LogEntry represents an individual recorded log line.
type LogEntry struct {
	ID        int64     `json:"id"`
	RunID     string    `json:"run_id"`
	StepID    string    `json:"step_id"`
	Stream    string    `json:"stream"`
	Line      string    `json:"line"`
	Timestamp time.Time `json:"timestamp"`
}

// SecretMasker scrubs sensitive values from log strings.
type SecretMasker interface {
	MaskString(s string) string
}

// LogStore abstracts database log persistence and historical queries.
type LogStore interface {
	InsertBatch(ctx context.Context, entries []LogEntry) error
	QueryLogs(ctx context.Context, runID, stepID string, afterID int64, limit int) ([]LogEntry, error)
}

// MockRedisPubSub provides an in-memory Pub/Sub mechanism for testing and air-gapped runs.
type MockRedisPubSub struct {
	mu          sync.RWMutex
	subscribers map[string][]chan string
	published   map[string][]string
}

// NewMockRedis creates an in-memory pub/sub broker.
func NewMockRedis() *MockRedisPubSub {
	return &MockRedisPubSub{
		subscribers: make(map[string][]chan string),
		published:   make(map[string][]string),
	}
}

func (m *MockRedisPubSub) Publish(channel, message string) {
	m.mu.Lock()
	m.published[channel] = append(m.published[channel], message)
	subs := append([]chan string(nil), m.subscribers[channel]...)
	m.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- message:
		default:
		}
	}
}

func (m *MockRedisPubSub) Subscribe(channel string) chan string {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch := make(chan string, 100)
	m.subscribers[channel] = append(m.subscribers[channel], ch)
	return ch
}

func (m *MockRedisPubSub) Published(channel string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, len(m.published[channel]))
	copy(out, m.published[channel])
	return out
}
