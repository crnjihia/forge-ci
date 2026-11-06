// path: internal/executor/executor_test.go
package executor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crnji/forge-ci/internal/pipeline"
)

func TestRunStep_Success(t *testing.T) {
	mock := newMockDockerClient()
	exec := NewExecutor(mock, nil)

	tempDir := t.TempDir()

	cfg := StepExecutionConfig{
		RunID: "run-abc-123",
		Step: pipeline.Step{
			ID:    "test-step",
			Image: "golang:1.22",
			Run:   "go version",
			Resources: &pipeline.Resources{
				CPU:    1.5,
				Memory: "512m",
			},
			Env: map[string]string{
				"STEP_VAR": "custom_step_value",
			},
		},
		WorkspaceDir: tempDir,
		GlobalEnv: map[string]string{
			"CI": "true",
		},
	}

	result, err := exec.RunStep(context.Background(), cfg)
	if err != nil {
		t.Fatalf("expected successful step run, got error: %v", err)
	}

	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if result.ContainerID != "mock-container-12345" {
		t.Errorf("expected container ID 'mock-container-12345', got %q", result.ContainerID)
	}

	// Verify lifecycle execution order
	if !mock.createCalled || !mock.startCalled || !mock.waitCalled || !mock.removeCalled {
		t.Errorf("expected all lifecycle phases called: create=%v, start=%v, wait=%v, remove=%v",
			mock.createCalled, mock.startCalled, mock.waitCalled, mock.removeCalled)
	}

	// Verify container configuration
	cCfg := mock.lastConfig
	if cCfg.Image != "golang:1.22" {
		t.Errorf("expected image golang:1.22, got %q", cCfg.Image)
	}
	if !cCfg.ReadonlyRootfs {
		t.Errorf("expected ReadonlyRootfs to be true")
	}
	if cCfg.WorkingDir != "/workspace" {
		t.Errorf("expected WorkingDir to be '/workspace', got %q", cCfg.WorkingDir)
	}
	if cCfg.Labels["forge.run_id"] != "run-abc-123" || cCfg.Labels["forge.step_id"] != "test-step" {
		t.Errorf("unexpected labels: %+v", cCfg.Labels)
	}

	// Verify security and resource host configuration
	hCfg := mock.lastHostConfig
	if hCfg.Privileged {
		t.Errorf("expected privileged mode to be false")
	}
	if hCfg.NetworkMode != "none" {
		t.Errorf("expected network mode 'none', got %q", hCfg.NetworkMode)
	}
	if len(hCfg.SecurityOpt) == 0 || hCfg.SecurityOpt[0] != "no-new-privileges:true" {
		t.Errorf("expected no-new-privileges:true, got %v", hCfg.SecurityOpt)
	}
	expectedNanoCPUs := int64(1.5 * 1e9)
	if hCfg.Resources.NanoCPUs != expectedNanoCPUs {
		t.Errorf("expected NanoCPUs %d, got %d", expectedNanoCPUs, hCfg.Resources.NanoCPUs)
	}
	expectedMem := int64(512 * 1024 * 1024)
	if hCfg.Resources.Memory != expectedMem {
		t.Errorf("expected memory %d bytes, got %d", expectedMem, hCfg.Resources.Memory)
	}
	if len(hCfg.Binds) != 1 || !strings.Contains(hCfg.Binds[0], ":/workspace:rw") {
		t.Errorf("expected workspace bind mount, got %v", hCfg.Binds)
	}
}

func TestRunStep_NonZeroExitCode(t *testing.T) {
	mock := newMockDockerClient()
	mock.waitStatusCode = 2 // Simulated failure exit code
	exec := NewExecutor(mock, nil)

	cfg := StepExecutionConfig{
		RunID: "run-fail-1",
		Step: pipeline.Step{
			ID:    "failing-step",
			Image: "alpine",
			Run:   "exit 2",
		},
	}

	result, err := exec.RunStep(context.Background(), cfg)
	if err == nil {
		t.Fatalf("expected error for non-zero exit, got nil")
	}

	if result.ExitCode != 2 {
		t.Errorf("expected exit code 2, got %d", result.ExitCode)
	}
	if !strings.Contains(err.Error(), "exit status 2") {
		t.Errorf("expected error message to contain 'exit status 2', got: %v", err)
	}

	// Guaranteed cleanup must still have run!
	if !mock.removeCalled {
		t.Errorf("expected ContainerRemove to be called on non-zero exit")
	}
}

func TestRunStep_ContextCancellation(t *testing.T) {
	mock := newMockDockerClient()
	mock.waitDelay = 300 * time.Millisecond
	exec := NewExecutor(mock, nil)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	cfg := StepExecutionConfig{
		RunID: "run-cancel",
		Step: pipeline.Step{
			ID:    "slow-step",
			Image: "alpine",
			Run:   "sleep 60",
		},
	}

	_, err := exec.RunStep(ctx, cfg)
	if err == nil {
		t.Fatalf("expected cancellation error, got nil")
	}
	if !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("expected cancellation message, got: %v", err)
	}

	// Defer cleanup must still remove container
	if !mock.removeCalled {
		t.Errorf("expected container removal on context cancellation")
	}
}

func TestRunStep_ContextTimeout(t *testing.T) {
	mock := newMockDockerClient()
	mock.waitDelay = 500 * time.Millisecond
	exec := NewExecutor(mock, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	cfg := StepExecutionConfig{
		RunID: "run-timeout",
		Step: pipeline.Step{
			ID:    "timed-step",
			Image: "alpine",
			Run:   "sleep 10",
		},
	}

	_, err := exec.RunStep(ctx, cfg)
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "cancelled or timed out") {
		t.Errorf("expected timeout message, got: %v", err)
	}

	if !mock.removeCalled {
		t.Errorf("expected container removal on timeout")
	}
}

func TestRunStep_LogStreaming(t *testing.T) {
	mock := newMockDockerClient()
	mock.logContent = "Compiling app...\nBuild successful!\n"
	exec := NewExecutor(mock, nil)

	var logMu sync.Mutex
	var collectedLines []string

	cfg := StepExecutionConfig{
		RunID: "run-logs",
		Step: pipeline.Step{
			ID:    "logged-step",
			Image: "alpine",
			Run:   "echo test",
		},
		LogCallback: func(stream string, line string) {
			logMu.Lock()
			collectedLines = append(collectedLines, line)
			logMu.Unlock()
		},
	}

	result, err := exec.RunStep(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected step execution error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}

	logMu.Lock()
	defer logMu.Unlock()
	if len(collectedLines) != 2 {
		t.Fatalf("expected 2 logged lines, got %d: %v", len(collectedLines), collectedLines)
	}
	if collectedLines[0] != "Compiling app..." || collectedLines[1] != "Build successful!" {
		t.Errorf("unexpected logged lines: %v", collectedLines)
	}
}

func TestJanitor_CleanupRunContainers(t *testing.T) {
	mock := newMockDockerClient()
	mock.containers = []ContainerSummary{
		{ID: "c-1", Labels: map[string]string{"forge.run_id": "target-run"}},
		{ID: "c-2", Labels: map[string]string{"forge.run_id": "target-run"}},
	}
	janitor := NewJanitor(mock, nil)

	err := janitor.CleanupRunContainers(context.Background(), "target-run")
	if err != nil {
		t.Fatalf("unexpected janitor error: %v", err)
	}

	if !mock.listCalled {
		t.Errorf("expected ContainerList to be called")
	}
	if !mock.removeCalled {
		t.Errorf("expected ContainerRemove to be called")
	}
}

func TestJanitor_CleanupWorkspace(t *testing.T) {
	janitor := NewJanitor(nil, nil)
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.txt")
	_ = os.WriteFile(testFile, []byte("temp"), 0644)

	err := janitor.CleanupWorkspace(tempDir)
	if err != nil {
		t.Fatalf("unexpected error cleaning workspace: %v", err)
	}

	if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
		t.Errorf("expected workspace directory to be deleted")
	}
}

