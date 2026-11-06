// path: internal/billing/mpesa.go
package billing

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

var kenyanPhoneRegex = regexp.MustCompile(`^(?:254|\+254|0)?([17]\d{8})$`)

// NormalizeKenyanPhone converts any standard Kenyan mobile format (+254..., 07..., 01...)
// into the canonical 12-digit format required by Daraja (2547XXXXXXXX or 2541XXXXXXXX).
func NormalizeKenyanPhone(phone string) (string, error) {
	cleaned := strings.TrimSpace(phone)
	cleaned = strings.ReplaceAll(cleaned, " ", "")
	cleaned = strings.ReplaceAll(cleaned, "-", "")

	matches := kenyanPhoneRegex.FindStringSubmatch(cleaned)
	if len(matches) != 2 {
		return "", fmt.Errorf("invalid Kenyan phone number %q: must match +2547XX..., +2541XX..., 07XX... or 01XX...", phone)
	}

	return "254" + matches[1], nil
}

// STKPushRequest holds parameters for initiating an M-Pesa STK push.
type STKPushRequest struct {
	PhoneNumber string  // Customer phone number (+254...)
	AmountKES   float64 // Amount in Kenyan Shillings
	AccountRef  string  // Account identifier displayed on phone prompt
	Description string  // Transaction note
}

// STKPushResponse represents Daraja's immediate acknowledgment of the push request.
type STKPushResponse struct {
	MerchantRequestID string `json:"MerchantRequestID"`
	CheckoutRequestID string `json:"CheckoutRequestID"`
	ResponseCode      string `json:"ResponseCode"`
	ResponseDesc      string `json:"ResponseDescription"`
	CustomerMessage   string `json:"CustomerMessage"`
}

// CallbackMetadataItem represents a key-value entry in the Daraja callback payload.
type CallbackMetadataItem struct {
	Name  string      `json:"Name"`
	Value interface{} `json:"Value"`
}

// CallbackMetadata wraps the items list in the callback.
type CallbackMetadata struct {
	Item []CallbackMetadataItem `json:"Item"`
}

// STKCallbackBody represents the inner callback payload.
type STKCallbackBody struct {
	MerchantRequestID string            `json:"MerchantRequestID"`
	CheckoutRequestID string            `json:"CheckoutRequestID"`
	ResultCode        int               `json:"ResultCode"`
	ResultDesc        string            `json:"ResultDesc"`
	CallbackMetadata  *CallbackMetadata `json:"CallbackMetadata,omitempty"`
}

// STKCallbackPayload represents the full incoming webhook envelope from Safaricom.
type STKCallbackPayload struct {
	Body struct {
		STKCallback STKCallbackBody `json:"stkCallback"`
	} `json:"Body"`
}

// ParsedCallback contains normalized transaction outcomes extracted from the webhook.
type ParsedCallback struct {
	CheckoutRequestID string
	MerchantRequestID string
	ResultCode        int
	ResultDesc        string
	Success           bool
	AmountKES         float64
	MpesaReceipt      string
	PhoneNumber       string
	TransactionDate   string
}

// MpesaClient abstracts Daraja API interactions.
type MpesaClient interface {
	InitiateSTKPush(ctx context.Context, req STKPushRequest) (*STKPushResponse, error)
}

// DarajaConfig defines connection parameters for the Safaricom Daraja API.
type DarajaConfig struct {
	ConsumerKey    string
	ConsumerSecret string
	Passkey        string
	Shortcode      string
	CallbackURL    string
	IsSandbox      bool
}

// DarajaClient implements the live Safaricom Daraja REST API client.
type DarajaClient struct {
	cfg        DarajaConfig
	httpClient *http.Client
	baseURL    string

	tokenMu     sync.RWMutex
	accessToken string
	tokenExpiry time.Time
}

// NewDarajaClient creates a client configured for live or sandbox Daraja endpoints.
func NewDarajaClient(cfg DarajaConfig) *DarajaClient {
	baseURL := "https://api.safaricom.co.ke"
	if cfg.IsSandbox {
		baseURL = "https://sandbox.safaricom.co.ke"
	}
	if cfg.Shortcode == "" {
		cfg.Shortcode = "174379" // Default Daraja sandbox Paybill
	}

	return &DarajaClient{
		cfg:        cfg,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// getAccessToken fetches or returns a cached OAuth 2.0 bearer token.
func (c *DarajaClient) getAccessToken(ctx context.Context) (string, error) {
	c.tokenMu.RLock()
	if c.accessToken != "" && time.Now().Before(c.tokenExpiry) {
		token := c.accessToken
		c.tokenMu.RUnlock()
		return token, nil
	}
	c.tokenMu.RUnlock()

	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	// Double check under write lock
	if c.accessToken != "" && time.Now().Before(c.tokenExpiry) {
		return c.accessToken, nil
	}

	authURL := fmt.Sprintf("%s/oauth/v1/generate?grant_type=client_credentials", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, authURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating oauth request: %w", err)
	}

	req.SetBasicAuth(c.cfg.ConsumerKey, c.cfg.ConsumerSecret)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("requesting daraja oauth token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("daraja oauth returned status %d: %s", resp.StatusCode, string(body))
	}

	var oauthResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   string `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&oauthResp); err != nil {
		return "", fmt.Errorf("decoding daraja oauth response: %w", err)
	}

	c.accessToken = oauthResp.AccessToken
	// Subtract 60 seconds buffer
	c.tokenExpiry = time.Now().Add(3500 * time.Second)

	return c.accessToken, nil
}

// InitiateSTKPush sends a prompt to the customer's phone requesting M-Pesa PIN entry.
func (c *DarajaClient) InitiateSTKPush(ctx context.Context, req STKPushRequest) (*STKPushResponse, error) {
	phone, err := NormalizeKenyanPhone(req.PhoneNumber)
	if err != nil {
		return nil, err
	}

	if req.AmountKES <= 0 {
		return nil, fmt.Errorf("amount must be greater than 0 KES")
	}

	token, err := c.getAccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("authenticating with daraja: %w", err)
	}

	// Nairobi timezone timestamp: YYYYMMDDHHmmss
	nairobiLoc, locErr := time.LoadLocation("Africa/Nairobi")
	if locErr != nil {
		nairobiLoc = time.FixedZone("EAT", 3*3600)
	}
	timestamp := time.Now().In(nairobiLoc).Format("20060102150405")

	// Password = base64(Shortcode + Passkey + Timestamp)
	rawPass := fmt.Sprintf("%s%s%s", c.cfg.Shortcode, c.cfg.Passkey, timestamp)
	password := base64.StdEncoding.EncodeToString([]byte(rawPass))

	accountRef := req.AccountRef
	if accountRef == "" {
		accountRef = "ForgeCI"
	}
	desc := req.Description
	if desc == "" {
		desc = "Forge CI Credits Topup"
	}

	payload := map[string]interface{}{
		"BusinessShortCode": c.cfg.Shortcode,
		"Password":          password,
		"Timestamp":         timestamp,
		"TransactionType":   "CustomerPayBillOnline",
		"Amount":            int64(req.AmountKES),
		"PartyA":            phone,
		"PartyB":            c.cfg.Shortcode,
		"PhoneNumber":       phone,
		"CallBackURL":       c.cfg.CallbackURL,
		"AccountReference":  accountRef,
		"TransactionDesc":   desc,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshaling stk request: %w", err)
	}

	stkURL := fmt.Sprintf("%s/mpesa/stkpush/v1/processrequest", c.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, stkURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("creating stk http request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("executing stk push request: %w", err)
	}
	defer resp.Body.Close()

	var stkResp STKPushResponse
	if err := json.NewDecoder(resp.Body).Decode(&stkResp); err != nil {
		return nil, fmt.Errorf("decoding stk push response: %w", err)
	}

	if stkResp.ResponseCode != "0" {
		return &stkResp, fmt.Errorf("daraja rejected request (code %s): %s", stkResp.ResponseCode, stkResp.ResponseDesc)
	}

	return &stkResp, nil
}

// ParseCallback validates and extracts transaction metadata from the M-Pesa webhook callback.
func ParseCallback(body []byte) (*ParsedCallback, error) {
	var payload STKCallbackPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("unmarshaling callback body: %w", err)
	}

	cb := payload.Body.STKCallback
	parsed := &ParsedCallback{
		CheckoutRequestID: cb.CheckoutRequestID,
		MerchantRequestID: cb.MerchantRequestID,
		ResultCode:        cb.ResultCode,
		ResultDesc:        cb.ResultDesc,
		Success:           cb.ResultCode == 0,
	}

	if cb.CallbackMetadata != nil {
		for _, item := range cb.CallbackMetadata.Item {
			switch item.Name {
			case "Amount":
				if v, ok := item.Value.(float64); ok {
					parsed.AmountKES = v
				}
			case "MpesaReceiptNumber":
				if v, ok := item.Value.(string); ok {
					parsed.MpesaReceipt = v
				}
			case "PhoneNumber":
				if v, ok := item.Value.(float64); ok {
					parsed.PhoneNumber = fmt.Sprintf("%.0f", v)
				} else if v, ok := item.Value.(string); ok {
					parsed.PhoneNumber = v
				}
			case "TransactionDate":
				if v, ok := item.Value.(float64); ok {
					parsed.TransactionDate = fmt.Sprintf("%.0f", v)
				} else if v, ok := item.Value.(string); ok {
					parsed.TransactionDate = v
				}
			}
		}
	}

	return parsed, nil
}

