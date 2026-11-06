// path: cmd/api/routes.go
package main

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/crnji/forge-ci/internal/artifacts"
	"github.com/crnji/forge-ci/internal/billing"
	"github.com/crnji/forge-ci/internal/logs"
	"github.com/crnji/forge-ci/internal/secrets"
	"github.com/crnji/forge-ci/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RouterConfig holds all service dependencies required for route registration.
type RouterConfig struct {
	Store          store.Store
	Coordinator    *Coordinator
	SecretManager  *secrets.SecretManager
	ArtifactStore  artifacts.ArtifactStorage
	ArtifactBucket string
	BillingService *billing.Service
	WebhookSecret  []byte
	RedisPub       logs.RedisPub
	DBPool         *pgxpool.Pool
	Logger         *slog.Logger
}

// BuildRouter configures middleware, health checks, and all API endpoints.
func BuildRouter(cfg RouterConfig) chi.Router {
	r := chi.NewRouter()

	// 1. CORS middleware
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-CSRF-Token, Last-Event-ID, X-Hub-Signature-256, X-GitHub-Event, X-GitHub-Delivery")
			w.Header().Set("Access-Control-Expose-Headers", "Link, Last-Event-ID")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	// 2. Structured request logging
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			next.ServeHTTP(w, r)
			if cfg.Logger != nil {
				cfg.Logger.Info("handled request",
					"method", r.Method,
					"path", r.URL.Path,
					"duration_ms", time.Since(start).Milliseconds(),
				)
			}
		})
	})

	// Health check
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		render.JSON(w, r, map[string]interface{}{
			"status":     "ok",
			"timestamp":  time.Now().UTC().Format(time.RFC3339),
			"service":    "forge-ci-api",
			"version":    Version,
			"commit":     Commit,
			"build_date": BuildDate,
		})
	})

	// Version metadata
	r.Get("/version", func(w http.ResponseWriter, r *http.Request) {
		render.JSON(w, r, map[string]interface{}{
			"service":    "forge-ci-api",
			"version":    Version,
			"commit":     Commit,
			"build_date": BuildDate,
		})
	})

	// Pipeline Runs endpoints
	r.Route("/runs", func(rr chi.Router) {
		rr.Post("/", handleTriggerRun(cfg.Store, cfg.Coordinator))
		rr.Get("/", handleListRuns(cfg.Store))
		rr.Get("/{id}", handleGetRun(cfg.Store))
		rr.Post("/{id}/cancel", handleCancelRun(cfg.Coordinator))

		// Real-time Server-Sent Events log streaming
		rr.Get("/{runID}/logs", logs.SSEHandler(cfg.RedisPub, cfg.DBPool, cfg.SecretManager))

		// Run Artifacts
		rr.Get("/{runID}/artifacts", handleListArtifacts(cfg.Store))
		rr.Get("/{runID}/artifacts/{artifactID}/download", handleDownloadArtifact(cfg.Store, cfg.ArtifactStore, cfg.ArtifactBucket))
	})

	// Secrets Management endpoints
	r.Route("/secrets", func(sr chi.Router) {
		sr.Post("/", handleCreateSecret(cfg.SecretManager))
		sr.Get("/", handleListSecrets(cfg.SecretManager))
		sr.Get("/{name}", handleGetSecret(cfg.SecretManager))
		sr.Delete("/{name}", handleDeleteSecret(cfg.SecretManager))
	})

	// Webhooks
	r.Route("/webhooks", func(wr chi.Router) {
		wr.Post("/github", handleGitHubWebhook(cfg.Store, cfg.Coordinator, cfg.WebhookSecret))
	})

	// Billing (M-Pesa STK Push and Callbacks)
	r.Route("/billing", func(br chi.Router) {
		br.Post("/mpesa/topup", handleMpesaTopUp(cfg.BillingService))
		br.Post("/mpesa/callback", handleMpesaCallback(cfg.BillingService))
		br.Get("/balance", handleGetBillingBalance(cfg.BillingService))
	})

	return r
}

