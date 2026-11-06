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

// StepSummary represents basic step information for historical replay.
type StepSummary struct {
	StepID     string `json:"step_id"`
	Name       string `json:"name"`
	Command    string `json:"command"`
	Status     string `json:"status"`
	ExitCode   *int   `json:"exit_code,omitempty"`
	DurationMs int64  `json:"duration_ms"`
}

// RunHistoryProvider retrieves step execution history for a run.
type RunHistoryProvider interface {
	GetRunSteps(ctx context.Context, runID string) ([]StepSummary, error)
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

// DefaultMockPubSub is the global in-memory pub/sub broker used in offline/mock mode.
var DefaultMockPubSub = NewMockRedis()

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

func (m *MockRedisPubSub) Unsubscribe(channel string, ch chan string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	subs := m.subscribers[channel]
	for i, c := range subs {
		if c == ch {
			m.subscribers[channel] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
}

func (m *MockRedisPubSub) Published(channel string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, len(m.published[channel]))
	copy(out, m.published[channel])
	return out
}
