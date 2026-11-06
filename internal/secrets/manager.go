// path: internal/secrets/manager.go
package secrets

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// SecretRecord represents an encrypted secret stored in memory or a database.
type SecretRecord struct {
	Name           string
	EncryptedValue []byte
	Nonce          []byte
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// SecretStore abstracts persistence for encrypted secrets.
type SecretStore interface {
	SaveSecret(ctx context.Context, record SecretRecord) error
	GetSecret(ctx context.Context, name string) (*SecretRecord, error)
	DeleteSecret(ctx context.Context, name string) error
	ListSecretNames(ctx context.Context) ([]string, error)
}

// SecretManager handles authenticated encryption and decryption of secrets using AES-256-GCM.
// It performs real-time secret scrubbing on log output to prevent credential leakage.
type SecretManager struct {
	mu        sync.RWMutex
	masterKey []byte                  // 32-byte key
	store     map[string]SecretRecord // in-memory cache
	dbStore   SecretStore             // optional persistent database backend
}

// NewSecretManager creates a manager reading the master key from FORGE_MASTER_KEY.
func NewSecretManager() (*SecretManager, error) {
	raw := os.Getenv("FORGE_MASTER_KEY")
	if raw == "" {
		return nil, fmt.Errorf("FORGE_MASTER_KEY is required")
	}
	return NewSecretManagerFromKeyString(raw)
}

// NewSecretManagerFromKeyString parses a raw 32-byte string or base64-encoded key.
func NewSecretManagerFromKeyString(raw string) (*SecretManager, error) {
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(key) != 32 {
		key = []byte(raw)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("master key must be exactly 32 bytes (got %d)", len(key))
	}
	return NewSecretManagerFromKey(key), nil
}

// NewSecretManagerFromKey creates a manager directly from a 32-byte slice.
func NewSecretManagerFromKey(key []byte) *SecretManager {
	return &SecretManager{
		masterKey: key,
		store:     make(map[string]SecretRecord),
	}
}

// SetStore attaches a persistent database backing store to the manager.
func (sm *SecretManager) SetStore(store SecretStore) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.dbStore = store
}

// AddSecret encrypts the plaintext value using AES-256-GCM and stores it.
func (sm *SecretManager) AddSecret(name, plain string) error {
	if name == "" {
		return fmt.Errorf("secret name cannot be empty")
	}

	block, err := aes.NewCipher(sm.masterKey)
	if err != nil {
		return fmt.Errorf("creating cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("creating GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("nonce generation failed: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, []byte(plain), nil)
	now := time.Now().UTC()

	record := SecretRecord{
		Name:           name,
		EncryptedValue: ciphertext,
		Nonce:          nonce,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	sm.mu.Lock()
	sm.store[name] = record
	db := sm.dbStore
	sm.mu.Unlock()

	if db != nil {
		return db.SaveSecret(context.Background(), record)
	}
	return nil
}

// GetSecret decrypts and returns the plaintext value for the given name.
func (sm *SecretManager) GetSecret(name string) (string, error) {
	sm.mu.RLock()
	record, ok := sm.store[name]
	db := sm.dbStore
	sm.mu.RUnlock()

	if !ok {
		if db != nil {
			rec, err := db.GetSecret(context.Background(), name)
			if err != nil || rec == nil {
				return "", fmt.Errorf("secret %q not found", name)
			}
			record = *rec
			sm.mu.Lock()
			sm.store[name] = record
			sm.mu.Unlock()
		} else {
			return "", fmt.Errorf("secret %q not found", name)
		}
	}

	block, err := aes.NewCipher(sm.masterKey)
	if err != nil {
		return "", fmt.Errorf("cipher error: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("GCM error: %w", err)
	}

	plain, err := gcm.Open(nil, record.Nonce, record.EncryptedValue, nil)
	if err != nil {
		return "", fmt.Errorf("decryption failed for %q: authentication tag mismatch or corrupted data", name)
	}

	return string(plain), nil
}

// DeleteSecret removes a secret from memory and backing persistence.
func (sm *SecretManager) DeleteSecret(name string) error {
	sm.mu.Lock()
	_, ok := sm.store[name]
	if ok {
		delete(sm.store, name)
	}
	db := sm.dbStore
	sm.mu.Unlock()

	if !ok && db == nil {
		return fmt.Errorf("secret %q not found", name)
	}

	if db != nil {
		return db.DeleteSecret(context.Background(), name)
	}
	return nil
}

// ListSecretNames returns a slice of all stored secret identifiers.
func (sm *SecretManager) ListSecretNames() []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	names := make([]string, 0, len(sm.store))
	for name := range sm.store {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// InjectSecrets retrieves and decrypts the specified list of secret names into an environment map.
func (sm *SecretManager) InjectSecrets(requested []string) (map[string]string, error) {
	env := make(map[string]string, len(requested))
	for _, name := range requested {
		val, err := sm.GetSecret(name)
		if err != nil {
			return nil, fmt.Errorf("resolving pipeline secret %q: %w", name, err)
		}
		env[name] = val
	}
	return env, nil
}

// MaskString replaces any occurrence of any stored secret value with "***".
// It handles edge cases including multi-line strings, Base64 encodings, and URL-encoded variants.
func (sm *SecretManager) MaskString(s string) string {
	if s == "" {
		return ""
	}

	sm.mu.RLock()
	names := make([]string, 0, len(sm.store))
	for name := range sm.store {
		names = append(names, name)
	}
	sm.mu.RUnlock()

	// Collect all patterns to mask
	var targets []string
	seen := make(map[string]bool)

	for _, name := range names {
		plain, err := sm.GetSecret(name)
		if err != nil || len(plain) < 2 {
			continue
		}

		// 1. Literal plaintext
		if !seen[plain] {
			seen[plain] = true
			targets = append(targets, plain)
		}

		// 2. Base64 encoded representation
		b64 := base64.StdEncoding.EncodeToString([]byte(plain))
		if len(b64) >= 4 && !seen[b64] {
			seen[b64] = true
			targets = append(targets, b64)
		}

		// 3. URL-escaped variant
		urlEscaped := url.QueryEscape(plain)
		if urlEscaped != plain && !seen[urlEscaped] {
			seen[urlEscaped] = true
			targets = append(targets, urlEscaped)
		}

		// 4. Multi-line individual line chunks (lines > 6 chars)
		if strings.Contains(plain, "\n") {
			lines := strings.Split(plain, "\n")
			for _, line := range lines {
				trimmed := strings.TrimSpace(line)
				if len(trimmed) >= 6 && !seen[trimmed] {
					seen[trimmed] = true
					targets = append(targets, trimmed)
				}
			}
		}
	}

	// Sort patterns by length descending so longer tokens are replaced before shorter substrings
	sort.Slice(targets, func(i, j int) bool {
		return len(targets[i]) > len(targets[j])
	})

	masked := s
	for _, pattern := range targets {
		masked = strings.ReplaceAll(masked, pattern, "***")
	}

	return masked
}
