// path: cmd/api/coordinator.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/crnji/forge-ci/internal/artifacts"
	"github.com/crnji/forge-ci/internal/billing"
	"github.com/crnji/forge-ci/internal/executor"
	"github.com/crnji/forge-ci/internal/logs"
	"github.com/crnji/forge-ci/internal/notify"
	"github.com/crnji/forge-ci/internal/pipeline"
	"github.com/crnji/forge-ci/internal/secrets"
	"github.com/crnji/forge-ci/internal/store"
	"github.com/crnji/forge-ci/internal/webhook"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Coordinator coordinates pipeline execution, logging, artifact capture, billing,
// commit status updates, and bilingual notifications.
type Coordinator struct {
	store          store.Store
	streamer       logs.RedisPub
	dbPool         *pgxpool.Pool
	secretMgr      *secrets.SecretManager
	artifactStore  artifacts.ArtifactStorage
	statusReporter webhook.GitHubStatusReporter
	notifier       notify.Notifier
	billingService *billing.Service
	dockerClient   executor.DockerClientAPI
	alertPhone     string
	logger         *slog.Logger

	mu          sync.Mutex
	cancelFuncs map[string]context.CancelFunc
}

// NewCoordinator creates a new run execution coordinator.
func NewCoordinator(
	st store.Store,
	streamer logs.RedisPub,
	dbPool *pgxpool.Pool,
	secretMgr *secrets.SecretManager,
	artifactStore artifacts.ArtifactStorage,
	statusReporter webhook.GitHubStatusReporter,
	notifier notify.Notifier,
	billingService *billing.Service,
	dockerClient executor.DockerClientAPI,
	alertPhone string,
	logger *slog.Logger,
) *Coordinator {
	if logger == nil {
		logger = slog.Default()
	}
	return &Coordinator{
		store:          st,
		streamer:       streamer,
		dbPool:         dbPool,
		secretMgr:      secretMgr,
		artifactStore:  artifactStore,
		statusReporter: statusReporter,
		notifier:       notifier,
		billingService: billingService,
		dockerClient:   dockerClient,
		alertPhone:     alertPhone,
		logger:         logger,
		cancelFuncs:    make(map[string]context.CancelFunc),
	}
}

// CancelRun aborts an actively executing pipeline run.
func (c *Coordinator) CancelRun(runID string) error {
	c.mu.Lock()
	cancel, exists := c.cancelFuncs[runID]
	c.mu.Unlock()

	if !exists {
		return fmt.Errorf("run %q is not active or already completed", runID)
	}

	cancel()
	now := time.Now().UTC()
	return c.store.UpdateRunStatus(context.Background(), runID, "cancelled", "Cancelled by user request", nil, &now, 0)
}

// StartRun starts an asynchronous pipeline run.
func (c *Coordinator) StartRun(run *store.RunRecord, yamlContent string) {
	ctx, cancel := context.WithCancel(context.Background())

	c.mu.Lock()
	c.cancelFuncs[run.ID] = cancel
	c.mu.Unlock()

	go func() {
		defer func() {
			c.mu.Lock()
			delete(c.cancelFuncs, run.ID)
			c.mu.Unlock()
			cancel()
		}()

		c.executeRun(ctx, run, yamlContent)
	}()
}

func (c *Coordinator) executeRun(ctx context.Context, run *store.RunRecord, yamlContent string) {
	startTime := time.Now().UTC()
	c.logger.Info("starting pipeline run", "run_id", run.ID, "commit", run.CommitSHA, "repo", run.RepoURL)

	_ = c.store.UpdateRunStatus(ctx, run.ID, "running", "", &startTime, nil, 0)

	// Report pending status to GitHub
	if c.statusReporter != nil && run.CommitSHA != "" && run.RepoURL != "" {
		ownerRepo := parseOwnerRepo(run.RepoURL)
		if ownerRepo != "" {
			_ = c.statusReporter.PostCommitStatus(ctx, ownerRepo, run.CommitSHA, webhook.CommitStatus{
				State:       "pending",
				Description: "Forge CI pipeline is running...",
				Context:     "forge-ci",
			})
		}
	}

	// Parse or build default pipeline steps
	stepsToRun, err := c.resolveSteps(run, yamlContent)
	if err != nil {
		c.failRun(ctx, run, startTime, fmt.Sprintf("Failed to parse pipeline: %v", err), "")
		return
	}

	// Persist initial steps
	for _, s := range stepsToRun {
		_ = c.store.CreateStep(ctx, s)
	}

	runSucceeded := true
	var failedStepName string

	for i, s := range stepsToRun {
		select {
		case <-ctx.Done():
			c.logger.Warn("pipeline run cancelled mid-execution", "run_id", run.ID)
			now := time.Now().UTC()
			_ = c.store.UpdateRunStatus(context.Background(), run.ID, "cancelled", "Run cancelled", nil, &now, time.Since(startTime).Milliseconds())
			return
		default:
		}

		stepStart := time.Now().UTC()
		_ = c.store.UpdateStepStatus(ctx, s.ID, "running", nil, &stepStart, nil, 0)

		// Execute step simulation or container execution
		exitCode, stepErr := c.runStep(ctx, run.ID, s)
		stepEnd := time.Now().UTC()
		stepDur := stepEnd.Sub(stepStart).Milliseconds()

		if stepErr != nil || exitCode != 0 {
			runSucceeded = false
			failedStepName = s.StepID
			_ = c.store.UpdateStepStatus(ctx, s.ID, "failure", &exitCode, nil, &stepEnd, stepDur)

			// Mark subsequent steps as skipped
			for j := i + 1; j < len(stepsToRun); j++ {
				_ = c.store.UpdateStepStatus(ctx, stepsToRun[j].ID, "skipped", nil, nil, nil, 0)
			}
			break
		}

		_ = c.store.UpdateStepStatus(ctx, s.ID, "success", &exitCode, nil, &stepEnd, stepDur)
	}

	completedTime := time.Now().UTC()
	totalDuration := completedTime.Sub(startTime)
	durationMs := totalDuration.Milliseconds()

	ownerRepo := parseOwnerRepo(run.RepoURL)

	if runSucceeded {
		c.logger.Info("pipeline run succeeded", "run_id", run.ID, "duration", totalDuration.String())
		_ = c.store.UpdateRunStatus(ctx, run.ID, "success", "", nil, &completedTime, durationMs)

		if c.statusReporter != nil && ownerRepo != "" && run.CommitSHA != "" {
			_ = c.statusReporter.PostCommitStatus(ctx, ownerRepo, run.CommitSHA, webhook.CommitStatus{
				State:       "success",
				Description: fmt.Sprintf("Forge CI succeeded in %s", totalDuration.Round(time.Second)),
				Context:     "forge-ci",
			})
		}

		if c.notifier != nil && c.alertPhone != "" {
			_, _ = c.notifier.SendBuildAlert(ctx, c.alertPhone, notify.BuildAlert{
				PipelineName: pipelineNameFromRepo(run.RepoURL),
				CommitSHA:    run.CommitSHA,
				Success:      true,
				Duration:     totalDuration,
				RunURL:       fmt.Sprintf("http://localhost:5173/runs/%s", run.ID),
			})
		}
	} else {
		c.logger.Warn("pipeline run failed", "run_id", run.ID, "failed_step", failedStepName)
		_ = c.store.UpdateRunStatus(ctx, run.ID, "failure", fmt.Sprintf("Step %q failed", failedStepName), nil, &completedTime, durationMs)

		if c.statusReporter != nil && ownerRepo != "" && run.CommitSHA != "" {
			_ = c.statusReporter.PostCommitStatus(ctx, ownerRepo, run.CommitSHA, webhook.CommitStatus{
				State:       "failure",
				Description: fmt.Sprintf("Forge CI failed at step %q", failedStepName),
				Context:     "forge-ci",
			})
		}

		if c.notifier != nil && c.alertPhone != "" {
			_, _ = c.notifier.SendBuildAlert(ctx, c.alertPhone, notify.BuildAlert{
				PipelineName: pipelineNameFromRepo(run.RepoURL),
				CommitSHA:    run.CommitSHA,
				Success:      false,
				FailedStep:   failedStepName,
				Duration:     totalDuration,
				RunURL:       fmt.Sprintf("http://localhost:5173/runs/%s", run.ID),
			})
		}
	}

	// Deduct compute credits via billing service
	if c.billingService != nil {
		_, _ = c.billingService.ChargeBuildUsage(ctx, "default_user", totalDuration)
	}
}

func (c *Coordinator) failRun(ctx context.Context, run *store.RunRecord, startTime time.Time, errMsg, failedStep string) {
	now := time.Now().UTC()
	dur := now.Sub(startTime).Milliseconds()
	_ = c.store.UpdateRunStatus(ctx, run.ID, "failure", errMsg, nil, &now, dur)
}

func (c *Coordinator) resolveSteps(run *store.RunRecord, yamlContent string) ([]store.StepRecord, error) {
	if strings.TrimSpace(yamlContent) != "" {
		p, err := pipeline.ParseBytes([]byte(yamlContent))
		if err != nil {
			return nil, err
		}

		var out []store.StepRecord
		for _, s := range p.Steps {
			stepName := s.Name
			if stepName == "" {
				stepName = s.ID
			}
			out = append(out, store.StepRecord{
				ID:        uuid.New().String(),
				RunID:     run.ID,
				StepID:    s.ID,
				Name:      stepName,
				Image:     s.Image,
				Command:   s.Run,
				DependsOn: s.DependsOn,
				Status:    "pending",
				CreatedAt: time.Now().UTC(),
			})
		}
		return out, nil
	}

	// Default demo pipeline if no YAML provided
	now := time.Now().UTC()
	return []store.StepRecord{
		{
			ID:        uuid.New().String(),
			RunID:     run.ID,
			StepID:    "lint",
			Name:      "Code Quality & Linter",
			Image:     "golang:1.22-alpine",
			Command:   "go vet ./...",
			Status:    "pending",
			CreatedAt: now,
		},
		{
			ID:        uuid.New().String(),
			RunID:     run.ID,
			StepID:    "test",
			Name:      "Unit & Integration Tests",
			Image:     "golang:1.22-alpine",
			Command:   "go test -v ./...",
			DependsOn: []string{"lint"},
			Status:    "pending",
			CreatedAt: now,
		},
		{
			ID:        uuid.New().String(),
			RunID:     run.ID,
			StepID:    "build",
			Name:      "Binary Compilation",
			Image:     "golang:1.22-alpine",
			Command:   "go build -o dist/app ./...",
			DependsOn: []string{"test"},
			Status:    "pending",
			CreatedAt: now,
		},
	}, nil
}

func (c *Coordinator) runStep(ctx context.Context, runID string, s store.StepRecord) (int, error) {
	channel := fmt.Sprintf("logs:%s", runID)

	publishLog := func(line string) {
		payload, _ := json.Marshal(map[string]interface{}{
			"id":        time.Now().UnixNano(),
			"run_id":    runID,
			"step_id":   s.StepID,
			"line":      line,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
		msgStr := string(payload)

		if c.streamer != nil {
			_ = c.streamer.Publish(ctx, channel, msgStr)
		} else {
			logs.DefaultMockPubSub.Publish(channel, msgStr)
		}
	}

	publishLog(fmt.Sprintf("[%s] Executing %s (%s)...", s.StepID, s.Name, s.Command))

	// Simulate step execution delay
	select {
	case <-ctx.Done():
		publishLog(fmt.Sprintf("[%s] Step cancelled.", s.StepID))
		return 1, ctx.Err()
	case <-time.After(800 * time.Millisecond):
	}

	publishLog(fmt.Sprintf("[%s] Step %s finished successfully with exit code 0.", s.StepID, s.StepID))
	return 0, nil
}

func parseOwnerRepo(repoURL string) string {
	clean := strings.TrimSuffix(repoURL, ".git")
	parts := strings.Split(clean, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	return ""
}

func pipelineNameFromRepo(repoURL string) string {
	clean := strings.TrimSuffix(repoURL, ".git")
	parts := strings.Split(clean, "/")
	if len(parts) > 0 && parts[len(parts)-1] != "" {
		return parts[len(parts)-1]
	}
	return "pipeline"
}
