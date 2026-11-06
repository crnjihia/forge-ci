// path: cmd/api/handlers_runs.go
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/crnji/forge-ci/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/google/uuid"
)

type TriggerRunRequest struct {
	RepoURL      string `json:"repo_url"`
	CommitSHA    string `json:"commit_sha"`
	Branch       string `json:"branch"`
	PipelineYAML string `json:"pipeline_yaml,omitempty"`
}

func handleTriggerRun(st store.Store, coord *Coordinator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req TriggerRunRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, map[string]string{"error": "invalid JSON body"})
			return
		}

		if req.RepoURL == "" {
			req.RepoURL = "https://github.com/crnjihia/forge-ci.git"
		}
		if req.Branch == "" {
			req.Branch = "main"
		}
		if req.CommitSHA == "" {
			b := make([]byte, 20)
			_, _ = rand.Read(b)
			req.CommitSHA = hex.EncodeToString(b)
		}

		run := store.RunRecord{
			ID:        uuid.New().String(),
			RepoURL:   req.RepoURL,
			CommitSHA: req.CommitSHA,
			Branch:    req.Branch,
			Status:    "pending",
			Trigger:   "manual",
			CreatedAt: time.Now().UTC(),
		}

		if err := st.CreateRun(r.Context(), run); err != nil {
			render.Status(r, http.StatusInternalServerError)
			render.JSON(w, r, map[string]string{"error": err.Error()})
			return
		}

		coord.StartRun(&run, req.PipelineYAML)

		render.Status(r, http.StatusCreated)
		render.JSON(w, r, run)
	}
}

func handleListRuns(st store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := 50
		offset := 0

		if l := r.URL.Query().Get("limit"); l != "" {
			if v, err := strconv.Atoi(l); err == nil && v > 0 {
				limit = v
			}
		}
		if o := r.URL.Query().Get("offset"); o != "" {
			if v, err := strconv.Atoi(o); err == nil && v >= 0 {
				offset = v
			}
		}

		runs, err := st.ListRuns(r.Context(), limit, offset)
		if err != nil {
			render.Status(r, http.StatusInternalServerError)
			render.JSON(w, r, map[string]string{"error": err.Error()})
			return
		}

		if runs == nil {
			runs = []store.RunRecord{}
		}

		render.JSON(w, r, runs)
	}
}

func handleGetRun(st store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if id == "" {
			id = chi.URLParam(r, "runID")
		}

		run, err := st.GetRun(r.Context(), id)
		if err != nil {
			render.Status(r, http.StatusNotFound)
			render.JSON(w, r, map[string]string{"error": err.Error()})
			return
		}

		render.JSON(w, r, run)
	}
}

func handleCancelRun(coord *Coordinator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if id == "" {
			id = chi.URLParam(r, "runID")
		}

		if err := coord.CancelRun(id); err != nil {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, map[string]string{"error": err.Error()})
			return
		}

		render.JSON(w, r, map[string]string{"message": "run cancellation requested", "run_id": id})
	}
}

