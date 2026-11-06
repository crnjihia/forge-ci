// path: internal/notify/africas_talking.go
package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/crnji/forge-ci/internal/billing"
)

const (
	// ProductionBaseURL is Africa's Talking production SMS endpoint.
	ProductionBaseURL = "https://api.africastalking.com/version1/messaging"

	// SandboxBaseURL is Africa's Talking sandbox SMS endpoint.
	SandboxBaseURL = "https://api.sandbox.africastalking.com/version1/messaging"
)

// Notifier defines the interface for dispatching build alerts and SMS notifications.
type Notifier interface {
	SendSMS(ctx context.Context, to string, message string) (*SMSResponse, error)
	SendBuildAlert(ctx context.Context, to string, alert BuildAlert) (*SMSResponse, error)
}

// Config holds credentials and options for Africa's Talking integration.
type Config struct {
	Username   string       // Africa's Talking username ("sandbox" for sandbox)
	APIKey     string       // Africa's Talking API key
	SenderID   string       // Optional Alphanumeric sender ID or shortcode (e.g., "FORGECI")
	IsSandbox  bool         // If true, forces sandbox endpoint
	BaseURL    string       // Optional base URL override (for tests or proxies)
	HTTPClient *http.Client // Optional HTTP client
}

// BuildAlert holds pipeline execution details for formatting bilingual SMS alerts.
type BuildAlert struct {
	PipelineName string
	CommitSHA    string
	Success      bool
	Duration     time.Duration
	FailedStep   string
	RunURL       string
}

// RecipientResult holds delivery status details for an individual phone recipient.
type RecipientResult struct {
	StatusCode int    `json:"statusCode"`
	Number     string `json:"number"`
	Status     string `json:"status"`
	Cost       string `json:"cost"`
	MessageID  string `json:"messageId"`
}

// SMSResponse maps Africa's Talking JSON response.
type SMSResponse struct {
	SMSMessageData struct {
		Message    string            `json:"Message"`
		Recipients []RecipientResult `json:"Recipients"`
	} `json:"SMSMessageData"`
}

// AfricasTalkingClient interacts with Africa's Talking SMS API.
type AfricasTalkingClient struct {
	cfg        Config
	httpClient *http.Client
	baseURL    string
}

// NewAfricasTalkingClient initializes an Africa's Talking SMS client.
func NewAfricasTalkingClient(cfg Config) *AfricasTalkingClient {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: 15 * time.Second,
		}
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		if cfg.IsSandbox || strings.EqualFold(cfg.Username, "sandbox") {
			baseURL = SandboxBaseURL
		} else {
			baseURL = ProductionBaseURL
		}
	}

	return &AfricasTalkingClient{
		cfg:        cfg,
		httpClient: client,
		baseURL:    baseURL,
	}
}

// NormalizeKenyanPhoneE164 normalizes a Kenyan phone number into international E.164 format (+254...).
func NormalizeKenyanPhoneE164(phone string) (string, error) {
	canonical, err := billing.NormalizeKenyanPhone(phone)
	if err != nil {
		return "", err
	}
	return "+" + canonical, nil
}

// FormatBuildMessage generates a bilingual (Swahili & English) SMS status message.
// Success example: "Kazi imefaulu! Build for forge-ci (commit a1b2c3d) succeeded in 45s. Angalia: https://ci.forge.ke/runs/123"
// Failure example: "Kazi imeshindwa! Build for forge-ci (commit a1b2c3d) failed at step \"test\". Angalia: https://ci.forge.ke/runs/123"
func FormatBuildMessage(alert BuildAlert) string {
	sha := alert.CommitSHA
	if len(sha) > 7 {
		sha = sha[:7]
	}
	if sha == "" {
		sha = "unknown"
	}

	pipeline := alert.PipelineName
	if pipeline == "" {
		pipeline = "pipeline"
	}

	if alert.Success {
		durStr := alert.Duration.Round(time.Second).String()
		if alert.Duration == 0 {
			durStr = "0s"
		}

		if alert.RunURL != "" {
			return fmt.Sprintf("Kazi imefaulu! Build for %s (commit %s) succeeded in %s. Angalia: %s", pipeline, sha, durStr, alert.RunURL)
		}
		return fmt.Sprintf("Kazi imefaulu! Build for %s (commit %s) succeeded in %s.", pipeline, sha, durStr)
	}

	step := alert.FailedStep
	if step == "" {
		step = "unknown"
	}

	if alert.RunURL != "" {
		return fmt.Sprintf("Kazi imeshindwa! Build for %s (commit %s) failed at step %q. Angalia: %s", pipeline, sha, step, alert.RunURL)
	}
	return fmt.Sprintf("Kazi imeshindwa! Build for %s (commit %s) failed at step %q.", pipeline, sha, step)
}

// SendSMS dispatches a single SMS via Africa's Talking API.
func (c *AfricasTalkingClient) SendSMS(ctx context.Context, to string, message string) (*SMSResponse, error) {
	normalizedTo, err := NormalizeKenyanPhoneE164(to)
	if err != nil {
		return nil, fmt.Errorf("invalid recipient phone: %w", err)
	}

	trimmedMsg := strings.TrimSpace(message)
	if trimmedMsg == "" {
		return nil, fmt.Errorf("message content cannot be empty")
	}

	formData := url.Values{}
	formData.Set("username", c.cfg.Username)
	formData.Set("to", normalizedTo)
	formData.Set("message", trimmedMsg)
	if c.cfg.SenderID != "" {
		formData.Set("from", c.cfg.SenderID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create SMS request: %w", err)
	}

	req.Header.Set("apiKey", c.cfg.APIKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute SMS request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read SMS response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("africas talking SMS API error (status %d): %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}

	var smsResp SMSResponse
	if err := json.Unmarshal(bodyBytes, &smsResp); err != nil {
		return nil, fmt.Errorf("failed to parse Africa's Talking SMS response JSON: %w (body: %s)", err, string(bodyBytes))
	}

	return &smsResp, nil
}

// SendBuildAlert constructs a bilingual SMS notification and sends it to the recipient.
func (c *AfricasTalkingClient) SendBuildAlert(ctx context.Context, to string, alert BuildAlert) (*SMSResponse, error) {
	msg := FormatBuildMessage(alert)
	return c.SendSMS(ctx, to, msg)
}

