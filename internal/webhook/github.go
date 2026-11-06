// path: internal/webhook/github.go
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	SignatureHeader = "X-Hub-Signature-256"
	EventHeader     = "X-GitHub-Event"
	DeliveryHeader  = "X-GitHub-Delivery"
	ZeroCommitSHA   = "0000000000000000000000000000000000000000"
)

// Repository represents the GitHub repo metadata in a webhook.
type Repository struct {
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	CloneURL string `json:"clone_url"`
	HTMLURL  string `json:"html_url"`
	Private  bool   `json:"private"`
}

// Sender represents the user who triggered the webhook.
type Sender struct {
	Login string `json:"login"`
}

// PushPayload represents a GitHub push webhook event payload.
type PushPayload struct {
	Ref        string     `json:"ref"`
	Before     string     `json:"before"`
	After      string     `json:"after"`
	Repository Repository `json:"repository"`
	Sender     Sender     `json:"sender"`
}

// PushEvent represents a normalized CI run trigger from a GitHub push.
type PushEvent struct {
	DeliveryID string
	RepoName   string
	RepoURL    string
	Branch     string
	CommitSHA  string
	Sender     string
	IsPrivate  bool
}

// CommitStatus represents the build status reported back to GitHub.
type CommitStatus struct {
	State       string `json:"state"` // pending, success, failure, error
	TargetURL   string `json:"target_url,omitempty"`
	Description string `json:"description,omitempty"`
	Context     string `json:"context"` // e.g. "forge-ci"
}

// GitHubStatusReporter abstracts posting statuses to the GitHub Commit Status API.
type GitHubStatusReporter interface {
	PostCommitStatus(ctx context.Context, ownerRepo, sha string, status CommitStatus) error
}

// VerifySignature validates a GitHub HMAC-SHA256 signature in constant time.
func VerifySignature(secretKey []byte, rawPayload []byte, signatureHeader string) bool {
	if len(secretKey) == 0 || len(rawPayload) == 0 || signatureHeader == "" {
		return false
	}

	mac := hmac.New(sha256.New, secretKey)
	mac.Write(rawPayload)
	expectedMAC := mac.Sum(nil)
	expectedSignature := "sha256=" + hex.EncodeToString(expectedMAC)

	return subtle.ConstantTimeCompare([]byte(expectedSignature), []byte(signatureHeader)) == 1
}

// ParseWebhookRequest verifies HMAC-SHA256 signature and returns the push event.
func ParseWebhookRequest(r *http.Request, secretKey []byte) (*PushEvent, error) {
	sig := r.Header.Get(SignatureHeader)
	if sig == "" {
		return nil, fmt.Errorf("missing %s header", SignatureHeader)
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("reading request body: %w", err)
	}
	// Restore body for any downstream readers
	r.Body = io.NopCloser(bytes.NewReader(body))

	if !VerifySignature(secretKey, body, sig) {
		return nil, fmt.Errorf("HMAC-SHA256 signature verification failed")
	}

	eventType := r.Header.Get(EventHeader)
	if eventType == "ping" {
		return nil, nil // Ping handled as no-op
	}
	if eventType != "push" {
		return nil, fmt.Errorf("unsupported github event %q (only 'push' triggers runs)", eventType)
	}

	var payload PushPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decoding push payload JSON: %w", err)
	}

	// Ignore branch deletion pushes
	if payload.After == "" || payload.After == ZeroCommitSHA {
		return nil, fmt.Errorf("ignoring branch deletion push event")
	}

	branch := strings.TrimPrefix(payload.Ref, "refs/heads/")

	return &PushEvent{
		DeliveryID: r.Header.Get(DeliveryHeader),
		RepoName:   payload.Repository.FullName,
		RepoURL:    payload.Repository.CloneURL,
		Branch:     branch,
		CommitSHA:  payload.After,
		Sender:     payload.Sender.Login,
		IsPrivate:  payload.Repository.Private,
	}, nil
}

// HTTPGitHubReporter posts status updates directly to api.github.com.
type HTTPGitHubReporter struct {
	token      string
	apiBaseURL string
	httpClient *http.Client
	logger     *slog.Logger
}

// NewHTTPGitHubReporter creates an HTTP-based status reporter.
func NewHTTPGitHubReporter(token string, logger *slog.Logger) *HTTPGitHubReporter {
	if logger == nil {
		logger = slog.Default()
	}
	return &HTTPGitHubReporter{
		token:      token,
		apiBaseURL: "https://api.github.com",
		httpClient: &http.Client{Timeout: 10 * time.Second},
		logger:     logger,
	}
}

// PostCommitStatus creates a commit status on GitHub.
func (h *HTTPGitHubReporter) PostCommitStatus(ctx context.Context, ownerRepo, sha string, status CommitStatus) error {
	if status.Context == "" {
		status.Context = "forge-ci"
	}

	url := fmt.Sprintf("%s/repos/%s/statuses/%s", h.apiBaseURL, ownerRepo, sha)
	body, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("marshaling commit status: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating http request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+h.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("posting github status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github api returned status %d: %s", resp.StatusCode, string(respBody))
	}

	h.logger.Info("reported commit status to github",
		"repo", ownerRepo,
		"sha", sha,
		"state", status.State,
	)

	return nil
}

