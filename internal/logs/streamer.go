// path: internal/logs/streamer.go
package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisPub defines the Redis pub/sub operations required by the logging subsystem.
type RedisPub interface {
	Publish(ctx context.Context, channel string, message interface{}) *redis.IntCmd
	Subscribe(ctx context.Context, channels ...string) *redis.PubSub
}

// StreamerConfig holds parameters for capturing, fanning out, and persisting container logs.
type StreamerConfig struct {
	RunID         string
	StepID        string
	BatchSize     int
	FlushInterval time.Duration
	Masker        SecretMasker
	RedisClient   RedisPub
	LogStore      LogStore
}

// Streamer handles demultiplexing, secret masking, Redis broadcasting, and chunked DB storage.
type Streamer struct {
	cfg StreamerConfig
}

// NewStreamer creates a new log streaming pipeline.
func NewStreamer(cfg StreamerConfig) *Streamer {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 50
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 250 * time.Millisecond
	}
	return &Streamer{cfg: cfg}
}

// StreamLogs reads an incoming raw Docker log stream, demuxes it, broadcasts via Redis, and flushes chunks to database.
func (s *Streamer) StreamLogs(ctx context.Context, r io.Reader) error {
	var mu sync.Mutex
	buffer := make([]LogEntry, 0, s.cfg.BatchSize)

	flush := func() error {
		mu.Lock()
		if len(buffer) == 0 {
			mu.Unlock()
			return nil
		}
		chunk := make([]LogEntry, len(buffer))
		copy(chunk, buffer)
		buffer = buffer[:0]
		mu.Unlock()

		if s.cfg.LogStore != nil {
			return s.cfg.LogStore.InsertBatch(ctx, chunk)
		}
		return nil
	}

	// Periodic flusher for low-frequency outputs
	ticker := time.NewTicker(s.cfg.FlushInterval)
	defer ticker.Stop()

	flushDone := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				_ = flush()
			case <-flushDone:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	defer close(flushDone)

	// Demux stream frames line by line
	err := DemuxDockerStream(r, func(stream StreamType, line string) {
		// 1. Scrub sensitive secret tokens
		if s.cfg.Masker != nil {
			line = s.cfg.Masker.MaskString(line)
		}

		entry := LogEntry{
			RunID:     s.cfg.RunID,
			StepID:    s.cfg.StepID,
			Stream:    string(stream),
			Line:      line,
			Timestamp: time.Now().UTC(),
		}

		// 2. Broadcast to Redis Pub/Sub channels (run-level and step-level)
		if s.cfg.RedisClient != nil {
			if payload, jsonErr := json.Marshal(entry); jsonErr == nil {
				runChannel := fmt.Sprintf("logs:%s", s.cfg.RunID)
				stepChannel := fmt.Sprintf("logs:%s:%s", s.cfg.RunID, s.cfg.StepID)
				s.cfg.RedisClient.Publish(ctx, runChannel, string(payload))
				s.cfg.RedisClient.Publish(ctx, stepChannel, string(payload))
			}
		}

		// 3. Buffer for chunked batch insert
		mu.Lock()
		buffer = append(buffer, entry)
		shouldFlush := len(buffer) >= s.cfg.BatchSize
		mu.Unlock()

		if shouldFlush {
			_ = flush()
		}
	})

	// Final flush on EOF
	_ = flush()
	return err
}
