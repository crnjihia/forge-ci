// path: internal/checkout/git_test.go
package checkout

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/crnji/forge-ci/internal/executor"
)

type mockCheckoutRunner struct {
	lastConfig executor.StepExecutionConfig
	exitCode   int
	err        error
	called     bool
}

func (m *mockCheckoutRunner) RunStep(ctx context.Context, cfg executor.StepExecutionConfig) (*executor.StepResult, error) {
	m.called = true
	m.lastConfig = cfg

	if cfg.LogCallback != nil {
		cfg.LogCallback("stdout", "Cloning into '/workspace'...")
		if token, ok := cfg.Step.Env["FORGE_DEPLOY_TOKEN"]; ok && token != "" {
			cfg.LogCallback("stdout", fmt.Sprintf("Using credentials: %s", token))
		}
	}

	if m.err != nil {
		return nil, m.err
	}

	return &executor.StepResult{
		StepID:   cfg.Step.ID,
		ExitCode: m.exitCode,
	}, nil
}

func TestBuildCheckoutScript_PublicRepo(t *testing.T) {
	cfg := CheckoutConfig{
		RepoURL:   "https://github.com/example/public-repo.git",
		CommitSHA: "abcdef1234567890",
		Branch:    "main",
	}

	script, env := BuildCheckoutScript(cfg)

	if !strings.Contains(script, "git fetch --depth 1 origin \"$FORGE_COMMIT_SHA\"") {
		t.Errorf("script missing commit fetch command")
	}
	if env["FORGE_REPO_URL"] != "https://github.com/example/public-repo.git" {
		t.Errorf("unexpected repo url in env: %s", env["FORGE_REPO_URL"])
	}
	if env["FORGE_COMMIT_SHA"] != "abcdef1234567890" {
		t.Errorf("unexpected commit sha in env: %s", env["FORGE_COMMIT_SHA"])
	}
	if _, ok := env["FORGE_DEPLOY_TOKEN"]; ok {
		t.Errorf("expected no deploy token in env for public repo")
	}
}

func TestBuildCheckoutScript_PrivateRepoWithToken(t *testing.T) {
	cfg := CheckoutConfig{
		RepoURL:     "https://github.com/example/private-repo.git",
		CommitSHA:   "1122334455667788",
		DeployToken: "ghp_mock_deploy_token_999",
	}

	script, env := BuildCheckoutScript(cfg)

	if !strings.Contains(script, "http.extraHeader \"Authorization: token $FORGE_DEPLOY_TOKEN\"") {
		t.Errorf("script missing http.extraHeader configuration for deploy token")
	}
	if env["FORGE_DEPLOY_TOKEN"] != "ghp_mock_deploy_token_999" {
		t.Errorf("expected deploy token in env: %s", env["FORGE_DEPLOY_TOKEN"])
	}
}

func TestCheckout_Success(t *testing.T) {
	mockRunner := &mockCheckoutRunner{}
	cloner := NewCloner(mockRunner, nil)

	tempDir := t.TempDir()

	cfg := CheckoutConfig{
		RunID:        "run-checkout-101",
		RepoURL:      "https://github.com/example/repo.git",
		CommitSHA:    "abc1234",
		WorkspaceDir: tempDir,
		Timeout:      30 * time.Second,
	}

	err := cloner.Checkout(context.Background(), cfg)
	if err != nil {
		t.Fatalf("expected successful checkout, got: %v", err)
	}

	if !mockRunner.called {
		t.Fatalf("expected step runner to be called")
	}

	stepCfg := mockRunner.lastConfig
	if stepCfg.Step.Image != DefaultGitImage {
		t.Errorf("expected default image %q, got %q", DefaultGitImage, stepCfg.Step.Image)
	}
	if stepCfg.NetworkMode != "bridge" {
		t.Errorf("expected network mode 'bridge', got %q", stepCfg.NetworkMode)
	}
	if stepCfg.WorkspaceDir != tempDir {
		t.Errorf("expected workspace dir %q, got %q", tempDir, stepCfg.WorkspaceDir)
	}
}

func TestCheckout_MissingFields(t *testing.T) {
	cloner := NewCloner(&mockCheckoutRunner{}, nil)

	err := cloner.Checkout(context.Background(), CheckoutConfig{WorkspaceDir: "/tmp"})
	if err == nil || !strings.Contains(err.Error(), "repository URL is required") {
		t.Errorf("expected repo URL error, got %v", err)
	}

	err = cloner.Checkout(context.Background(), CheckoutConfig{RepoURL: "https://github.com/example/repo"})
	if err == nil || !strings.Contains(err.Error(), "workspace directory is required") {
		t.Errorf("expected workspace dir error, got %v", err)
	}
}

func TestCheckout_TokenMaskingInLogs(t *testing.T) {
	secretToken := "secret_deploy_token_12345"
	mockRunner := &mockCheckoutRunner{}
	cloner := NewCloner(mockRunner, nil)

	var loggedLines []string
	cfg := CheckoutConfig{
		RunID:        "run-mask-test",
		RepoURL:      "https://github.com/example/repo.git",
		CommitSHA:    "abc1234",
		DeployToken:  secretToken,
		WorkspaceDir: t.TempDir(),
		LogCallback: func(stream, line string) {
			loggedLines = append(loggedLines, line)
		},
	}

	err := cloner.Checkout(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected checkout error: %v", err)
	}

	for _, line := range loggedLines {
		if strings.Contains(line, secretToken) {
			t.Errorf("secret deploy token was leaked in log line: %q", line)
		}
	}

	foundMasked := false
	for _, line := range loggedLines {
		if strings.Contains(line, "***") {
			foundMasked = true
			break
		}
	}
	if !foundMasked {
		t.Errorf("expected masked line '***' in logs, got %v", loggedLines)
	}
}

func TestSanitizeRepoURL(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"https://github.com/org/repo.git", "https://github.com/org/repo.git"},
		{"https://user:token123@github.com/org/repo.git", "https://***@github.com/org/repo.git"},
		{"http://token@gitlab.com/group/project.git", "http://***@gitlab.com/group/project.git"},
	}

	for _, c := range cases {
		sanitized := sanitizeRepoURL(c.input)
		if sanitized != c.expected {
			t.Errorf("input %q: expected %q, got %q", c.input, c.expected, sanitized)
		}
	}
}

