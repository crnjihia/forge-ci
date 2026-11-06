// path: internal/store/memory.go
package store

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/crnji/forge-ci/internal/artifacts"
	"github.com/crnji/forge-ci/internal/billing"
	"github.com/crnji/forge-ci/internal/logs"
	"github.com/crnji/forge-ci/internal/secrets"
)

// MemoryStore provides a thread-safe in-memory store implementing Store,
// billing.BillingRepository, secrets.SecretStore, and logs.LogStore.
type MemoryStore struct {
	mu           sync.RWMutex
	runs         map[string]RunRecord
	steps        map[string][]StepRecord
	artifacts    map[string][]artifacts.ArtifactRecord
	balances     map[string]float64
	transactions map[string]billing.TransactionRecord
	secrets      map[string]secrets.SecretRecord
	logEntries   []logs.LogEntry
}

// NewMemoryStore initializes a fresh in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		runs:         make(map[string]RunRecord),
		steps:        make(map[string][]StepRecord),
		artifacts:    make(map[string][]artifacts.ArtifactRecord),
		balances:     make(map[string]float64),
		transactions: make(map[string]billing.TransactionRecord),
		secrets:      make(map[string]secrets.SecretRecord),
		logEntries:   make([]logs.LogEntry, 0),
	}
}

// ==============================================================================
// Store: Runs
// ==============================================================================

func (m *MemoryStore) CreateRun(ctx context.Context, run RunRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.runs[run.ID]; exists {
		return fmt.Errorf("run %q already exists", run.ID)
	}
	m.runs[run.ID] = run
	return nil
}

func (m *MemoryStore) GetRun(ctx context.Context, id string) (*RunRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	run, exists := m.runs[id]
	if !exists {
		return nil, fmt.Errorf("run %q not found", id)
	}

	runCopy := run
	if stepList, ok := m.steps[id]; ok {
		runCopy.Steps = make([]StepRecord, len(stepList))
		copy(runCopy.Steps, stepList)
	}
	return &runCopy, nil
}

func (m *MemoryStore) ListRuns(ctx context.Context, limit, offset int) ([]RunRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	all := make([]RunRecord, 0, len(m.runs))
	for _, run := range m.runs {
		runCopy := run
		if stepList, ok := m.steps[run.ID]; ok {
			runCopy.Steps = make([]StepRecord, len(stepList))
			copy(runCopy.Steps, stepList)
		}
		all = append(all, runCopy)
	}

	// Sort newest first
	sort.Slice(all, func(i, j int) bool {
		return all[i].CreatedAt.After(all[j].CreatedAt)
	})

	if offset >= len(all) {
		return []RunRecord{}, nil
	}
	end := offset + limit
	if limit <= 0 || end > len(all) {
		end = len(all)
	}

	return all[offset:end], nil
}

func (m *MemoryStore) UpdateRunStatus(ctx context.Context, id, status, errorMessage string, startedAt, completedAt *time.Time, durationMs int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	run, exists := m.runs[id]
	if !exists {
		return fmt.Errorf("run %q not found", id)
	}

	run.Status = status
	if errorMessage != "" {
		run.ErrorMessage = errorMessage
	}
	if startedAt != nil {
		run.StartedAt = startedAt
	}
	if completedAt != nil {
		run.CompletedAt = completedAt
	}
	if durationMs > 0 {
		run.DurationMs = durationMs
	}

	m.runs[id] = run
	return nil
}

// ==============================================================================
// Store: Steps
// ==============================================================================

func (m *MemoryStore) CreateStep(ctx context.Context, step StepRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.steps[step.RunID] = append(m.steps[step.RunID], step)
	return nil
}

func (m *MemoryStore) GetStepsByRunID(ctx context.Context, runID string) ([]StepRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	steps, ok := m.steps[runID]
	if !ok {
		return []StepRecord{}, nil
	}

	out := make([]StepRecord, len(steps))
	copy(out, steps)
	return out, nil
}

func (m *MemoryStore) UpdateStepStatus(ctx context.Context, id, status string, exitCode *int, startedAt, completedAt *time.Time, durationMs int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for runID, stepList := range m.steps {
		for i, s := range stepList {
			if s.ID == id || s.StepID == id {
				stepList[i].Status = status
				if exitCode != nil {
					stepList[i].ExitCode = exitCode
				}
				if startedAt != nil {
					stepList[i].StartedAt = startedAt
				}
				if completedAt != nil {
					stepList[i].CompletedAt = completedAt
				}
				if durationMs > 0 {
					stepList[i].DurationMs = durationMs
				}
				m.steps[runID] = stepList
				return nil
			}
		}
	}
	return fmt.Errorf("step %q not found", id)
}

// ==============================================================================
// Store: Artifacts
// ==============================================================================

func (m *MemoryStore) CreateArtifact(ctx context.Context, record artifacts.ArtifactRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.artifacts[record.RunID] = append(m.artifacts[record.RunID], record)
	return nil
}

func (m *MemoryStore) ListArtifactsByRun(ctx context.Context, runID string) ([]artifacts.ArtifactRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	records, ok := m.artifacts[runID]
	if !ok {
		return []artifacts.ArtifactRecord{}, nil
	}

	out := make([]artifacts.ArtifactRecord, len(records))
	copy(out, records)
	return out, nil
}

func (m *MemoryStore) GetArtifact(ctx context.Context, id string) (*artifacts.ArtifactRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, records := range m.artifacts {
		for _, rec := range records {
			if rec.ID == id {
				recCopy := rec
				return &recCopy, nil
			}
		}
	}
	return nil, fmt.Errorf("artifact %q not found", id)
}

// ==============================================================================
// billing.BillingRepository Implementation
// ==============================================================================

func (m *MemoryStore) GetBalance(ctx context.Context, userID string) (float64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.balances[userID], nil
}

func (m *MemoryStore) AddCredits(ctx context.Context, userID string, amountKES float64) (float64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.balances[userID] += amountKES
	return m.balances[userID], nil
}

func (m *MemoryStore) DeductCredits(ctx context.Context, userID string, amountKES float64) (float64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.balances[userID] < amountKES {
		return m.balances[userID], fmt.Errorf("insufficient credits: balance %.2f KES is below charge of %.2f KES", m.balances[userID], amountKES)
	}
	m.balances[userID] -= amountKES
	return m.balances[userID], nil
}

func (m *MemoryStore) CreateTransaction(ctx context.Context, tx billing.TransactionRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.transactions[tx.CheckoutRequestID] = tx
	return nil
}

func (m *MemoryStore) UpdateTransaction(ctx context.Context, checkoutID, status, receipt string, resultCode int, resultDesc string) error {
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

func (m *MemoryStore) GetTransaction(ctx context.Context, checkoutID string) (*billing.TransactionRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	tx, exists := m.transactions[checkoutID]
	if !exists {
		return nil, fmt.Errorf("transaction %q not found", checkoutID)
	}
	return &tx, nil
}

// ==============================================================================
// secrets.SecretStore Implementation
// ==============================================================================

func (m *MemoryStore) SaveSecret(ctx context.Context, record secrets.SecretRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.secrets[record.Name] = record
	return nil
}

func (m *MemoryStore) GetSecret(ctx context.Context, name string) (*secrets.SecretRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rec, exists := m.secrets[name]
	if !exists {
		return nil, fmt.Errorf("secret %q not found", name)
	}
	return &rec, nil
}

func (m *MemoryStore) DeleteSecret(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.secrets[name]; !exists {
		return fmt.Errorf("secret %q not found", name)
	}
	delete(m.secrets, name)
	return nil
}

func (m *MemoryStore) ListSecretNames(ctx context.Context) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.secrets))
	for name := range m.secrets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// ==============================================================================
// logs.LogStore Implementation
// ==============================================================================

func (m *MemoryStore) InsertLogBatch(ctx context.Context, entries []logs.LogEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logEntries = append(m.logEntries, entries...)
	return nil
}

