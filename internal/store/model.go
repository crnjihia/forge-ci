// path: internal/store/model.go
package store

import (
	"context"
	"time"

	"github.com/crnji/forge-ci/internal/artifacts"
)

// RunRecord represents a CI/CD pipeline execution in the database.
type RunRecord struct {
	ID           string       `json:"id"`
	RepoURL      string       `json:"repo_url"`
	CommitSHA    string       `json:"commit_sha"`
	Branch       string       `json:"branch"`
	Status       string       `json:"status"` // pending, running, success, failure, error, skipped, cancelled
	Trigger      string       `json:"trigger"` // manual, webhook
	ErrorMessage string       `json:"error_message,omitempty"`
	CreatedAt    time.Time    `json:"created_at"`
	StartedAt    *time.Time   `json:"started_at,omitempty"`
	CompletedAt  *time.Time   `json:"completed_at,omitempty"`
	DurationMs   int64        `json:"duration_ms"`
	Steps        []StepRecord `json:"steps,omitempty"`
}

// StepRecord represents an individual DAG container step in a pipeline execution.
type StepRecord struct {
	ID          string     `json:"id"`
	RunID       string     `json:"run_id"`
	StepID      string     `json:"step_id"`
	Name        string     `json:"name"`
	Image       string     `json:"image"`
	Command     string     `json:"command"`
	Status      string     `json:"status"` // pending, running, success, failure, skipped
	ExitCode    *int       `json:"exit_code,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	DurationMs  int64      `json:"duration_ms"`
	CreatedAt   time.Time  `json:"created_at"`
	DependsOn   []string   `json:"depends_on,omitempty"`
}

// Store abstracts database operations for CI runs, steps, and artifacts.
type Store interface {
	// Runs
	CreateRun(ctx context.Context, run RunRecord) error
	GetRun(ctx context.Context, id string) (*RunRecord, error)
	ListRuns(ctx context.Context, limit, offset int) ([]RunRecord, error)
	UpdateRunStatus(ctx context.Context, id, status, errorMessage string, startedAt, completedAt *time.Time, durationMs int64) error

	// Steps
	CreateStep(ctx context.Context, step StepRecord) error
	GetStepsByRunID(ctx context.Context, runID string) ([]StepRecord, error)
	UpdateStepStatus(ctx context.Context, id, status string, exitCode *int, startedAt, completedAt *time.Time, durationMs int64) error

	// Artifacts
	CreateArtifact(ctx context.Context, record artifacts.ArtifactRecord) error
	ListArtifactsByRun(ctx context.Context, runID string) ([]artifacts.ArtifactRecord, error)
	GetArtifact(ctx context.Context, id string) (*artifacts.ArtifactRecord, error)
}

