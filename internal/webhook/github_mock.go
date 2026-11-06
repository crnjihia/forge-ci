// path: internal/webhook/github_mock.go
package webhook

import (
	"context"
	"fmt"
	"sync"
)

// MockGitHubReporter provides an in-memory double for testing commit status reporting.
type MockGitHubReporter struct {
	mu          sync.RWMutex
	statuses    map[string][]CommitStatus
	injectedErr error
}

// NewMockGitHubReporter creates a mock status reporter instance.
func NewMockGitHubReporter() *MockGitHubReporter {
	return &MockGitHubReporter{
		statuses: make(map[string][]CommitStatus),
	}
}

// SetError allows injecting errors for testing failure handling.
func (m *MockGitHubReporter) SetError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.injectedErr = err
}

// PostCommitStatus records the status update in memory.
func (m *MockGitHubReporter) PostCommitStatus(ctx context.Context, ownerRepo, sha string, status CommitStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.injectedErr != nil {
		return m.injectedErr
	}

	key := fmt.Sprintf("%s@%s", ownerRepo, sha)
	m.statuses[key] = append(m.statuses[key], status)
	return nil
}

// StatusesFor returns all recorded status updates for a repository commit SHA.
func (m *MockGitHubReporter) StatusesFor(ownerRepo, sha string) []CommitStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := fmt.Sprintf("%s@%s", ownerRepo, sha)
	out := make([]CommitStatus, len(m.statuses[key]))
	copy(out, m.statuses[key])
	return out
}

// TotalCalls returns the total count of status post requests handled.
func (m *MockGitHubReporter) TotalCalls() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := 0
	for _, slice := range m.statuses {
		total += len(slice)
	}
	return total
}

