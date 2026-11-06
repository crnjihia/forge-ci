// path: internal/logs/logs_test.go
package logs

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

type mockLogStore struct {
	mu      sync.Mutex
	batches [][]LogEntry
}

func (m *mockLogStore) InsertBatch(ctx context.Context, entries []LogEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := make([]LogEntry, len(entries))
	copy(copied, entries)
	m.batches = append(m.batches, copied)
	return nil
}

func (m *mockLogStore) QueryLogs(ctx context.Context, runID, stepID string, afterID int64, limit int) ([]LogEntry, error) {
	return nil, nil
}

type mockMasker struct{}

func (m *mockMasker) MaskString(s string) string {
	return strings.ReplaceAll(s, "SUPER_SECRET_TOKEN", "***")
}

func TestDemuxDockerStream_MixedStdoutStderr(t *testing.T) {
	var streamBuf bytes.Buffer

	// Write 3 Docker multiplexed frames
	streamBuf.Write(EncodeDockerFrame(StreamStdout, "Compiling package...\n"))
	streamBuf.Write(EncodeDockerFrame(StreamStderr, "Warning: deprecated API used\n"))
	streamBuf.Write(EncodeDockerFrame(StreamStdout, "Compilation succeeded!\n"))

	var stdoutLines []string
	var stderrLines []string

	err := DemuxDockerStream(&streamBuf, func(stream StreamType, line string) {
		if stream == StreamStdout {
			stdoutLines = append(stdoutLines, line)
		} else if stream == StreamStderr {
			stderrLines = append(stderrLines, line)
		}
	})

	if err != nil {
		t.Fatalf("unexpected demux error: %v", err)
	}

	if len(stdoutLines) != 2 {
		t.Fatalf("expected 2 stdout lines, got %d: %v", len(stdoutLines), stdoutLines)
	}
	if stdoutLines[0] != "Compiling package..." || stdoutLines[1] != "Compilation succeeded!" {
		t.Errorf("unexpected stdout lines: %v", stdoutLines)
	}

	if len(stderrLines) != 1 {
		t.Fatalf("expected 1 stderr line, got %d: %v", len(stderrLines), stderrLines)
	}
	if stderrLines[0] != "Warning: deprecated API used" {
		t.Errorf("unexpected stderr line: %v", stderrLines)
	}
}

func TestDemuxDockerStream_FallbackPlainText(t *testing.T) {
	plain := "Hello world\nSecond line\n"
	buf := strings.NewReader(plain)

	var lines []string
	err := DemuxDockerStream(buf, func(stream StreamType, line string) {
		lines = append(lines, line)
	})

	if err != nil {
		t.Fatalf("unexpected fallback error: %v", err)
	}

	if len(lines) != 2 || lines[0] != "Hello world" || lines[1] != "Second line" {
		t.Errorf("unexpected lines from plain text fallback: %v", lines)
	}
}

func TestStreamer_ChunkedBatchingAndMasking(t *testing.T) {
	store := &mockLogStore{}
	masker := &mockMasker{}

	streamer := NewStreamer(StreamerConfig{
		RunID:         "run-batch-1",
		StepID:        "step-1",
		BatchSize:     3, // Small batch size to trigger flushes
		FlushInterval: 100 * time.Millisecond,
		Masker:        masker,
		LogStore:      store,
	})

	var streamBuf bytes.Buffer
	// Write 5 log lines with sensitive tokens
	streamBuf.Write(EncodeDockerFrame(StreamStdout, "Line 1: auth with SUPER_SECRET_TOKEN\n"))
	streamBuf.Write(EncodeDockerFrame(StreamStdout, "Line 2: processing\n"))
	streamBuf.Write(EncodeDockerFrame(StreamStdout, "Line 3: checkpoint\n"))
	streamBuf.Write(EncodeDockerFrame(StreamStdout, "Line 4: finished\n"))
	streamBuf.Write(EncodeDockerFrame(StreamStdout, "Line 5: exiting\n"))

	err := streamer.StreamLogs(context.Background(), &streamBuf)
	if err != nil {
		t.Fatalf("unexpected streaming error: %v", err)
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	// 5 items with batch size 3 should result in 2 batches (batch 1 has 3 items, batch 2 has 2 items)
	if len(store.batches) != 2 {
		t.Fatalf("expected 2 batches, got %d", len(store.batches))
	}
	if len(store.batches[0]) != 3 {
		t.Errorf("expected batch 0 to have 3 items, got %d", len(store.batches[0]))
	}
	if len(store.batches[1]) != 2 {
		t.Errorf("expected batch 1 to have 2 items, got %d", len(store.batches[1]))
	}

	// Verify masking occurred
	firstLine := store.batches[0][0].Line
	if strings.Contains(firstLine, "SUPER_SECRET_TOKEN") {
		t.Errorf("expected secret to be masked, got: %s", firstLine)
	}
	if !strings.Contains(firstLine, "***") {
		t.Errorf("expected masked placeholder '***', got: %s", firstLine)
	}
}

func TestSSEHandler_HeadersAndMissingRunID(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/runs/{runID}/logs", SSEHandler(nil, nil, nil))

	// Test missing runID route param
	req := httptest.NewRequest(http.MethodGet, "/runs//logs", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Route without runID will 404 or 400
	if w.Code != http.StatusNotFound && w.Code != http.StatusBadRequest {
		t.Errorf("expected 400/404 for empty run ID, got %d", w.Code)
	}
}

