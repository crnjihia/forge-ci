// path: internal/artifacts/mock_store.go
package artifacts

import (
	"context"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"
)

// MockArtifactStorage provides an in-memory S3 storage double for offline testing.
type MockArtifactStorage struct {
	mu       sync.RWMutex
	uploaded map[string][]byte
}

// NewMockArtifactStorage creates an in-memory artifact storage instance.
func NewMockArtifactStorage() *MockArtifactStorage {
	return &MockArtifactStorage{
		uploaded: make(map[string][]byte),
	}
}

// Upload stores file content in memory.
func (m *MockArtifactStorage) Upload(ctx context.Context, bucket, s3Key string, reader io.Reader, size int64, contentType string) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	mapKey := fmt.Sprintf("%s/%s", bucket, s3Key)
	m.uploaded[mapKey] = data
	return nil
}

// PresignedDownloadURL returns a simulated presigned download URL.
func (m *MockArtifactStorage) PresignedDownloadURL(ctx context.Context, bucket, s3Key string, expiry time.Duration) (string, error) {
	if expiry <= 0 {
		expiry = 1 * time.Hour
	}
	return fmt.Sprintf("http://localhost:9000/%s/%s?X-Amz-Expires=%d", bucket, s3Key, int(expiry.Seconds())), nil
}

// GetUploaded retrieves the stored byte content for an artifact.
func (m *MockArtifactStorage) GetUploaded(bucket, s3Key string) ([]byte, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	mapKey := fmt.Sprintf("%s/%s", bucket, s3Key)
	data, ok := m.uploaded[mapKey]
	return data, ok
}

// UploadedKeys returns a sorted list of all uploaded object keys across all buckets.
func (m *MockArtifactStorage) UploadedKeys() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	keys := make([]string, 0, len(m.uploaded))
	for k := range m.uploaded {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

