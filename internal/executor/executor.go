// path: internal/executor/executor.go
package executor

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/crnji/forge-ci/internal/pipeline"
)

// StepExecutionConfig defines parameters for executing a single step.
type StepExecutionConfig struct {
	RunID        string
	Step         pipeline.Step
	WorkspaceDir string
	GlobalEnv    map[string]string
	NetworkMode  string
	LogCallback  func(stream string, line string)
}

// StepResult represents the final execution outcome of a step container.
type StepResult struct {
	StepID      string
	ContainerID string
	ExitCode    int
	Duration    time.Duration
	StartedAt   time.Time
	CompletedAt time.Time
	Error       error
}

// Executor runs pipeline steps inside hardened Docker containers.
type Executor struct {
	cli    DockerClientAPI
	logger *slog.Logger
}

// NewExecutor creates a new step executor instance.
func NewExecutor(cli DockerClientAPI, logger *slog.Logger) *Executor {
	if logger == nil {
		logger = slog.Default()
	}
	return &Executor{
		cli:    cli,
		logger: logger,
	}
}

// RunStep launches an isolated container for a single step, streams logs, and enforces resource quotas.
func (e *Executor) RunStep(ctx context.Context, cfg StepExecutionConfig) (*StepResult, error) {
	startedAt := time.Now()
	res := &StepResult{
		StepID:    cfg.Step.ID,
		StartedAt: startedAt,
	}

	if e.cli == nil {
		return nil, fmt.Errorf("docker client is required")
	}

	// 1. Build environment variables slice
	envSlice := buildEnvSlice(cfg.GlobalEnv, cfg.Step.Env)

	// 2. Parse resource limits
	var nanoCPUs int64
	var memoryBytes int64
	if cfg.Step.Resources != nil {
		if cfg.Step.Resources.CPU > 0 {
			nanoCPUs = int64(cfg.Step.Resources.CPU * 1e9)
		}
		if cfg.Step.Resources.Memory != "" {
			var err error
			memoryBytes, err = pipeline.ParseMemoryBytes(cfg.Step.Resources.Memory)
			if err != nil {
				return nil, fmt.Errorf("invalid memory resource: %w", err)
			}
		}
	}

	// 3. Network mode default
	netMode := cfg.NetworkMode
	if netMode == "" {
		netMode = "none"
	}

	// 4. Container configuration
	containerCfg := &ContainerConfig{
		Image:          cfg.Step.Image,
		Cmd:            []string{"/bin/sh", "-c", cfg.Step.Run},
		Env:            envSlice,
		WorkingDir:     "/workspace",
		ReadonlyRootfs: true,
		Labels: map[string]string{
			"forge.run_id":  cfg.RunID,
			"forge.step_id": cfg.Step.ID,
		},
	}

	// 5. Host configuration with strict security sandboxing
	hostCfg := &HostConfig{
		AutoRemove:  false,
		NetworkMode: netMode,
		Privileged:  false,
		SecurityOpt: []string{"no-new-privileges:true"},
		Tmpfs: map[string]string{
			"/tmp": "rw,exec,nosuid,size=64m",
		},
		Resources: Resources{
			NanoCPUs: nanoCPUs,
			Memory:   memoryBytes,
		},
	}

	if cfg.WorkspaceDir != "" {
		absPath, err := filepath.Abs(cfg.WorkspaceDir)
		if err == nil {
			hostCfg.Binds = []string{fmt.Sprintf("%s:/workspace:rw", absPath)}
		} else {
			hostCfg.Binds = []string{fmt.Sprintf("%s:/workspace:rw", cfg.WorkspaceDir)}
		}
	}

	// 6. Create container
	createResp, err := e.cli.ContainerCreate(ctx, containerCfg, hostCfg, "")
	if err != nil {
		return nil, fmt.Errorf("container create failed for step %q: %w", cfg.Step.ID, err)
	}
	containerID := createResp.ID
	res.ContainerID = containerID

	// 7. Guaranteed container removal on exit
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = e.cli.ContainerRemove(cleanupCtx, containerID, ContainerRemoveOptions{
			Force:         true,
			RemoveVolumes: true,
		})
	}()

	// 8. Start container
	if err := e.cli.ContainerStart(ctx, containerID, ContainerStartOptions{}); err != nil {
		return nil, fmt.Errorf("container start failed for step %q: %w", cfg.Step.ID, err)
	}

	// 9. Stream logs if callback provided
	var logWg sync.WaitGroup
	if cfg.LogCallback != nil {
		logStream, logErr := e.cli.ContainerLogs(ctx, containerID, ContainerLogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Follow:     true,
		})
		if logErr == nil && logStream != nil {
			logWg.Add(1)
			go func() {
				defer logWg.Done()
				defer logStream.Close()
				scanner := bufio.NewScanner(logStream)
				for scanner.Scan() {
					cfg.LogCallback("stdout", scanner.Text())
				}
			}()
		}
	}

	// 10. Wait for container termination
	waitCh, errCh := e.cli.ContainerWait(ctx, containerID, ContainerWaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			res.Error = fmt.Errorf("container wait error on step %q: %w", cfg.Step.ID, err)
			return res, res.Error
		}
	case status := <-waitCh:
		res.ExitCode = int(status.StatusCode)
		if status.StatusCode != 0 {
			res.Error = fmt.Errorf("step %q failed with exit status %d", cfg.Step.ID, status.StatusCode)
		}
	case <-ctx.Done():
		res.Error = fmt.Errorf("step %q cancelled or timed out: %w", cfg.Step.ID, ctx.Err())
		return res, res.Error
	}

	logWg.Wait()

	completedAt := time.Now()
	res.CompletedAt = completedAt
	res.Duration = completedAt.Sub(startedAt)

	return res, res.Error
}

// Janitor handles asynchronous cleanup of orphaned containers and workspace directories.
type Janitor struct {
	cli    DockerClientAPI
	logger *slog.Logger
}

// NewJanitor creates a new cleanup manager.
func NewJanitor(cli DockerClientAPI, logger *slog.Logger) *Janitor {
	if logger == nil {
		logger = slog.Default()
	}
	return &Janitor{
		cli:    cli,
		logger: logger,
	}
}

// CleanupRunContainers finds and forcefully terminates any orphaned containers for the given run ID.
func (j *Janitor) CleanupRunContainers(ctx context.Context, runID string) error {
	if j.cli == nil || runID == "" {
		return nil
	}

	containers, err := j.cli.ContainerList(ctx, ContainerListOptions{
		All: true,
		Labels: map[string]string{
			"forge.run_id": runID,
		},
	})
	if err != nil {
		return fmt.Errorf("listing containers for run %q: %w", runID, err)
	}

	for _, c := range containers {
		_ = j.cli.ContainerRemove(ctx, c.ID, ContainerRemoveOptions{
			Force:         true,
			RemoveVolumes: true,
		})
		j.logger.Info("janitor cleaned up container", "container_id", c.ID, "run_id", runID)
	}

	return nil
}

// CleanupWorkspace safely removes the local workspace directory from the host.
func (j *Janitor) CleanupWorkspace(workspaceDir string) error {
	if workspaceDir == "" {
		return nil
	}
	return os.RemoveAll(workspaceDir)
}

// buildEnvSlice merges global environment and step environment into KEY=VALUE strings.
func buildEnvSlice(global, step map[string]string) []string {
	merged := make(map[string]string, len(global)+len(step))
	for k, v := range global {
		merged[k] = v
	}
	for k, v := range step {
		merged[k] = v
	}
	if len(merged) == 0 {
		return nil
	}
	out := make([]string, 0, len(merged))
	for k, v := range merged {
		out = append(out, fmt.Sprintf("%s=%s", k, v))
	}
	return out
}
