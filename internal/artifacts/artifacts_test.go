// path: internal/artifacts/artifacts_test.go
package artifacts

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func setupTestWorkspace(t *testing.T) string {
	t.Helper()
	tempDir := t.TempDir()

	files := map[string]string{
		"dist/app":               "binary content",
		"dist/web/bundle.js":     "console.log('test')",
		"dist/web/bundle.css":    "body { margin: 0; }",
		"build/logs/test.log":    "test passed",
		"docs/manual.txt":        "user guide",
		"coverage.xml":           "<coverage></coverage>",
		"root_ignore.tmp":        "temporary",
	}

	for relPath, content := range files {
		absPath := filepath.Join(tempDir, relPath)
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			t.Fatalf("creating dir for %s: %v", relPath, err)
		}
		if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
			t.Fatalf("writing file %s: %v", relPath, err)
		}
	}

	return tempDir
}

func TestCollectMatchingFiles_RecursiveGlob(t *testing.T) {
	workspace := setupTestWorkspace(t)

	cases := []struct {
		name     string
		patterns []string
		expected []string
	}{
		{
			name:     "recursive directory match dist/**",
			patterns: []string{"dist/**"},
			expected: []string{"dist/app", "dist/web/bundle.css", "dist/web/bundle.js"},
		},
		{
			name:     "extension wildcard **/*.log",
			patterns: []string{"**/*.log"},
			expected: []string{"build/logs/test.log"},
		},
		{
			name:     "exact file match coverage.xml",
			patterns: []string{"coverage.xml"},
			expected: []string{"coverage.xml"},
		},
		{
			name:     "combined multiple patterns",
			patterns: []string{"coverage.xml", "docs/*.txt"},
			expected: []string{"coverage.xml", "docs/manual.txt"},
		},
		{
			name:     "non-existent pattern",
			patterns: []string{"missing/**"},
			expected: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			matches, err := CollectMatchingFiles(workspace, tc.patterns)
			if err != nil {
				t.Fatalf("unexpected matching error: %v", err)
			}
			if !reflect.DeepEqual(matches, tc.expected) {
				t.Errorf("expected %v, got %v", tc.expected, matches)
			}
		})
	}
}

func TestCollectAndUpload_FullCycle(t *testing.T) {
	workspace := setupTestWorkspace(t)
	mockStorage := NewMockArtifactStorage()
	collector := NewArtifactCollector(mockStorage, "forge-artifacts", nil)

	ctx := context.Background()
	records, err := collector.CollectAndUpload(ctx, "run-123", "build-step", workspace, []string{"dist/**"})
	if err != nil {
		t.Fatalf("unexpected upload error: %v", err)
	}

	if len(records) != 3 {
		t.Fatalf("expected 3 artifact records, got %d", len(records))
	}

	// Verify records and uploaded storage contents
	for _, rec := range records {
		if rec.RunID != "run-123" || rec.StepID != "build-step" {
			t.Errorf("unexpected record metadata: %+v", rec)
		}
		if !strings.HasPrefix(rec.S3Key, "runs/run-123/build-step/dist/") {
			t.Errorf("unexpected s3 key: %s", rec.S3Key)
		}

		data, ok := mockStorage.GetUploaded("forge-artifacts", rec.S3Key)
		if !ok || len(data) == 0 {
			t.Errorf("missing uploaded data in mock storage for key %s", rec.S3Key)
		}
	}
}

func TestPresignedDownloadURL(t *testing.T) {
	mockStorage := NewMockArtifactStorage()
	s3Key := "runs/run-abc/step-1/dist/app"

	urlStr, err := mockStorage.PresignedDownloadURL(context.Background(), "forge-artifacts", s3Key, 30*time.Minute)
	if err != nil {
		t.Fatalf("unexpected presigned URL error: %v", err)
	}

	expectedPrefix := "http://localhost:9000/forge-artifacts/runs/run-abc/step-1/dist/app"
	if !strings.HasPrefix(urlStr, expectedPrefix) {
		t.Errorf("expected URL to start with %q, got %q", expectedPrefix, urlStr)
	}
	if !strings.Contains(urlStr, "X-Amz-Expires=1800") {
		t.Errorf("expected expiry parameter in URL, got %q", urlStr)
	}
}

func TestDetectContentType(t *testing.T) {
	cases := []struct {
		path     string
		expected string
	}{
		{"dist/app.json", "application/json"},
		{"report.xml", "application/xml"},
		{"index.html", "text/html"},
		{"build.log", "text/plain"},
		{"app.tar.gz", "application/gzip"},
		{"archive.zip", "application/zip"},
		{"binary.out", "application/octet-stream"},
	}

	for _, c := range cases {
		got := detectContentType(c.path)
		if got != c.expected {
			t.Errorf("path %q: expected %q, got %q", c.path, c.expected, got)
		}
	}
}

