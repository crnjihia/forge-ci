// path: internal/secrets/secrets_test.go
package secrets

import (
	"encoding/base64"
	"strings"
	"testing"
)

var testMasterKey = []byte("0123456789abcdef0123456789abcdef") // 32 bytes

func TestSecretManager_AddGetDelete(t *testing.T) {
	sm := NewSecretManagerFromKey(testMasterKey)

	err := sm.AddSecret("API_KEY", "secret_value_xyz")
	if err != nil {
		t.Fatalf("expected successful add, got: %v", err)
	}

	val, err := sm.GetSecret("API_KEY")
	if err != nil {
		t.Fatalf("expected successful get, got: %v", err)
	}
	if val != "secret_value_xyz" {
		t.Errorf("expected 'secret_value_xyz', got %q", val)
	}

	names := sm.ListSecretNames()
	if len(names) != 1 || names[0] != "API_KEY" {
		t.Errorf("expected ['API_KEY'], got %v", names)
	}

	err = sm.DeleteSecret("API_KEY")
	if err != nil {
		t.Fatalf("expected successful delete, got: %v", err)
	}

	_, err = sm.GetSecret("API_KEY")
	if err == nil {
		t.Fatalf("expected error after delete, got nil")
	}
}

func TestSecretManager_NonceRandomness(t *testing.T) {
	sm := NewSecretManagerFromKey(testMasterKey)

	plain := "identical_secret_content"
	_ = sm.AddSecret("SEC_1", plain)
	_ = sm.AddSecret("SEC_2", plain)

	sm.mu.RLock()
	rec1 := sm.store["SEC_1"]
	rec2 := sm.store["SEC_2"]
	sm.mu.RUnlock()

	if string(rec1.Nonce) == string(rec2.Nonce) {
		t.Errorf("expected different random nonces for identical plaintexts")
	}
	if string(rec1.EncryptedValue) == string(rec2.EncryptedValue) {
		t.Errorf("expected different ciphertexts due to unique nonces")
	}
}

func TestSecretManager_TamperDetection(t *testing.T) {
	sm := NewSecretManagerFromKey(testMasterKey)

	_ = sm.AddSecret("INTEGRITY_KEY", "critical_payload")

	// Corrupt ciphertext
	sm.mu.Lock()
	rec := sm.store["INTEGRITY_KEY"]
	rec.EncryptedValue[0] ^= 0xFF // Flip bits
	sm.store["INTEGRITY_KEY"] = rec
	sm.mu.Unlock()

	_, err := sm.GetSecret("INTEGRITY_KEY")
	if err == nil {
		t.Fatalf("expected GCM authentication tag error on tampered data, got nil")
	}
	if !strings.Contains(err.Error(), "authentication tag mismatch") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSecretManager_InjectSecrets(t *testing.T) {
	sm := NewSecretManagerFromKey(testMasterKey)

	_ = sm.AddSecret("DEPLOY_TOKEN", "token_123")
	_ = sm.AddSecret("DB_PASS", "pass_456")

	env, err := sm.InjectSecrets([]string{"DEPLOY_TOKEN", "DB_PASS"})
	if err != nil {
		t.Fatalf("expected successful injection, got: %v", err)
	}

	if env["DEPLOY_TOKEN"] != "token_123" || env["DB_PASS"] != "pass_456" {
		t.Errorf("unexpected injected map: %+v", env)
	}

	_, err = sm.InjectSecrets([]string{"MISSING_TOKEN"})
	if err == nil {
		t.Fatalf("expected error for missing secret, got nil")
	}
}

func TestSecretManager_MaskString_MultiLine(t *testing.T) {
	sm := NewSecretManagerFromKey(testMasterKey)

	multiLineKey := `-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEA0Y3k+3wZ9SampleKeyContentLineOne
LineTwoOfSampleKeyContentABCDEFGHIJKLMN123456789
-----END RSA PRIVATE KEY-----`

	_ = sm.AddSecret("SSH_KEY", multiLineKey)

	// Test masking full multi-line block
	fullLog := "Loaded private key:\n" + multiLineKey + "\nProceeding with build."
	maskedFull := sm.MaskString(fullLog)
	if strings.Contains(maskedFull, "LineTwoOfSampleKeyContent") {
		t.Errorf("multi-line key not masked: %s", maskedFull)
	}

	// Test masking single line of the key
	singleLineLog := "Error parsing token: LineTwoOfSampleKeyContentABCDEFGHIJKLMN123456789"
	maskedSingle := sm.MaskString(singleLineLog)
	if strings.Contains(maskedSingle, "LineTwoOfSampleKeyContent") {
		t.Errorf("single line of multi-line key not masked: %s", maskedSingle)
	}
	if !strings.Contains(maskedSingle, "***") {
		t.Errorf("expected placeholder '***', got: %s", maskedSingle)
	}
}

func TestSecretManager_MaskString_Base64(t *testing.T) {
	sm := NewSecretManagerFromKey(testMasterKey)

	rawSecret := "secret_api_token_value_777"
	_ = sm.AddSecret("API_TOKEN", rawSecret)

	b64Secret := base64.StdEncoding.EncodeToString([]byte(rawSecret))

	logWithB64 := "Authorization header: Basic " + b64Secret
	masked := sm.MaskString(logWithB64)

	if strings.Contains(masked, b64Secret) {
		t.Errorf("base64 secret representation was leaked in log: %s", masked)
	}
	if !strings.Contains(masked, "***") {
		t.Errorf("expected placeholder '***', got: %s", masked)
	}
}

func TestSecretManager_MaskString_OverlappingTokens(t *testing.T) {
	sm := NewSecretManagerFromKey(testMasterKey)

	// Shorter secret is a substring of longer secret
	_ = sm.AddSecret("SHORT_KEY", "token")
	_ = sm.AddSecret("LONG_KEY", "token_admin_long_identity")

	logMsg := "Connecting using token_admin_long_identity now"
	masked := sm.MaskString(logMsg)

	// Long token must be replaced first as a whole
	expected := "Connecting using *** now"
	if masked != expected {
		t.Errorf("expected %q, got %q", expected, masked)
	}
}

