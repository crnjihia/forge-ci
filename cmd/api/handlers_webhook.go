// path: cmd/api/handlers_webhook.go
package main

import (
	"net/http"
	"time"

	"github.com/crnji/forge-ci/internal/store"
	"github.com/crnji/forge-ci/internal/webhook"
	"github.com/go-chi/render"
	"github.com/google/uuid"
)

func handleGitHubWebhook(st store.Store, coord *Coordinator, webhookSecret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		event, err := webhook.ParseWebhookRequest(r, webhookSecret)
		if err != nil {
			render.Status(r, http.StatusUnauthorized)
			render.JSON(w, r, map[string]string{"error": err.Error()})
			return
		}

		// Ping event or branch deletion returns early acknowledgment
		if event == nil {
			render.Status(r, http.StatusOK)
			render.JSON(w, r, map[string]string{"status": "acknowledged", "message": "ping or ignored event"})
			return
		}

		run := store.RunRecord{
			ID:        uuid.New().String(),
			RepoURL:   event.RepoURL,
			CommitSHA: event.CommitSHA,
			Branch:    event.Branch,
			Status:    "pending",
			Trigger:   "webhook",
			CreatedAt: time.Now().UTC(),
		}

		if err := st.CreateRun(r.Context(), run); err != nil {
			render.Status(r, http.StatusInternalServerError)
			render.JSON(w, r, map[string]string{"error": err.Error()})
			return
		}

		coord.StartRun(&run, "")

		render.Status(r, http.StatusAccepted)
		render.JSON(w, r, map[string]interface{}{
			"status":   "accepted",
			"run_id":   run.ID,
			"repo":     event.RepoName,
			"commit":   event.CommitSHA,
			"branch":   event.Branch,
		})
	}
}

