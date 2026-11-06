// path: cmd/runner/main.go
package main

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/crnji/forge-ci/internal/executor"
)

var (
	// Version is injected during build via -ldflags "-X main.Version=..."
	Version = "v1.0.0"
	// Commit is injected during build via -ldflags "-X main.Commit=..."
	Commit = "none"
	// BuildDate is injected during build via -ldflags "-X main.BuildDate=..."
	BuildDate = "unknown"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	logger.Info("starting forge-ci standalone container runner daemon",
		"version", Version,
		"commit", Commit,
		"build_date", BuildDate,
	)

	// Initialize janitor to clean dangling containers
	_ = executor.NewJanitor(executor.NewMockDockerClient(), logger)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down container runner daemon...")
}
