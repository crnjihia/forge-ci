// path: internal/webhook/webhook_test.go
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

var testSecret = []byte("super_secure_webhook_secret_key_123")

func generateSignature(payload, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifySignature_ValidAndInvalid(t *testing.T) {
	payload := []byte(`{"ref":"refs/heads/main","after":"abcdef123"}`)
	validSig := generateSignature(payload, testSecret)

	if !VerifySignature(testSecret, payload, validSig) {
		t.Errorf("expected valid signature to pass verification")
	}

	// Tampered payload
	tampered := []byte(`{"ref":"refs/heads/main","after":"tampered"}`)
	if VerifySignature(testSecret, tampered, validSig) {
		t.Errorf("expected tampered payload to fail verification")
	}

	// Wrong secret key
	wrongSecret := []byte("incorrect_secret_key")
	if VerifySignature(wrongSecret, payload, validSig) {
		t.Errorf("expected wrong secret key to fail verification")
	}

	// Empty headers/payloads
	if VerifySignature(testSecret, nil, validSig) || VerifySignature(nil, payload, validSig) || VerifySignature(testSecret, payload, "") {
		t.Errorf("expected empty arguments to fail verification")
	}
}

func TestParseWebhookRequest_PushEvent(t *testing.T) {
	payloadObj := PushPayload{
		Ref:   "refs/heads/feature/isolated-builds",
		After: "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b",
		Repository: Repository{
			Name:     "forge-ci",
			FullName: "crnji/forge-ci",
			CloneURL: "https://github.com/crnji/forge-ci.git",
			HTMLURL:  "https://github.com/crnji/forge-ci",
			Private:  true,
		},
		Sender: Sender{
			Login: "chris",
		},
	}

	body, err := json.Marshal(payloadObj)
	if err != nil {
		t.Fatalf("marshaling payload: %v", err)
	}

	req := httptest.NewRequest("POST", "/webhooks/github", bytes.NewReader(body))
	req.Header.Set(SignatureHeader, generateSignature(body, testSecret))
	req.Header.Set(EventHeader, "push")
	req.Header.Set(DeliveryHeader, "delivery-uuid-999")

	event, err := ParseWebhookRequest(req, testSecret)
	if err != nil {
		t.Fatalf("expected successful push event parse, got: %v", err)
	}

	if event.Branch != "feature/isolated-builds" {
		t.Errorf("expected branch 'feature/isolated-builds', got %q", event.Branch)
	}
	if event.CommitSHA != "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b" {
		t.Errorf("unexpected commit SHA: %q", event.CommitSHA)
	}
	if event.RepoName != "crnji/forge-ci" || event.RepoURL != "https://github.com/crnji/forge-ci.git" {
		t.Errorf("unexpected repo metadata: %+v", event)
	}
	if !event.IsPrivate {
		t.Errorf("expected IsPrivate to be true")
	}
	if event.Sender != "chris" {
		t.Errorf("expected sender 'chris', got %q", event.Sender)
	}
}

func TestParseWebhookRequest_PingEvent(t *testing.T) {
	body := []byte(`{"zen":"Keep it logically awesome."}`)
	req := httptest.NewRequest("POST", "/webhooks/github", bytes.NewReader(body))
	req.Header.Set(SignatureHeader, generateSignature(body, testSecret))
	req.Header.Set(EventHeader, "ping")

	event, err := ParseWebhookRequest(req, testSecret)
	if err != nil {
		t.Fatalf("unexpected error on ping: %v", err)
	}
	if event != nil {
		t.Errorf("expected nil event for ping, got %+v", event)
	}
}

func TestParseWebhookRequest_DeletedBranch(t *testing.T) {
	payloadObj := PushPayload{
		Ref:   "refs/heads/temporary-branch",
		After: ZeroCommitSHA, // 0000000000000000000000000000000000000000
		Repository: Repository{
			FullName: "crnji/forge-ci",
		},
	}
	body, _ := json.Marshal(payloadObj)

	req := httptest.NewRequest("POST", "/webhooks/github", bytes.NewReader(body))
	req.Header.Set(SignatureHeader, generateSignature(body, testSecret))
	req.Header.Set(EventHeader, "push")

	_, err := ParseWebhookRequest(req, testSecret)
	if err == nil || !strings.Contains(err.Error(), "ignoring branch deletion") {
		t.Errorf("expected branch deletion error, got: %v", err)
	}
}

func TestMockGitHubReporter_PostAndRetrieve(t *testing.T) {
	mock := NewMockGitHubReporter()
	ctx := context.Background()

	err := mock.PostCommitStatus(ctx, "crnji/forge-ci", "sha123", CommitStatus{
		State:       "pending",
		Description: "Running pipeline steps",
		Context:     "forge-ci",
	})
	if err != nil {
		t.Fatalf("unexpected error posting pending status: %v", err)
	}

	err = mock.PostCommitStatus(ctx, "crnji/forge-ci", "sha123", CommitStatus{
		State:       "success",
		Description: "All 4 steps passed in 12s",
		Context:     "forge-ci",
	})
	if err != nil {
		t.Fatalf("unexpected error posting success status: %v", err)
	}

	statuses := mock.StatusesFor("crnji/forge-ci", "sha123")
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(statuses))
	}
	if statuses[0].State != "pending" || statuses[1].State != "success" {
		t.Errorf("unexpected status progression: %+v", statuses)
	}
	if mock.TotalCalls() != 2 {
		t.Errorf("expected 2 total calls, got %d", mock.TotalCalls())
	}
}

