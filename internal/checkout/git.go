// path: internal/checkout/git.go
package checkout

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/crnji/forge-ci/internal/executor"
	"github.com/crnji/forge-ci/internal/pipeline"
)

const (
	DefaultGitImage = "alpine/git:latest"
	DefaultTimeout  = 2 * time.Minute
)

// CheckoutConfig defines parameters for cloning source code into the workspace.
type CheckoutConfig struct {
	RunID        string
	RepoURL      string
	CommitSHA    string
	Branch       string
	DeployToken  string
	WorkspaceDir string
	Image        string
	Timeout      time.Duration
	LogCallback  func(stream string, line string)
}

// StepRunner abstracts container execution for checkout operations.
type StepRunner interface {
	RunStep(ctx context.Context, cfg executor.StepExecutionConfig) (*executor.StepResult, error)
}

// Cloner manages cloning Git repositories into isolated workspace directories.
type Cloner struct {
	runner StepRunner
	logger *slog.Logger
}

// NewCloner creates a new Git checkout manager.
func NewCloner(runner StepRunner, logger *slog.Logger) *Cloner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Cloner{
		runner: runner,
		logger: logger,
	}
}

// Checkout executes a Git checkout using an ephemeral container.
func (c *Cloner) Checkout(ctx context.Context, cfg CheckoutConfig) error {
	if cfg.RepoURL == "" {
		return fmt.Errorf("repository URL is required")
	}
	if cfg.WorkspaceDir == "" {
		return fmt.Errorf("workspace directory is required")
	}

	image := cfg.Image
	if image == "" {
		image = DefaultGitImage
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	checkoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd, env := BuildCheckoutScript(cfg)

	// Wrap log callback to mask sensitive deploy token
	wrappedLogCallback := func(stream, line string) {
		if cfg.LogCallback != nil {
			maskedLine := line
			if cfg.DeployToken != "" {
				maskedLine = strings.ReplaceAll(maskedLine, cfg.DeployToken, "***")
			}
			cfg.LogCallback(stream, maskedLine)
		}
	}

	stepConfig := executor.StepExecutionConfig{
		RunID: cfg.RunID,
		Step: pipeline.Step{
			ID:    "checkout",
			Image: image,
			Run:   cmd,
			Env:   env,
			Resources: &pipeline.Resources{
				CPU:    1.0,
				Memory: "512m",
			},
		},
		WorkspaceDir: cfg.WorkspaceDir,
		NetworkMode:  "bridge", // Checkout requires outbound network access to clone
		LogCallback:  wrappedLogCallback,
	}

	c.logger.Info("starting repo checkout",
		"run_id", cfg.RunID,
		"repo_url", sanitizeRepoURL(cfg.RepoURL),
		"commit_sha", cfg.CommitSHA,
		"branch", cfg.Branch,
	)

	result, err := c.runner.RunStep(checkoutCtx, stepConfig)
	if err != nil {
		return fmt.Errorf("checkout step failed: %w", err)
	}

	if result != nil && result.ExitCode != 0 {
		return fmt.Errorf("git checkout container exited with status %d", result.ExitCode)
	}

	c.logger.Info("repo checkout completed successfully", "run_id", cfg.RunID)
	return nil
}

// BuildCheckoutScript constructs the shell script and environment variables for the git container.
func BuildCheckoutScript(cfg CheckoutConfig) (string, map[string]string) {
	env := map[string]string{
		"FORGE_REPO_URL":   cfg.RepoURL,
		"FORGE_COMMIT_SHA": cfg.CommitSHA,
		"FORGE_BRANCH":     cfg.Branch,
	}

	if cfg.DeployToken != "" {
		env["FORGE_DEPLOY_TOKEN"] = cfg.DeployToken
	}

	script := `set -e
mkdir -p /workspace
cd /workspace

if [ ! -d .git ]; then
  git init -q
  git remote add origin "$FORGE_REPO_URL"
fi

if [ -n "$FORGE_DEPLOY_TOKEN" ]; then
  git config http.extraHeader "Authorization: token $FORGE_DEPLOY_TOKEN"
fi

if [ -n "$FORGE_COMMIT_SHA" ]; then
  git fetch --depth 1 origin "$FORGE_COMMIT_SHA"
  git checkout -q FETCH_HEAD
elif [ -n "$FORGE_BRANCH" ]; then
  git fetch --depth 1 origin "$FORGE_BRANCH"
  git checkout -q FETCH_HEAD
else
  git fetch --depth 1 origin HEAD
  git checkout -q FETCH_HEAD
fi

git submodule update --init --recursive --depth 1 2>/dev/null || true
`

	return script, env
}

// sanitizeRepoURL removes basic auth credentials from URL for safe logging.
func sanitizeRepoURL(rawURL string) string {
	parts := strings.SplitN(rawURL, "@", 2)
	if len(parts) == 2 && strings.Contains(parts[0], "://") {
		proto := strings.Split(parts[0], "://")[0]
		return fmt.Sprintf("%s://***@%s", proto, parts[1])
	}
	return rawURL
}

