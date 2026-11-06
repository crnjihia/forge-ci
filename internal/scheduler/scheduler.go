// path: internal/scheduler/scheduler.go
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/crnji/forge-ci/internal/dag"
	"github.com/crnji/forge-ci/internal/executor"
	"github.com/crnji/forge-ci/internal/pipeline"
	"golang.org/x/sync/errgroup"
)

// StepStatus represents the lifecycle state of a pipeline step.
type StepStatus string

const (
	StatusPending   StepStatus = "pending"
	StatusRunning   StepStatus = "running"
	StatusSuccess   StepStatus = "success"
	StatusFailed    StepStatus = "failure"
	StatusSkipped   StepStatus = "skipped"
	StatusCancelled StepStatus = "cancelled"
)

// StepExecutionResult captures the outcome and metadata for an individual step.
type StepExecutionResult struct {
	StepID      string
	Status      StepStatus
	ExitCode    int
	Duration    time.Duration
	StartedAt   time.Time
	CompletedAt time.Time
	Error       error
}

// PipelineRunResult represents the aggregated execution report for the full pipeline.
type PipelineRunResult struct {
	RunID       string
	Status      StepStatus
	StartedAt   time.Time
	CompletedAt time.Time
	Duration    time.Duration
	StepResults map[string]*StepExecutionResult
	Error       error
}

// StepRunner abstracts container execution for individual steps.
type StepRunner interface {
	RunStep(ctx context.Context, cfg executor.StepExecutionConfig) (*executor.StepResult, error)
}

// SchedulerOptions configures the parallel step scheduler.
type SchedulerOptions struct {
	RunID           string
	WorkspaceDir    string
	GlobalEnv       map[string]string
	MaxConcurrency  int
	FailFast        bool
	ContinueOnError bool
	Logger          *slog.Logger
	OnStepStart     func(stepID string)
	OnStepFinish    func(stepID string, res *StepExecutionResult)
}

// Scheduler coordinates concurrent step execution according to a dependency DAG.
type Scheduler struct {
	pipeline *pipeline.Pipeline
	graph    *dag.Graph
	runner   StepRunner
	opts     SchedulerOptions
	logger   *slog.Logger

	mu             sync.RWMutex
	status         map[string]StepStatus
	results        map[string]*StepExecutionResult
	unresolvedDeps map[string]int
}

// New creates a new parallel scheduler instance.
func New(p *pipeline.Pipeline, g *dag.Graph, runner StepRunner, opts SchedulerOptions) *Scheduler {
	if opts.MaxConcurrency <= 0 {
		opts.MaxConcurrency = 4
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	s := &Scheduler{
		pipeline:       p,
		graph:          g,
		runner:         runner,
		opts:           opts,
		logger:         logger,
		status:         make(map[string]StepStatus),
		results:        make(map[string]*StepExecutionResult),
		unresolvedDeps: make(map[string]int),
	}

	for _, id := range g.StepIDs() {
		s.status[id] = StatusPending
		s.unresolvedDeps[id] = len(g.Dependencies(id))
		s.results[id] = &StepExecutionResult{
			StepID: id,
			Status: StatusPending,
		}
	}

	return s
}

// Execute runs all pipeline steps adhering to concurrency caps and dependency constraints.
func (s *Scheduler) Execute(parentCtx context.Context) (*PipelineRunResult, error) {
	startedAt := time.Now()
	allStepIDs := s.graph.StepIDs()
	totalSteps := len(allStepIDs)

	runResult := &PipelineRunResult{
		RunID:       s.opts.RunID,
		Status:      StatusRunning,
		StartedAt:   startedAt,
		StepResults: make(map[string]*StepExecutionResult, totalSteps),
	}

	if totalSteps == 0 {
		runResult.Status = StatusSuccess
		runResult.CompletedAt = time.Now()
		runResult.Duration = runResult.CompletedAt.Sub(startedAt)
		return runResult, nil
	}

	// Semaphore bounds concurrent goroutines
	sem := make(chan struct{}, s.opts.MaxConcurrency)
	readyCh := make(chan string, totalSteps)

	// Context for fail-fast cancellation
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	g, groupCtx := errgroup.WithContext(ctx)

	// Enqueue initial root steps (in-degree 0)
	s.mu.Lock()
	for _, id := range allStepIDs {
		if s.unresolvedDeps[id] == 0 {
			readyCh <- id
		}
	}
	s.mu.Unlock()

	var doneWg sync.WaitGroup
	doneWg.Add(totalSteps)

	// Dispatcher loop
	go func() {
		for {
			select {
			case <-groupCtx.Done():
				return
			case stepID, ok := <-readyCh:
				if !ok {
					return
				}

				// Check if step was already skipped or cancelled
				s.mu.RLock()
				currentStatus := s.status[stepID]
				s.mu.RUnlock()

				if currentStatus == StatusSkipped || currentStatus == StatusCancelled {
					doneWg.Done()
					continue
				}

				g.Go(func() error {
					defer doneWg.Done()

					// Acquire concurrency slot
					select {
					case sem <- struct{}{}:
					case <-groupCtx.Done():
						s.markStepTerminated(stepID, StatusCancelled, groupCtx.Err())
						return groupCtx.Err()
					}
					defer func() { <-sem }()

					return s.executeStep(groupCtx, stepID, readyCh, cancel)
				})
			}
		}
	}()

	// Wait until all steps have reported completion, skipping, or cancellation
	doneCh := make(chan struct{})
	go func() {
		doneWg.Wait()
		close(doneCh)
	}()

	var executionErr error
	select {
	case <-doneCh:
		// Normal completion of all steps
	case <-groupCtx.Done():
		executionErr = groupCtx.Err()
	}

	// Wait for any remaining errgroup goroutines
	if egErr := g.Wait(); egErr != nil && executionErr == nil {
		executionErr = egErr
	}

	completedAt := time.Now()
	runResult.CompletedAt = completedAt
	runResult.Duration = completedAt.Sub(startedAt)

	// Collect final statuses and determine overall run status
	s.mu.RLock()
	hasFailure := false
	hasCancelled := false

	for id, res := range s.results {
		runResult.StepResults[id] = res
		if s.status[id] == StatusPending {
			s.status[id] = StatusCancelled
			res.Status = StatusCancelled
		}
		if res.Status == StatusFailed {
			hasFailure = true
		}
		if res.Status == StatusCancelled {
			hasCancelled = true
		}
	}
	s.mu.RUnlock()

	if hasFailure {
		runResult.Status = StatusFailed
		if executionErr == nil {
			executionErr = fmt.Errorf("one or more steps failed")
		}
	} else if hasCancelled {
		runResult.Status = StatusCancelled
	} else {
		runResult.Status = StatusSuccess
	}

	runResult.Error = executionErr
	return runResult, executionErr
}

// executeStep executes a single step and propagates downstream completions or skips.
func (s *Scheduler) executeStep(ctx context.Context, stepID string, readyCh chan<- string, cancelFailFast context.CancelFunc) error {
	stepDef, exists := s.graph.Step(stepID)
	if !exists {
		return fmt.Errorf("step %q not found in graph", stepID)
	}

	stepStartedAt := time.Now()
	s.mu.Lock()
	s.status[stepID] = StatusRunning
	s.results[stepID].Status = StatusRunning
	s.results[stepID].StartedAt = stepStartedAt
	s.mu.Unlock()

	if s.opts.OnStepStart != nil {
		s.opts.OnStepStart(stepID)
	}

	cfg := executor.StepExecutionConfig{
		RunID:        s.opts.RunID,
		Step:         *stepDef,
		WorkspaceDir: s.opts.WorkspaceDir,
		GlobalEnv:    s.opts.GlobalEnv,
	}

	stepRes, err := s.runner.RunStep(ctx, cfg)
	stepCompletedAt := time.Now()

	s.mu.Lock()

	res := s.results[stepID]
	res.CompletedAt = stepCompletedAt
	res.Duration = stepCompletedAt.Sub(stepStartedAt)
	if stepRes != nil {
		res.ExitCode = stepRes.ExitCode
	}

	isSuccess := err == nil && (stepRes == nil || stepRes.ExitCode == 0)
	allowContinue := stepDef.ContinueOnError || s.opts.ContinueOnError
	var toEnqueue []string

	if isSuccess {
		res.Status = StatusSuccess
		s.status[stepID] = StatusSuccess

		// Unlock downstream dependents
		for _, dep := range s.graph.Dependents(stepID) {
			s.unresolvedDeps[dep]--
			if s.unresolvedDeps[dep] == 0 && s.status[dep] == StatusPending {
				toEnqueue = append(toEnqueue, dep)
			}
		}
	} else {
		res.Status = StatusFailed
		res.Error = err
		s.status[stepID] = StatusFailed

		if allowContinue {
			s.logger.Warn("step failed but continue-on-error is enabled", "step", stepID, "error", err)
			for _, dep := range s.graph.Dependents(stepID) {
				s.unresolvedDeps[dep]--
				if s.unresolvedDeps[dep] == 0 && s.status[dep] == StatusPending {
					toEnqueue = append(toEnqueue, dep)
				}
			}
		} else {
			if s.opts.FailFast {
				s.logger.Error("step failed with fail-fast enabled; aborting pipeline", "step", stepID, "error", err)
				s.mu.Unlock()
				cancelFailFast()
				return fmt.Errorf("step %q failed: %w", stepID, err)
			}

			// Propagate skipped status to all downstream descendants
			descendants, _ := s.graph.Descendants(stepID)
			for _, desc := range descendants {
				if s.status[desc] == StatusPending {
					s.status[desc] = StatusSkipped
					descRes := s.results[desc]
					descRes.Status = StatusSkipped
					descRes.Error = fmt.Errorf("skipped due to failure in ancestor step %q", stepID)
					if s.opts.OnStepFinish != nil {
						s.opts.OnStepFinish(desc, descRes)
					}
					toEnqueue = append(toEnqueue, desc)
				}
			}
		}
	}

	if s.opts.OnStepFinish != nil {
		s.opts.OnStepFinish(stepID, res)
	}

	s.mu.Unlock()

	for _, id := range toEnqueue {
		readyCh <- id
	}

	return nil
}

// markStepTerminated records a step as cancelled or skipped.
func (s *Scheduler) markStepTerminated(stepID string, status StepStatus, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.status[stepID] = status
	res := s.results[stepID]
	res.Status = status
	res.Error = err
	res.CompletedAt = time.Now()

	if s.opts.OnStepFinish != nil {
		s.opts.OnStepFinish(stepID, res)
	}
}
