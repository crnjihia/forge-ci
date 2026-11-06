// path: internal/logs/http.go
package logs

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SSEHandler streams logs for a run (and optional step) over Server-Sent Events.
// It replays backlog logs from PostgreSQL or in-memory cache, and streams real-time updates
// from Redis Pub/Sub or DefaultMockPubSub with secret masking.
func SSEHandler(redisClient RedisPub, dbPool *pgxpool.Pool, masker SecretMasker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		runID := chi.URLParam(r, "runID")
		stepID := r.URL.Query().Get("step")

		if runID == "" {
			http.Error(w, "runID is required", http.StatusBadRequest)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported by client", http.StatusInternalServerError)
			return
		}

		// Setup Server-Sent Events headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		// Send initial preamble comment so browser's EventSource immediately transitions to OPEN (readyState=1)
		fmt.Fprintf(w, ": connected\n\n")
		flusher.Flush()

		// Parse Last-Event-ID header for client reconnection catchup
		var lastID int64
		if idStr := r.Header.Get("Last-Event-ID"); idStr != "" {
			if v, err := strconv.ParseInt(idStr, 10, 64); err == nil {
				lastID = v
			}
		}

		// 1. Replay historical logs from PostgreSQL if available
		if lastID > 0 && dbPool != nil {
			query := `SELECT id, run_id, step_id, stream, line, timestamp FROM logs WHERE run_id=$1 AND id > $2 ORDER BY id ASC LIMIT 500`
			args := []interface{}{runID, lastID}

			if stepID != "" {
				query = `SELECT id, run_id, step_id, stream, line, timestamp FROM logs WHERE run_id=$1 AND step_id=$2 AND id > $3 ORDER BY id ASC LIMIT 500`
				args = []interface{}{runID, stepID, lastID}
			}

			rows, err := dbPool.Query(ctx, query, args...)
			if err == nil {
				for rows.Next() {
					var entry LogEntry
					if scanErr := rows.Scan(&entry.ID, &entry.RunID, &entry.StepID, &entry.Stream, &entry.Line, &entry.Timestamp); scanErr == nil {
						if masker != nil {
							entry.Line = masker.MaskString(entry.Line)
						}
						if payload, jsonErr := json.Marshal(entry); jsonErr == nil {
							fmt.Fprintf(w, "id: %d\nevent: log\ndata: %s\n\n", entry.ID, string(payload))
							flusher.Flush()
						}
					}
				}
				rows.Close()
			}
		}

		channel := fmt.Sprintf("logs:%s", runID)
		if stepID != "" {
			channel = fmt.Sprintf("logs:%s:%s", runID, stepID)
		}

		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()

		// 2. Real Redis Mode
		if redisClient != nil {
			sub := redisClient.Subscribe(ctx, channel)
			defer sub.Close()
			ch := sub.Channel()

			for {
				select {
				case msg, ok := <-ch:
					if !ok {
						return
					}
					data := msg.Payload
					if masker != nil {
						data = masker.MaskString(data)
					}
					fmt.Fprintf(w, "event: log\ndata: %s\n\n", data)
					flusher.Flush()

				case <-ticker.C:
					fmt.Fprintf(w, ": keepalive\n\n")
					flusher.Flush()

				case <-ctx.Done():
					return
				}
			}
		}

		// 3. In-Memory Mock Mode (Offline / Local Dev)
		// Replay existing published logs for this channel so past logs are never missed
		for _, raw := range DefaultMockPubSub.Published(channel) {
			data := raw
			if masker != nil {
				data = masker.MaskString(data)
			}
			fmt.Fprintf(w, "event: log\ndata: %s\n\n", data)
			flusher.Flush()
		}

		ch := DefaultMockPubSub.Subscribe(channel)
		defer DefaultMockPubSub.Unsubscribe(channel, ch)

		for {
			select {
			case data, ok := <-ch:
				if !ok {
					return
				}
				if masker != nil {
					data = masker.MaskString(data)
				}
				fmt.Fprintf(w, "event: log\ndata: %s\n\n", data)
				flusher.Flush()

			case <-ticker.C:
				fmt.Fprintf(w, ": keepalive\n\n")
				flusher.Flush()

			case <-ctx.Done():
				return
			}
		}
	}
}
