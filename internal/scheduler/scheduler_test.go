// path: internal/scheduler/scheduler_test.go
package scheduler

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/crnji/forge-ci/internal/dag"
	"github.com/crnji/forge-ci/internal/executor"
	"github.com/crnji/forge-ci/internal/pipeline"
)

// mockStepRunner implements StepRunner for concurrency and execution testing.
type mockStepRunner struct {
	mu           sync.Mutex
	delays       map[string]time.Duration
	exitCodes    map[string]int
	injectedErrs map[string]error

	activeCount int32
	maxSeen     int32
	executed    []string
}

func newMockStepRunner() *mockStepRunner {
	return &mockStepRunner{
		delays:       make(map[string]time.Duration),
		exitCodes:    make(map[string]int),
		injectedErrs: make(map[string]error),
		executed:     make([]string, 0),
	}
}

func (m *mockStepRunner) RunStep(ctx context.Context, cfg executor.StepExecutionConfig) (*executor.StepResult, error) {
	current := atomic.AddInt32(&m.activeCount, 1)
	defer atomic.AddInt32(&m.activeCount, -1)

	// Track peak concurrent steps
	for {
		max := atomic.LoadInt32(&m.maxSeen)
		if current <= max || atomic.CompareAndSwapInt32(&m.maxSeen, max, current) {
			break
		}
	}

	m.mu.Lock()
	m.executed = append(m.executed, cfg.Step.ID)
	delay := m.delays[cfg.Step.ID]
	exitCode := m.exitCodes[cfg.Step.ID]
	injectedErr := m.injectedErrs[cfg.Step.ID]
	m.mu.Unlock()

	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if injectedErr != nil {
		return &executor.StepResult{
			StepID:   cfg.Step.ID,
			ExitCode: 1,
			Error:    injectedErr,
		}, injectedErr
	}

	if exitCode != 0 {
		err := fmt.Errorf("exit code %d", exitCode)
		return &executor.StepResult{
			StepID:   cfg.Step.ID,
			ExitCode: exitCode,
			Error:    err,
		}, err
	}

	return &executor.StepResult{
		StepID:   cfg.Step.ID,
		ExitCode: 0,
	}, nil
}

func TestScheduler_ParallelDiamond(t *testing.T) {
	//       A
	//     /   \
	//    B     C   (B and C should execute concurrently)
	//     \   /
	//       D
	p := &pipeline.Pipeline{
		Name: "diamond",
		Steps: []pipeline.Step{
			{ID: "A", Image: "alpine", Run: "echo A"},
			{ID: "B", Image: "alpine", Run: "echo B", DependsOn: []string{"A"}},
			{ID: "C", Image: "alpine", Run: "echo C", DependsOn: []string{"A"}},
			{ID: "D", Image: "alpine", Run: "echo D", DependsOn: []string{"B", "C"}},
		},
	}

	g, err := dag.Build(p)
	if err != nil {
		t.Fatalf("building dag: %v", err)
	}

	runner := newMockStepRunner()
	runner.delays["B"] = 40 * time.Millisecond
	runner.delays["C"] = 40 * time.Millisecond

	sched := New(p, g, runner, SchedulerOptions{
		RunID:          "run-diamond",
		MaxConcurrency: 4,
	})

	startTime := time.Now()
	res, err := sched.Execute(context.Background())
	elapsed := time.Since(startTime)

	if err != nil {
		t.Fatalf("expected successful run, got error: %v", err)
	}
	if res.Status != StatusSuccess {
		t.Errorf("expected pipeline status %s, got %s", StatusSuccess, res.Status)
	}

	// Verify all steps succeeded
	for _, id := range []string{"A", "B", "C", "D"} {
		stepRes, ok := res.StepResults[id]
		if !ok || stepRes.Status != StatusSuccess {
			t.Errorf("step %q not successful: %+v", id, stepRes)
		}
	}

	// Because B and C run concurrently (~40ms total rather than 80ms sequential),
	// total elapsed time should be well under 120ms
	if elapsed > 150*time.Millisecond {
		t.Errorf("execution took %v, expected concurrent execution < 150ms", elapsed)
	}
}

func TestScheduler_BoundedConcurrency(t *testing.T) {
	// 6 independent steps with MaxConcurrency = 2
	steps := make([]pipeline.Step, 6)
	for i := 0; i < 6; i++ {
		steps[i] = pipeline.Step{
			ID:    fmt.Sprintf("step-%d", i),
			Image: "alpine",
			Run:   "echo test",
		}
	}

	p := &pipeline.Pipeline{
		Name:  "concurrency-bound",
		Steps: steps,
	}

	g, err := dag.Build(p)
	if err != nil {
		t.Fatalf("building dag: %v", err)
	}

	runner := newMockStepRunner()
	for i := 0; i < 6; i++ {
		runner.delays[fmt.Sprintf("step-%d", i)] = 25 * time.Millisecond
	}

	sched := New(p, g, runner, SchedulerOptions{
		RunID:          "run-bounded",
		MaxConcurrency: 2, // Strictly capped at 2
	})

	res, err := sched.Execute(context.Background())
	if err != nil {
		t.Fatalf("unexpected execution error: %v", err)
	}
	if res.Status != StatusSuccess {
		t.Errorf("expected status success, got %s", res.Status)
	}

	maxConcurrent := atomic.LoadInt32(&runner.maxSeen)
	if maxConcurrent > 2 {
		t.Errorf("expected max concurrency <= 2, saw %d", maxConcurrent)
	}
}

func TestScheduler_FailFast(t *testing.T) {
	p := &pipeline.Pipeline{
		Name: "fail-fast",
		Steps: []pipeline.Step{
			{ID: "A", Image: "alpine", Run: "echo A"},
			{ID: "B", Image: "alpine", Run: "echo B", DependsOn: []string{"A"}},
			{ID: "C", Image: "alpine", Run: "echo C", DependsOn: []string{"B"}},
		},
	}

	g, err := dag.Build(p)
	if err != nil {
		t.Fatalf("building dag: %v", err)
	}

	runner := newMockStepRunner()
	runner.exitCodes["B"] = 1 // Step B fails

	sched := New(p, g, runner, SchedulerOptions{
		RunID:    "run-fail-fast",
		FailFast: true,
	})

	res, err := sched.Execute(context.Background())
	if err == nil {
		t.Fatalf("expected error with fail-fast enabled, got nil")
	}
	if res.Status != StatusFailed {
		t.Errorf("expected pipeline status %s, got %s", StatusFailed, res.Status)
	}
	if res.StepResults["B"].Status != StatusFailed {
		t.Errorf("expected step B status %s, got %s", StatusFailed, res.StepResults["B"].Status)
	}
	if res.StepResults["C"].Status == StatusSuccess {
		t.Errorf("step C should not have succeeded")
	}
}

func TestScheduler_SkippedStepPropagation(t *testing.T) {
	// A -> B -> D
	// A -> C
	// B fails, FailFast = false
	// C should still succeed!
	// D should be marked Skipped!
	p := &pipeline.Pipeline{
		Name: "skip-propagation",
		Steps: []pipeline.Step{
			{ID: "A", Image: "alpine", Run: "echo A"},
			{ID: "B", Image: "alpine", Run: "echo B", DependsOn: []string{"A"}},
			{ID: "C", Image: "alpine", Run: "echo C", DependsOn: []string{"A"}},
			{ID: "D", Image: "alpine", Run: "echo D", DependsOn: []string{"B"}},
		},
	}

	g, err := dag.Build(p)
	if err != nil {
		t.Fatalf("building dag: %v", err)
	}

	runner := newMockStepRunner()
	runner.exitCodes["B"] = 1 // Step B fails

	sched := New(p, g, runner, SchedulerOptions{
		RunID:    "run-skip",
		FailFast: false, // Continue independent branches
	})

	res, _ := sched.Execute(context.Background())
	if res.Status != StatusFailed {
		t.Errorf("expected pipeline status %s, got %s", StatusFailed, res.Status)
	}

	if res.StepResults["A"].Status != StatusSuccess {
		t.Errorf("step A expected success, got %s", res.StepResults["A"].Status)
	}
	if res.StepResults["B"].Status != StatusFailed {
		t.Errorf("step B expected failure, got %s", res.StepResults["B"].Status)
	}
	if res.StepResults["C"].Status != StatusSuccess {
		t.Errorf("independent step C expected success, got %s", res.StepResults["C"].Status)
	}
	if res.StepResults["D"].Status != StatusSkipped {
		t.Errorf("dependent step D expected skipped, got %s", res.StepResults["D"].Status)
	}
}

func TestScheduler_ContinueOnError(t *testing.T) {
	// A -> B -> C
	// Step B fails but has ContinueOnError: true
	// Downstream step C should STILL run and succeed!
	p := &pipeline.Pipeline{
		Name: "continue-on-error",
		Steps: []pipeline.Step{
			{ID: "A", Image: "alpine", Run: "echo A"},
			{ID: "B", Image: "alpine", Run: "echo B", DependsOn: []string{"A"}, ContinueOnError: true},
			{ID: "C", Image: "alpine", Run: "echo C", DependsOn: []string{"B"}},
		},
	}

	g, err := dag.Build(p)
	if err != nil {
		t.Fatalf("building dag: %v", err)
	}

	runner := newMockStepRunner()
	runner.exitCodes["B"] = 1

	sched := New(p, g, runner, SchedulerOptions{
		RunID:    "run-continue",
		FailFast: false,
	})

	res, _ := sched.Execute(context.Background())

	if res.StepResults["B"].Status != StatusFailed {
		t.Errorf("step B expected failure, got %s", res.StepResults["B"].Status)
	}
	if res.StepResults["C"].Status != StatusSuccess {
		t.Errorf("step C expected success with continue-on-error, got %s", res.StepResults["C"].Status)
	}
}

