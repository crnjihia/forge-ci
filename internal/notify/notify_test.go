// path: internal/notify/notify_test.go
package notify

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNormalizeKenyanPhoneE164(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expected  string
		expectErr bool
	}{
		{
			name:      "already formatted E.164 Safaricom",
			input:     "+254712345678",
			expected:  "+254712345678",
			expectErr: false,
		},
		{
			name:      "leading 254 without plus Safaricom",
			input:     "254712345678",
			expected:  "+254712345678",
			expectErr: false,
		},
		{
			name:      "local 07XX format Safaricom",
			input:     "0712345678",
			expected:  "+254712345678",
			expectErr: false,
		},
		{
			name:      "local 01XX format Safaricom/Airtel",
			input:     "0112345678",
			expected:  "+254112345678",
			expectErr: false,
		},
		{
			name:      "formatted with spaces",
			input:     " 0722 345 678 ",
			expected:  "+254722345678",
			expectErr: false,
		},
		{
			name:      "formatted with hyphens",
			input:     "+254-722-345-678",
			expected:  "+254722345678",
			expectErr: false,
		},
		{
			name:      "empty string",
			input:     "",
			expected:  "",
			expectErr: true,
		},
		{
			name:      "short number",
			input:     "07123",
			expected:  "",
			expectErr: true,
		},
		{
			name:      "invalid prefix 06",
			input:     "0612345678",
			expected:  "",
			expectErr: true,
		},
		{
			name:      "non-Kenyan international number",
			input:     "+14155552671",
			expected:  "",
			expectErr: true,
		},
		{
			name:      "too long number",
			input:     "071234567899",
			expected:  "",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeKenyanPhoneE164(tt.input)
			if tt.expectErr {
				if err == nil {
					t.Fatalf("expected error for input %q, got nil", tt.input)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error for input %q: %v", tt.input, err)
			}

			if got != tt.expected {
				t.Errorf("NormalizeKenyanPhoneE164(%q) = %q; want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestFormatBuildMessage(t *testing.T) {
	t.Run("success alert with run url", func(t *testing.T) {
		alert := BuildAlert{
			PipelineName: "forge-ci",
			CommitSHA:    "4f3c8a901234567890abcdef",
			Success:      true,
			Duration:     45 * time.Second,
			RunURL:       "https://ci.forge.ke/runs/123",
		}

		msg := FormatBuildMessage(alert)
		if !strings.HasPrefix(msg, "Kazi imefaulu!") {
			t.Errorf("expected message to begin with 'Kazi imefaulu!', got: %s", msg)
		}
		if !strings.Contains(msg, "forge-ci") {
			t.Errorf("expected message to mention pipeline 'forge-ci', got: %s", msg)
		}
		if !strings.Contains(msg, "commit 4f3c8a9") {
			t.Errorf("expected shortened commit '4f3c8a9', got: %s", msg)
		}
		if !strings.Contains(msg, "succeeded in 45s") {
			t.Errorf("expected duration 'succeeded in 45s', got: %s", msg)
		}
		if !strings.Contains(msg, "Angalia: https://ci.forge.ke/runs/123") {
			t.Errorf("expected dashboard link, got: %s", msg)
		}
	})

	t.Run("success alert without run url", func(t *testing.T) {
		alert := BuildAlert{
			PipelineName: "api-backend",
			CommitSHA:    "1234567",
			Success:      true,
			Duration:     84 * time.Second,
		}

		msg := FormatBuildMessage(alert)
		if strings.Contains(msg, "Angalia:") {
			t.Errorf("expected no 'Angalia:' link when RunURL is empty, got: %s", msg)
		}
		if !strings.Contains(msg, "succeeded in 1m24s.") {
			t.Errorf("expected formatted duration 1m24s, got: %s", msg)
		}
	})

	t.Run("failure alert with step and url", func(t *testing.T) {
		alert := BuildAlert{
			PipelineName: "forge-ci",
			CommitSHA:    "9b8a7c6d5e4f3a2b1c",
			Success:      false,
			FailedStep:   "docker-build",
			RunURL:       "https://ci.forge.ke/runs/456",
		}

		msg := FormatBuildMessage(alert)
		if !strings.HasPrefix(msg, "Kazi imeshindwa!") {
			t.Errorf("expected message to begin with 'Kazi imeshindwa!', got: %s", msg)
		}
		if !strings.Contains(msg, `failed at step "docker-build"`) {
			t.Errorf("expected failed step 'docker-build' in quotes, got: %s", msg)
		}
		if !strings.Contains(msg, "commit 9b8a7c6") {
			t.Errorf("expected truncated commit 9b8a7c6, got: %s", msg)
		}
		if !strings.Contains(msg, "Angalia: https://ci.forge.ke/runs/456") {
			t.Errorf("expected dashboard link, got: %s", msg)
		}
	})

	t.Run("defaults for empty fields", func(t *testing.T) {
		alert := BuildAlert{
			Success: false,
		}

		msg := FormatBuildMessage(alert)
		if !strings.Contains(msg, "Build for pipeline (commit unknown) failed at step \"unknown\".") {
			t.Errorf("expected default placeholders, got: %s", msg)
		}
	})
}

func TestAfricasTalkingClient_SendSMS_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST request, got %s", r.Method)
		}
		if r.Header.Get("apiKey") != "test_api_key_123" {
			t.Errorf("expected apiKey header 'test_api_key_123', got %q", r.Header.Get("apiKey"))
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("expected Accept application/json, got %q", r.Header.Get("Accept"))
		}
		if !strings.Contains(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
			t.Errorf("expected Content-Type urlencoded, got %q", r.Header.Get("Content-Type"))
		}

		if err := r.ParseForm(); err != nil {
			t.Fatalf("failed to parse form data: %v", err)
		}

		if r.Form.Get("username") != "sandbox" {
			t.Errorf("expected username 'sandbox', got %q", r.Form.Get("username"))
		}
		if r.Form.Get("to") != "+254712345678" {
			t.Errorf("expected to '+254712345678', got %q", r.Form.Get("to"))
		}
		if r.Form.Get("message") != "Kazi imefaulu! Build succeeded." {
			t.Errorf("expected message body, got %q", r.Form.Get("message"))
		}
		if r.Form.Get("from") != "FORGECI" {
			t.Errorf("expected senderID 'FORGECI', got %q", r.Form.Get("from"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{
			"SMSMessageData": {
				"Message": "Sent to 1/1 Total Cost: KES 0.8000",
				"Recipients": [
					{
						"statusCode": 101,
						"number": "+254712345678",
						"status": "Success",
						"cost": "KES 0.8000",
						"messageId": "ATXid_live_test_001"
					}
				]
			}
		}`))
	}))
	defer server.Close()

	client := NewAfricasTalkingClient(Config{
		Username:   "sandbox",
		APIKey:     "test_api_key_123",
		SenderID:   "FORGECI",
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})

	resp, err := client.SendSMS(context.Background(), "0712345678", "Kazi imefaulu! Build succeeded.")
	if err != nil {
		t.Fatalf("expected successful SendSMS, got err: %v", err)
	}

	if len(resp.SMSMessageData.Recipients) != 1 {
		t.Fatalf("expected 1 recipient, got %d", len(resp.SMSMessageData.Recipients))
	}

	rec := resp.SMSMessageData.Recipients[0]
	if rec.StatusCode != 101 || rec.Status != "Success" {
		t.Errorf("expected recipient status 101 Success, got %d %s", rec.StatusCode, rec.Status)
	}
	if rec.Number != "+254712345678" {
		t.Errorf("expected recipient number +254712345678, got %s", rec.Number)
	}
	if rec.Cost != "KES 0.8000" {
		t.Errorf("expected cost KES 0.8000, got %s", rec.Cost)
	}
	if rec.MessageID != "ATXid_live_test_001" {
		t.Errorf("expected messageId ATXid_live_test_001, got %s", rec.MessageID)
	}
}

func TestAfricasTalkingClient_SendSMS_Errors(t *testing.T) {
	client := NewAfricasTalkingClient(Config{
		Username: "sandbox",
		APIKey:   "key",
	})

	t.Run("invalid recipient phone", func(t *testing.T) {
		_, err := client.SendSMS(context.Background(), "invalid-phone", "test message")
		if err == nil {
			t.Fatal("expected error for invalid phone, got nil")
		}
	})

	t.Run("empty message", func(t *testing.T) {
		_, err := client.SendSMS(context.Background(), "+254712345678", "   ")
		if err == nil {
			t.Fatal("expected error for empty message, got nil")
		}
	})

	t.Run("http server error 500", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Internal Service Error"))
		}))
		defer server.Close()

		errClient := NewAfricasTalkingClient(Config{
			Username:   "sandbox",
			APIKey:     "key",
			BaseURL:    server.URL,
			HTTPClient: server.Client(),
		})

		_, err := errClient.SendSMS(context.Background(), "0712345678", "Hello")
		if err == nil {
			t.Fatal("expected error on HTTP 500, got nil")
		}
		if !strings.Contains(err.Error(), "500") {
			t.Errorf("expected error to mention status 500, got: %v", err)
		}
	})
}

func TestAfricasTalkingClient_SendBuildAlert(t *testing.T) {
	var capturedMsg string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		capturedMsg = r.Form.Get("message")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"SMSMessageData": {
				"Message": "Sent",
				"Recipients": [{"statusCode": 101, "number": "+254712345678", "status": "Success", "cost": "KES 0.80", "messageId": "ATX1"}]
			}
		}`))
	}))
	defer server.Close()

	client := NewAfricasTalkingClient(Config{
		Username:   "sandbox",
		APIKey:     "key",
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})

	alert := BuildAlert{
		PipelineName: "forge-ci",
		CommitSHA:    "abc12345",
		Success:      true,
		Duration:     30 * time.Second,
		RunURL:       "https://forge.ke",
	}

	resp, err := client.SendBuildAlert(context.Background(), "0712345678", alert)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp == nil || len(resp.SMSMessageData.Recipients) != 1 {
		t.Fatalf("expected valid SMS response, got %v", resp)
	}

	if !strings.Contains(capturedMsg, "Kazi imefaulu!") {
		t.Errorf("expected captured message to contain 'Kazi imefaulu!', got %q", capturedMsg)
	}
}

func TestMockNotifier(t *testing.T) {
	mock := NewMockNotifier()

	t.Run("basic send and retrieval", func(t *testing.T) {
		resp, err := mock.SendSMS(context.Background(), "0712345678", "Test notification")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(resp.SMSMessageData.Recipients) != 1 {
			t.Fatalf("expected 1 recipient, got %d", len(resp.SMSMessageData.Recipients))
		}
		if resp.SMSMessageData.Recipients[0].Status != "Success" {
			t.Errorf("expected status 'Success', got %s", resp.SMSMessageData.Recipients[0].Status)
		}

		if mock.Count() != 1 {
			t.Fatalf("expected count 1, got %d", mock.Count())
		}

		last := mock.GetLastMessage()
		if last == nil {
			t.Fatal("expected non-nil last message")
		}
		if last.To != "+254712345678" {
			t.Errorf("expected To '+254712345678', got %s", last.To)
		}
		if last.Message != "Test notification" {
			t.Errorf("expected Message 'Test notification', got %s", last.Message)
		}
	})

	t.Run("send build alert with alert metadata", func(t *testing.T) {
		alert := BuildAlert{
			PipelineName: "web-client",
			CommitSHA:    "fedcba98",
			Success:      false,
			FailedStep:   "lint",
		}

		_, err := mock.SendBuildAlert(context.Background(), "+254722334455", alert)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		last := mock.GetLastMessage()
		if last == nil || last.Alert == nil {
			t.Fatal("expected last message with non-nil Alert")
		}
		if last.Alert.FailedStep != "lint" {
			t.Errorf("expected FailedStep 'lint', got %s", last.Alert.FailedStep)
		}
		if !strings.Contains(last.Message, "Kazi imeshindwa!") {
			t.Errorf("expected message to contain 'Kazi imeshindwa!', got %s", last.Message)
		}
	})

	t.Run("invalid phone on mock", func(t *testing.T) {
		_, err := mock.SendSMS(context.Background(), "not-a-phone", "test")
		if err == nil {
			t.Fatal("expected error on invalid phone, got nil")
		}
	})

	t.Run("simulated failure number", func(t *testing.T) {
		mock.SetFailNumber("0799887766", true)

		resp, err := mock.SendSMS(context.Background(), "0799887766", "Alert")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(resp.SMSMessageData.Recipients) != 1 {
			t.Fatalf("expected 1 recipient, got %d", len(resp.SMSMessageData.Recipients))
		}
		if resp.SMSMessageData.Recipients[0].Status != "Failed" {
			t.Errorf("expected recipient status 'Failed', got %s", resp.SMSMessageData.Recipients[0].Status)
		}
		if resp.SMSMessageData.Recipients[0].StatusCode != 102 {
			t.Errorf("expected recipient statusCode 102, got %d", resp.SMSMessageData.Recipients[0].StatusCode)
		}
	})

	t.Run("simulated error", func(t *testing.T) {
		simErr := fmt.Errorf("network connection refused")
		mock.SetSimulatedError(simErr)

		_, err := mock.SendSMS(context.Background(), "0712345678", "Hello")
		if err != simErr {
			t.Fatalf("expected error %v, got %v", simErr, err)
		}

		mock.SetSimulatedError(nil)
	})

	t.Run("reset clears records", func(t *testing.T) {
		mock.Reset()
		if mock.Count() != 0 {
			t.Fatalf("expected count 0 after reset, got %d", mock.Count())
		}
		if mock.GetLastMessage() != nil {
			t.Fatal("expected nil last message after reset")
		}
	})

	t.Run("thread-safe concurrent sending", func(t *testing.T) {
		mock.Reset()
		const workers = 30
		var wg sync.WaitGroup
		wg.Add(workers)

		for i := 0; i < workers; i++ {
			go func(idx int) {
				defer wg.Done()
				_, _ = mock.SendSMS(context.Background(), "0712345678", fmt.Sprintf("Message %d", idx))
			}(i)
		}

		wg.Wait()
		if mock.Count() != workers {
			t.Fatalf("expected %d messages, got %d", workers, mock.Count())
		}
	})
}

