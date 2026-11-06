// path: cmd/api/api_test.go
package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/crnji/forge-ci/internal/artifacts"
	"github.com/crnji/forge-ci/internal/billing"
	"github.com/crnji/forge-ci/internal/executor"
	"github.com/crnji/forge-ci/internal/logs"
	"github.com/crnji/forge-ci/internal/notify"
	"github.com/crnji/forge-ci/internal/secrets"
	"github.com/crnji/forge-ci/internal/store"
	"github.com/crnji/forge-ci/internal/webhook"
)

func setupTestServer(t *testing.T) (http.Handler, *store.MemoryStore, *secrets.SecretManager, []byte) {
	t.Helper()

	memStore := store.NewMemoryStore()
	sm := secrets.NewSecretManagerFromKey([]byte("0123456789abcdef0123456789abcdef"))
	sm.SetStore(memStore)

	var redisPub logs.RedisPub
	artStorage := artifacts.NewMockArtifactStorage()
	statusReporter := webhook.NewMockGitHubReporter()
	notifier := notify.NewMockNotifier()
	mockMpesa := billing.NewMockMpesaClient()
	billingService := billing.NewService(mockMpesa, memStore, nil)
	dockerClient := executor.NewMockDockerClient()

	coord := NewCoordinator(
		memStore,
		redisPub,
		nil,
		sm,
		artStorage,
		statusReporter,
		notifier,
		billingService,
		dockerClient,
		"+254712345678",
		nil,
	)

	webhookSecret := []byte("test_webhook_secret_key_1234567")

	router := BuildRouter(RouterConfig{
		Store:          memStore,
		Coordinator:    coord,
		SecretManager:  sm,
		ArtifactStore:  artStorage,
		ArtifactBucket: "forge-artifacts",
		BillingService: billingService,
		WebhookSecret:  webhookSecret,
		RedisPub:       redisPub,
		DBPool:         nil,
		Logger:         nil,
	})

	return router, memStore, sm, webhookSecret
}

func TestHealthCheck(t *testing.T) {
	router, _, _, _ := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["status"] != "ok" {
		t.Errorf("expected status 'ok', got %v", body["status"])
	}
}

func TestRunsAPI(t *testing.T) {
	router, _, _, _ := setupTestServer(t)

	// 1. Trigger a run
	payload := []byte(`{
		"repo_url": "https://github.com/crnji/forge-ci.git",
		"branch": "main",
		"commit_sha": "a1b2c3d4e5f67890"
	}`)

	req := httptest.NewRequest(http.MethodPost, "/runs", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201 Created, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var createdRun store.RunRecord
	if err := json.Unmarshal(rec.Body.Bytes(), &createdRun); err != nil {
		t.Fatalf("failed to parse created run: %v", err)
	}

	if createdRun.ID == "" {
		t.Fatal("expected non-empty run ID")
	}
	if createdRun.Branch != "main" {
		t.Errorf("expected branch 'main', got %s", createdRun.Branch)
	}

	// 2. List runs
	listReq := httptest.NewRequest(http.MethodGet, "/runs", nil)
	listRec := httptest.NewRecorder()

	router.ServeHTTP(listRec, listReq)

	if listRec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", listRec.Code)
	}

	var runList []store.RunRecord
	if err := json.Unmarshal(listRec.Body.Bytes(), &runList); err != nil {
		t.Fatalf("failed to parse run list: %v", err)
	}

	if len(runList) == 0 {
		t.Fatal("expected at least 1 run in list")
	}

	// 3. Get run by ID
	getReq := httptest.NewRequest(http.MethodGet, "/runs/"+createdRun.ID, nil)
	getRec := httptest.NewRecorder()

	router.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", getRec.Code)
	}

	var fetchedRun store.RunRecord
	if err := json.Unmarshal(getRec.Body.Bytes(), &fetchedRun); err != nil {
		t.Fatalf("failed to parse run: %v", err)
	}

	if fetchedRun.ID != createdRun.ID {
		t.Errorf("expected run ID %s, got %s", createdRun.ID, fetchedRun.ID)
	}
}

func TestSecretsAPI(t *testing.T) {
	router, _, _, _ := setupTestServer(t)

	// 1. Create secret
	payload := []byte(`{"name":"DATABASE_PASSWORD","value":"supersecret123"}`)
	req := httptest.NewRequest(http.MethodPost, "/secrets", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201 Created, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	// 2. List secrets
	listReq := httptest.NewRequest(http.MethodGet, "/secrets", nil)
	listRec := httptest.NewRecorder()

	router.ServeHTTP(listRec, listReq)

	var names []string
	if err := json.Unmarshal(listRec.Body.Bytes(), &names); err != nil {
		t.Fatalf("failed to parse secret names: %v", err)
	}

	found := false
	for _, n := range names {
		if n == "DATABASE_PASSWORD" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected 'DATABASE_PASSWORD' in secret list")
	}

	// 3. Get secret metadata (masked value)
	getReq := httptest.NewRequest(http.MethodGet, "/secrets/DATABASE_PASSWORD", nil)
	getRec := httptest.NewRecorder()

	router.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", getRec.Code)
	}

	var secretMeta map[string]string
	_ = json.Unmarshal(getRec.Body.Bytes(), &secretMeta)
	if secretMeta["value"] != "********" {
		t.Errorf("expected masked value '********', got %s", secretMeta["value"])
	}

	// 4. Delete secret
	delReq := httptest.NewRequest(http.MethodDelete, "/secrets/DATABASE_PASSWORD", nil)
	delRec := httptest.NewRecorder()

	router.ServeHTTP(delRec, delReq)

	if delRec.Code != http.StatusNoContent {
		t.Fatalf("expected status 204 No Content, got %d", delRec.Code)
	}
}

func TestBillingAPI(t *testing.T) {
	router, _, _, _ := setupTestServer(t)

	// 1. Initiate M-Pesa Topup
	payload := []byte(`{
		"phone_number": "0712345678",
		"amount_kes": 500.0,
		"user_id": "chris_nairobi"
	}`)

	req := httptest.NewRequest(http.MethodPost, "/billing/mpesa/topup", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var topupResp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &topupResp); err != nil {
		t.Fatalf("failed to decode topup response: %v", err)
	}

	if topupResp["status"] != "initiated" {
		t.Errorf("expected status 'initiated', got %v", topupResp["status"])
	}
	if topupResp["checkout_request_id"] == "" {
		t.Error("expected checkout_request_id in response")
	}

	// 2. Check balance
	balReq := httptest.NewRequest(http.MethodGet, "/billing/balance?user_id=chris_nairobi", nil)
	balRec := httptest.NewRecorder()

	router.ServeHTTP(balRec, balReq)

	if balRec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", balRec.Code)
	}
}

func TestGitHubWebhook(t *testing.T) {
	router, _, _, webhookSecret := setupTestServer(t)

	payload := []byte(`{
		"ref": "refs/heads/main",
		"before": "0123456789abcdef0123456789abcdef01234567",
		"after": "abcdef0123456789abcdef0123456789abcdef01",
		"repository": {
			"name": "forge-ci",
			"full_name": "crnji/forge-ci",
			"clone_url": "https://github.com/crnji/forge-ci.git",
			"html_url": "https://github.com/crnji/forge-ci",
			"private": false
		},
		"sender": {
			"login": "crnji"
		}
	}`)

	// Compute HMAC signature
	mac := hmac.New(sha256.New, webhookSecret)
	mac.Write(payload)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-GitHub-Delivery", "delivery-uuid-12345")
	req.Header.Set("X-Hub-Signature-256", sig)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected status 202 Accepted, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["status"] != "accepted" {
		t.Errorf("expected status 'accepted', got %v", resp["status"])
	}
	if resp["run_id"] == "" {
		t.Error("expected non-empty run_id in webhook response")
	}

	// Give background execution a moment to register steps
	time.Sleep(50 * time.Millisecond)
}
