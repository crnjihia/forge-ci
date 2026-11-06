// path: cmd/api/main.go
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/crnji/forge-ci/internal/artifacts"
	"github.com/crnji/forge-ci/internal/billing"
	"github.com/crnji/forge-ci/internal/executor"
	"github.com/crnji/forge-ci/internal/logs"
	"github.com/crnji/forge-ci/internal/notify"
	"github.com/crnji/forge-ci/internal/secrets"
	"github.com/crnji/forge-ci/internal/store"
	"github.com/crnji/forge-ci/internal/webhook"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	useMocks := strings.EqualFold(os.Getenv("USE_MOCK_INTEGRATIONS"), "true") || os.Getenv("USE_MOCK_INTEGRATIONS") == ""
	logger.Info("starting forge-ci api server", "mock_integrations", useMocks)

	// 1. Secrets Master Key
	masterKey := os.Getenv("FORGE_MASTER_KEY")
	if masterKey == "" {
		masterKey = "0123456789abcdef0123456789abcdef" // 32 bytes fallback for local dev
	}
	sm, err := secrets.NewSecretManagerFromKeyString(masterKey)
	if err != nil {
		logger.Error("failed to initialize secret manager", "error", err)
		os.Exit(1)
	}

	// 2. Storage & Database Pool
	var st store.Store
	var dbPool *pgxpool.Pool
	pgURL := os.Getenv("POSTGRES_URL")

	if pgURL != "" && !useMocks {
		pool, err := store.ConnectDB(context.Background(), pgURL)
		if err != nil {
			logger.Warn("postgres connection failed; falling back to memory store", "error", err)
			memStore := store.NewMemoryStore()
			st = memStore
			sm.SetStore(memStore)
		} else {
			pgStore := store.NewPostgresStore(pool)
			if err := pgStore.AutoMigrate(context.Background()); err != nil {
				logger.Error("failed to automigrate postgres schema", "error", err)
				os.Exit(1)
			}
			st = pgStore
			dbPool = pool
			sm.SetStore(pgStore)
			defer pool.Close()
		}
	} else {
		memStore := store.NewMemoryStore()
		st = memStore
		sm.SetStore(memStore)
	}

	// 3. Redis Pub/Sub
	var redisPub logs.RedisPub
	redisURL := os.Getenv("REDIS_URL")

	if redisURL != "" && !useMocks {
		opts, err := redis.ParseURL(redisURL)
		if err == nil {
			rdb := redis.NewClient(opts)
			if err := rdb.Ping(context.Background()).Err(); err == nil {
				redisPub = rdb
				defer rdb.Close()
			}
		}
	}
	// In offline/mock mode when Redis is not running, redisPub remains nil and SSEHandler/coordinator gracefully handle it.

	// 4. Artifact Storage
	var artStorage artifacts.ArtifactStorage
	bucket := os.Getenv("MINIO_BUCKET")
	if bucket == "" {
		bucket = "forge-artifacts"
	}
	minioEndpoint := os.Getenv("MINIO_ENDPOINT")

	if minioEndpoint != "" && !useMocks {
		mStorage, err := artifacts.NewMinIOStorage(artifacts.MinIOConfig{
			Endpoint:  minioEndpoint,
			AccessKey: os.Getenv("MINIO_ACCESS_KEY"),
			SecretKey: os.Getenv("MINIO_SECRET_KEY"),
			UseSSL:    strings.EqualFold(os.Getenv("MINIO_USE_SSL"), "true"),
			Logger:    logger,
		})
		if err == nil {
			artStorage = mStorage
		}
	}
	if artStorage == nil {
		artStorage = artifacts.NewMockArtifactStorage()
	}

	// 5. GitHub Status Reporter
	var statusReporter webhook.GitHubStatusReporter
	ghToken := os.Getenv("GITHUB_TOKEN")
	if ghToken != "" && !useMocks {
		statusReporter = webhook.NewHTTPGitHubReporter(ghToken, logger)
	} else {
		statusReporter = webhook.NewMockGitHubReporter()
	}

	// 6. Africa's Talking SMS Notifier
	var notifier notify.Notifier
	atAPIKey := os.Getenv("AFRICASTALKING_API_KEY")
	if atAPIKey != "" && !useMocks {
		notifier = notify.NewAfricasTalkingClient(notify.Config{
			Username:  os.Getenv("AFRICASTALKING_USERNAME"),
			APIKey:    atAPIKey,
			SenderID:  os.Getenv("AFRICASTALKING_SENDER_ID"),
			IsSandbox: true,
		})
	} else {
		notifier = notify.NewMockNotifier()
	}

	// 7. M-Pesa Daraja Billing Service
	var billingService *billing.Service
	var billingRepo billing.BillingRepository
	if br, ok := st.(billing.BillingRepository); ok {
		billingRepo = br
	} else {
		billingRepo = billing.NewMemoryBillingRepository()
	}

	var mpesaClient billing.MpesaClient
	if !useMocks && os.Getenv("MPESA_CONSUMER_KEY") != "" {
		mpesaClient = billing.NewDarajaClient(billing.DarajaConfig{
			ConsumerKey:    os.Getenv("MPESA_CONSUMER_KEY"),
			ConsumerSecret: os.Getenv("MPESA_CONSUMER_SECRET"),
			Passkey:        os.Getenv("MPESA_PASSKEY"),
			Shortcode:      os.Getenv("MPESA_SHORTCODE"),
			CallbackURL:    os.Getenv("MPESA_CALLBACK_URL"),
			IsSandbox:      true,
		})
	} else {
		mpesaClient = billing.NewMockMpesaClient()
	}
	billingService = billing.NewService(mpesaClient, billingRepo, logger)

	// Seed demo user initial credit balance if empty
	if curBal, err := billingService.GetBalance(context.Background(), "default_user"); err == nil && curBal == 0 {
		_, _ = billingRepo.AddCredits(context.Background(), "default_user", 1000.00)
	}

	// 8. Docker Step Executor
	var dockerClient executor.DockerClientAPI = executor.NewMockDockerClient()

	// 9. Pipeline Execution Coordinator
	alertPhone := os.Getenv("ALERT_PHONE")
	if alertPhone == "" {
		alertPhone = "+254712345678" // Default Kenyan developer alert number
	}

	coordinator := NewCoordinator(
		st,
		redisPub,
		dbPool,
		sm,
		artStorage,
		statusReporter,
		notifier,
		billingService,
		dockerClient,
		alertPhone,
		logger,
	)

	// 10. Router Configuration
	webhookSecret := []byte(os.Getenv("GITHUB_WEBHOOK_SECRET"))
	if len(webhookSecret) == 0 {
		webhookSecret = []byte("sample_github_webhook_secret_32_bytes_long")
	}

	router := BuildRouter(RouterConfig{
		Store:          st,
		Coordinator:    coordinator,
		SecretManager:  sm,
		ArtifactStore:  artStorage,
		ArtifactBucket: bucket,
		BillingService: billingService,
		WebhookSecret:  webhookSecret,
		RedisPub:       redisPub,
		DBPool:         dbPool,
		Logger:         logger,
	})

	// 11. HTTP Server Setup
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := ":" + port

	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("HTTP API server listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server exited unexpectedly", "error", err)
		}
	}()

	// Graceful shutdown on SIGINT/SIGTERM
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down HTTP API server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("server forced to shutdown", "error", err)
	}
	logger.Info("server shutdown gracefully")
}
