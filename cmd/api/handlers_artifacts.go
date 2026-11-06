// path: cmd/api/handlers_artifacts.go
package main

import (
	"net/http"
	"time"

	"github.com/crnji/forge-ci/internal/artifacts"
	"github.com/crnji/forge-ci/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

func handleListArtifacts(st store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runID := chi.URLParam(r, "runID")
		if runID == "" {
			runID = chi.URLParam(r, "id")
		}

		records, err := st.ListArtifactsByRun(r.Context(), runID)
		if err != nil {
			render.Status(r, http.StatusInternalServerError)
			render.JSON(w, r, map[string]string{"error": err.Error()})
			return
		}

		if records == nil {
			records = []artifacts.ArtifactRecord{}
		}

		render.JSON(w, r, records)
	}
}

func handleDownloadArtifact(st store.Store, storage artifacts.ArtifactStorage, bucket string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		artifactID := chi.URLParam(r, "artifactID")
		if artifactID == "" {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, map[string]string{"error": "artifactID is required"})
			return
		}

		rec, err := st.GetArtifact(r.Context(), artifactID)
		if err != nil {
			render.Status(r, http.StatusNotFound)
			render.JSON(w, r, map[string]string{"error": err.Error()})
			return
		}

		if storage == nil {
			render.Status(r, http.StatusServiceUnavailable)
			render.JSON(w, r, map[string]string{"error": "artifact storage is not configured"})
			return
		}

		if bucket == "" {
			bucket = "forge-artifacts"
		}

		url, err := storage.PresignedDownloadURL(r.Context(), bucket, rec.S3Key, 1*time.Hour)
		if err != nil {
			render.Status(r, http.StatusInternalServerError)
			render.JSON(w, r, map[string]string{"error": err.Error()})
			return
		}

		if r.URL.Query().Get("redirect") == "true" {
			http.Redirect(w, r, url, http.StatusFound)
			return
		}

		render.JSON(w, r, map[string]interface{}{
			"download_url": url,
			"artifact":     rec,
			"expires_in":   "1h",
		})
	}
}

