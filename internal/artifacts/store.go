// path: internal/artifacts/store.go
package artifacts

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// ArtifactRecord represents metadata for a stored build artifact.
type ArtifactRecord struct {
	ID        string    `json:"id"`
	RunID     string    `json:"run_id"`
	StepID    string    `json:"step_id"`
	FilePath  string    `json:"file_path"`
	S3Key     string    `json:"s3_key"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

// ArtifactStorage defines operations for uploading artifacts and issuing download URLs.
type ArtifactStorage interface {
	Upload(ctx context.Context, bucket, s3Key string, reader io.Reader, size int64, contentType string) error
	PresignedDownloadURL(ctx context.Context, bucket, s3Key string, expiry time.Duration) (string, error)
}

// MinIOStorage implements ArtifactStorage against a real MinIO or S3 endpoint.
type MinIOStorage struct {
	client *minio.Client
	logger *slog.Logger
}

// MinIOConfig holds connection settings for MinIO.
type MinIOConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	UseSSL    bool
	Logger    *slog.Logger
}

// NewMinIOStorage creates a new S3-compatible storage client.
func NewMinIOStorage(cfg MinIOConfig) (*MinIOStorage, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	cli, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("initializing minio client: %w", err)
	}

	return &MinIOStorage{
		client: cli,
		logger: logger,
	}, nil
}

// Upload transfers an object stream to MinIO, creating the target bucket if missing.
func (s *MinIOStorage) Upload(ctx context.Context, bucket, s3Key string, reader io.Reader, size int64, contentType string) error {
	exists, err := s.client.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("checking bucket %q: %w", bucket, err)
	}
	if !exists {
		if err := s.client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("creating bucket %q: %w", bucket, err)
		}
	}

	if contentType == "" {
		contentType = "application/octet-stream"
	}

	_, err = s.client.PutObject(ctx, bucket, s3Key, reader, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("uploading object %q: %w", s3Key, err)
	}

	return nil
}

// PresignedDownloadURL issues a time-limited GET URL for artifact retrieval.
func (s *MinIOStorage) PresignedDownloadURL(ctx context.Context, bucket, s3Key string, expiry time.Duration) (string, error) {
	if expiry <= 0 {
		expiry = 1 * time.Hour
	}
	reqParams := make(url.Values)
	presignedURL, err := s.client.PresignedGetObject(ctx, bucket, s3Key, expiry, reqParams)
	if err != nil {
		return "", fmt.Errorf("generating presigned URL for %q: %w", s3Key, err)
	}
	return presignedURL.String(), nil
}

// ArtifactCollector scans the workspace for files matching step artifact globs and uploads them.
type ArtifactCollector struct {
	storage ArtifactStorage
	bucket  string
	logger  *slog.Logger
}

// NewArtifactCollector creates an artifact collection engine.
func NewArtifactCollector(storage ArtifactStorage, bucket string, logger *slog.Logger) *ArtifactCollector {
	if logger == nil {
		logger = slog.Default()
	}
	if bucket == "" {
		bucket = "forge-artifacts"
	}
	return &ArtifactCollector{
		storage: storage,
		bucket:  bucket,
		logger:  logger,
	}
}

// CollectAndUpload scans the workspace using step artifact patterns, uploads matches, and returns records.
func (c *ArtifactCollector) CollectAndUpload(ctx context.Context, runID, stepID, workspaceDir string, patterns []string) ([]ArtifactRecord, error) {
	if len(patterns) == 0 || workspaceDir == "" {
		return nil, nil
	}

	matchedRelPaths, err := CollectMatchingFiles(workspaceDir, patterns)
	if err != nil {
		return nil, fmt.Errorf("matching artifact files: %w", err)
	}

	records := make([]ArtifactRecord, 0, len(matchedRelPaths))
	for _, relPath := range matchedRelPaths {
		absPath := filepath.Join(workspaceDir, relPath)
		fileInfo, statErr := os.Stat(absPath)
		if statErr != nil {
			c.logger.Warn("skipping unreadable artifact file", "path", absPath, "error", statErr)
			continue
		}

		file, openErr := os.Open(absPath)
		if openErr != nil {
			c.logger.Warn("skipping unopenable artifact file", "path", absPath, "error", openErr)
			continue
		}

		s3Key := fmt.Sprintf("runs/%s/%s/%s", runID, stepID, filepath.ToSlash(relPath))
		contentType := detectContentType(absPath)

		uploadErr := c.storage.Upload(ctx, c.bucket, s3Key, file, fileInfo.Size(), contentType)
		_ = file.Close()
		if uploadErr != nil {
			return nil, fmt.Errorf("uploading artifact %q: %w", relPath, uploadErr)
		}

		records = append(records, ArtifactRecord{
			ID:        uuid.New().String(),
			RunID:     runID,
			StepID:    stepID,
			FilePath:  filepath.ToSlash(relPath),
			S3Key:     s3Key,
			SizeBytes: fileInfo.Size(),
			CreatedAt: time.Now().UTC(),
		})

		c.logger.Info("uploaded build artifact", "s3_key", s3Key, "size_bytes", fileInfo.Size())
	}

	return records, nil
}

// CollectMatchingFiles walks workspaceDir and returns relative paths of all regular files matching any pattern.
func CollectMatchingFiles(workspaceDir string, patterns []string) ([]string, error) {
	if _, err := os.Stat(workspaceDir); os.IsNotExist(err) {
		return nil, nil
	}

	var results []string
	seen := make(map[string]bool)

	err := filepath.Walk(workspaceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		rel, relErr := filepath.Rel(workspaceDir, path)
		if relErr != nil {
			return relErr
		}
		relSlash := filepath.ToSlash(rel)

		for _, pat := range patterns {
			cleanPat := filepath.ToSlash(strings.TrimSpace(pat))
			if matchGlob(cleanPat, relSlash) {
				if !seen[relSlash] {
					seen[relSlash] = true
					results = append(results, relSlash)
				}
				break
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	sort.Strings(results)
	return results, nil
}

// matchGlob evaluates simple wildcards, single-level (*), and recursive multi-level (**) patterns.
func matchGlob(pattern, path string) bool {
	// Recursive pattern e.g. "dist/**" or "**/*.txt"
	if strings.Contains(pattern, "**") {
		parts := strings.Split(pattern, "**")
		prefix := parts[0]
		suffix := ""
		if len(parts) > 1 {
			suffix = parts[1]
		}

		prefix = strings.TrimSuffix(prefix, "/")
		suffix = strings.TrimPrefix(suffix, "/")

		if prefix != "" && !strings.HasPrefix(path, prefix) {
			return false
		}
		if suffix != "" {
			if strings.HasPrefix(suffix, "*") {
				// e.g. suffix is "*.log"
				return strings.HasSuffix(path, strings.TrimPrefix(suffix, "*"))
			}
			return strings.HasSuffix(path, suffix)
		}
		return true
	}

	// Standard filepath.Match for flat/relative patterns
	matched, err := filepath.Match(pattern, path)
	if err == nil && matched {
		return true
	}

	// Also test matching just the base filename if pattern has no slash
	if !strings.Contains(pattern, "/") {
		baseMatched, baseErr := filepath.Match(pattern, filepath.Base(path))
		if baseErr == nil && baseMatched {
			return true
		}
	}

	return false
}

// detectContentType returns common MIME types based on file extension.
func detectContentType(path string) string {
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
		return "application/gzip"
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".json":
		return "application/json"
	case ".xml":
		return "application/xml"
	case ".html", ".htm":
		return "text/html"
	case ".txt", ".log":
		return "text/plain"
	case ".tar":
		return "application/x-tar"
	case ".gz":
		return "application/gzip"
	case ".zip":
		return "application/zip"
	default:
		return "application/octet-stream"
	}
}

